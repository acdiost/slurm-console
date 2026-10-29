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
	"strconv"
	"strings"
)

func registerTRESRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("tres.read", true))
	r.GET("/tres", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		v, err := services.SlurmFor(e.App).Read(e.Request.Context(), e.Request.PathValue("clusterId"), "tres", "")
		if err != nil {
			return slurmFailure(e, err)
		}
		a, err := rbac.Resolve(e.App, e.Auth.Id)
		if err != nil {
			return err
		}
		if !a.Allows("tres.read", e.Request.PathValue("clusterId")) {
			return e.ForbiddenError("资源目录权限已撤销。", nil)
		}
		var rows []models.TRES
		if json.Unmarshal(v.Data, &rows) != nil {
			return e.InternalServerError("", nil)
		}
		return response(e, map[string]any{"items": rows, "scope": "shared_registry", "fetchedAt": v.FetchedAt, "cached": v.Cached})
	})
	r.GET("/tres-limits", listTRESLimits)
	r.GET("/tres-limits/{kind}/{targetId}", getTRESLimits)
	r.GET("/tres-operations", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		v, err := accounting.For(e.App).RecentTRES(e.Auth.Id, e.Request.PathValue("clusterId"))
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, map[string]any{"items": v})
	})
}
func tresSnapshot(e *core.RequestEvent, kind string) (models.TRESSnapshot, services.SlurmResult, error) {
	var snapshot models.TRESSnapshot
	empty := services.SlurmResult{}
	cluster := e.Request.PathValue("clusterId")
	if len(client.TRESBuckets(kind)) == 0 {
		return snapshot, empty, e.BadRequestError("限额对象类型不合法。", nil)
	}
	permission := "associations.read"
	if kind == "qos" {
		permission = "qos.read"
	}
	a, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return snapshot, empty, err
	}
	if !a.Allows(permission, cluster) || kind == "qos" && !a.Allows("associations.read", cluster) {
		return snapshot, empty, e.ForbiddenError("没有该类限额对象的读取权限。", nil)
	}
	result, err := services.SlurmFor(e.App).Read(e.Request.Context(), cluster, "tres_limits", kind)
	if err != nil {
		return snapshot, result, slurmFailure(e, err)
	}
	a, err = rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return snapshot, result, err
	}
	if !a.Allows("tres.read", cluster) || !a.Allows(permission, cluster) || kind == "qos" && !a.Allows("associations.read", cluster) {
		return snapshot, result, e.ForbiddenError("限额读取权限已撤销。", nil)
	}
	if json.Unmarshal(result.Data, &snapshot) != nil {
		return snapshot, result, e.InternalServerError("", nil)
	}
	if kind == "qos" && !a.Allows("platform.qos.read", "") {
		visible := []models.TRESTarget{}
		for _, v := range snapshot.Targets {
			if slices.Contains(snapshot.ReferencedQOS, v.Name) {
				visible = append(visible, v)
			}
		}
		snapshot.Targets = visible
	}
	return snapshot, result, nil
}
func listTRESLimits(e *core.RequestEvent) error {
	if err := slurmQuery(e, "target", "q", "account", "user", "page", "perPage", "sort", "order"); err != nil {
		return err
	}
	q := e.Request.URL.Query()
	kind := q.Get("target")
	if kind == "" {
		kind = "association"
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	sort, order := q.Get("sort"), q.Get("order")
	if sort == "" {
		sort = "id"
	}
	if order == "" {
		order = "asc"
	}
	if !slices.Contains([]string{"id", "name", "account", "user"}, sort) || !slices.Contains([]string{"asc", "desc"}, order) {
		return e.BadRequestError("限额排序参数不合法。", nil)
	}
	snapshot, result, err := tresSnapshot(e, kind)
	if err != nil {
		return err
	}
	rows := []models.TRESTarget{}
	for _, v := range snapshot.Targets {
		if contains(q.Get("q"), v.ID+" "+v.Name+" "+v.Account+" "+v.User+" "+v.Partition) && (q.Get("account") == "" || v.Account == q.Get("account")) && (q.Get("user") == "" || v.User == q.Get("user")) {
			rows = append(rows, v)
		}
	}
	slices.SortFunc(rows, func(a, b models.TRESTarget) int {
		cmp := 0
		switch sort {
		case "name":
			cmp = strings.Compare(a.Name, b.Name)
		case "account":
			cmp = strings.Compare(a.Account, b.Account)
		case "user":
			cmp = strings.Compare(a.User, b.User)
		}
		if cmp == 0 {
			ai, _ := strconv.Atoi(a.ID)
			bi, _ := strconv.Atoi(b.ID)
			cmp = ai - bi
		}
		if order == "desc" {
			return -cmp
		}
		return cmp
	})
	total := len(rows)
	start := min((page-1)*size, total)
	rows = rows[start:min(start+size, total)]
	return response(e, map[string]any{"items": rows, "resources": snapshot.Resources, "slurmCluster": snapshot.Cluster, "page": page, "perPage": size, "totalItems": total, "fetchedAt": result.FetchedAt, "cached": result.Cached})
}
func getTRESLimits(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	id := e.Request.PathValue("targetId")
	if !client.ValidJobID(id) {
		return e.BadRequestError("对象 ID 不合法。", nil)
	}
	snapshot, result, err := tresSnapshot(e, e.Request.PathValue("kind"))
	if err != nil {
		return err
	}
	for _, v := range snapshot.Targets {
		if v.ID == id {
			return response(e, map[string]any{"item": v, "resources": snapshot.Resources, "fetchedAt": result.FetchedAt, "cached": result.Cached})
		}
	}
	return slurmFailure(e, client.Failure("not_found", "限额对象不存在或不在可见范围内。"))
}
