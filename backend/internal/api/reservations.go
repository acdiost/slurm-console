package api

import (
	"encoding/json"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slices"
	"slurm-console/backend/internal/accounting"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strings"
	"time"
)

func registerReservationRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("reservations.read", true))
	r.GET("/reservations", listReservations)
	r.GET("/reservations/{name}", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		name := e.Request.PathValue("name")
		if !client.ValidAccountingName(name) {
			return e.BadRequestError("预留名称不合法。", nil)
		}
		rows, meta, err := reservationData(e)
		if err != nil {
			return err
		}
		for _, v := range rows {
			if v.Name == name {
				return response(e, map[string]any{"item": v, "fetchedAt": meta.FetchedAt, "cached": meta.Cached})
			}
		}
		return slurmFailure(e, client.Failure("not_found", "预留不存在或已过期。"))
	})
	r.POST("/reservation-controls/prepare", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var input struct {
			Action string                 `json:"action"`
			Spec   client.ReservationSpec `json:"spec"`
		}
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		if !client.IsReservation(input.Action) {
			return e.BadRequestError("预留动作不合法。", nil)
		}
		v, err := accounting.For(e.App).Prepare(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), input.Action, accounting.Spec{Reservation: &input.Spec})
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, v)
	})
	r.POST("/reservation-controls/execute", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var input accounting.Input
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		id, _ := e.Get("requestId").(string)
		v, err := accounting.For(e.App).ExecuteReservation(e.Auth.Id, e.Request.PathValue("clusterId"), id, input)
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, v)
	})
	r.GET("/reservation-operations", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		v, err := accounting.For(e.App).RecentReservations(e.Auth.Id, e.Request.PathValue("clusterId"))
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, map[string]any{"items": v})
	})
	r.GET("/reservation-operations/{operationId}", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		id := e.Request.PathValue("operationId")
		if len(id) != 26 {
			return e.BadRequestError("操作编号不合法。", nil)
		}
		v, err := accounting.For(e.App).Get(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), id)
		if err != nil {
			return slurmFailure(e, err)
		}
		if !client.IsReservation(v.Action) {
			return e.NotFoundError("预留操作不存在。", nil)
		}
		return response(e, v)
	})
}
func reservationData(e *core.RequestEvent) ([]models.Reservation, services.SlurmResult, error) {
	result, err := accountingData(e, "reservations", "reservations.read")
	if err != nil {
		return nil, result, err
	}
	var rows []models.Reservation
	if json.Unmarshal(result.Data, &rows) != nil {
		return nil, result, e.InternalServerError("", nil)
	}
	for i := range rows {
		r := &rows[i]
		r.State = "unknown"
		if r.StartTime != nil && r.EndTime != nil {
			now := time.Now().Unix()
			switch {
			case now < *r.StartTime:
				r.State = "scheduled"
			case now < *r.EndTime:
				r.State = "active"
			default:
				r.State = "ended"
			}
		}
	}
	return rows, result, nil
}
func listReservations(e *core.RequestEvent) error {
	if err := slurmQuery(e, "q", "state", "page", "perPage", "sort", "order"); err != nil {
		return err
	}
	q := e.Request.URL.Query()
	state := q.Get("state")
	if !slices.Contains([]string{"", "scheduled", "active", "ended", "unknown"}, state) {
		return e.BadRequestError("预留状态不合法。", nil)
	}
	sort, order := q.Get("sort"), q.Get("order")
	if sort == "" {
		sort = "name"
	}
	if order == "" {
		order = "asc"
	}
	if !slices.Contains([]string{"name", "startTime", "endTime"}, sort) || !slices.Contains([]string{"asc", "desc"}, order) {
		return e.BadRequestError("预留排序不合法。", nil)
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	rows, meta, err := reservationData(e)
	if err != nil {
		return err
	}
	out := []models.Reservation{}
	for _, v := range rows {
		if contains(q.Get("q"), v.Name+" "+v.Nodes+" "+v.Users+" "+v.Accounts) && (state == "" || state == v.State) {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, func(a, b models.Reservation) int {
		cmp := 0
		if sort != "name" {
			x, y := a.StartTime, b.StartTime
			if sort == "endTime" {
				x, y = a.EndTime, b.EndTime
			}
			switch {
			case x == nil && y != nil:
				cmp = 1
			case x != nil && y == nil:
				cmp = -1
			case x != nil && y != nil:
				if *x < *y {
					cmp = -1
				} else if *x > *y {
					cmp = 1
				}
			}
		}
		if cmp == 0 {
			cmp = strings.Compare(a.Name, b.Name)
		}
		if order == "desc" {
			cmp = -cmp
		}
		return cmp
	})
	return pageResult(e, out, meta, page, size)
}
