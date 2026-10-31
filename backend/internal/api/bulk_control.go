package api

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/bulkcontrol"
	"strings"
)

func registerBulkControlRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}/bulk-control")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	r.POST("/prepare", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var in struct {
			Action  string   `json:"action"`
			Targets []string `json:"targets"`
		}
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		v, err := bulkcontrol.For(e.App).Prepare(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), in.Action, in.Targets)
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, v)
	})
	r.POST("/{batchId}/execute", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		id := e.Request.PathValue("batchId")
		if !bulkcontrol.ValidID(id) {
			return e.BadRequestError("批量确认编号不合法。", nil)
		}
		var in bulkcontrol.Input
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		requestID, _ := e.Get("requestId").(string)
		v, err := bulkcontrol.For(e.App).Execute(e.Auth.Id, e.Request.PathValue("clusterId"), id, requestID, in)
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, v)
	})
	r.GET("/operations", func(e *core.RequestEvent) error {
		if err := slurmQuery(e, "resource", "ids"); err != nil {
			return err
		}
		q := e.Request.URL.Query()
		v, err := bulkcontrol.For(e.App).Operations(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), q.Get("resource"), strings.Split(q.Get("ids"), ","))
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, v)
	})
	r.GET("/{batchId}", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		id := e.Request.PathValue("batchId")
		if !bulkcontrol.ValidID(id) {
			return e.BadRequestError("批量确认编号不合法。", nil)
		}
		v, err := bulkcontrol.For(e.App).Get(e.Auth.Id, e.Request.PathValue("clusterId"), id)
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, v)
	})
}
