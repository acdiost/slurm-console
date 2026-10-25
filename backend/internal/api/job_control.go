package api

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/jobcontrol"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/slurm/client"
)

func registerJobControlRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	routes := group.Group("/clusters/{clusterId}")
	routes.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("jobs.read", true))
	routes.GET("/jobs/{jobId}/cancel-context", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		id := e.Request.PathValue("jobId")
		if !client.ValidJobID(id) {
			return e.BadRequestError("Job ID 不合法。", nil)
		}
		value, err := jobcontrol.For(e.App).Prepare(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), id)
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, value)
	})
	routes.POST("/jobs/{jobId}/cancel", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		id := e.Request.PathValue("jobId")
		if !client.ValidJobID(id) {
			return e.BadRequestError("Job ID 不合法。", nil)
		}
		var input jobcontrol.Input
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		requestID, _ := e.Get("requestId").(string)
		value, err := jobcontrol.For(e.App).Cancel(e.Auth.Id, e.Request.PathValue("clusterId"), id, requestID, input)
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, value)
	}).BindFunc(rbac.Require("jobs.cancel", true))
	routes.GET("/job-operations/{operationId}", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		value, err := jobcontrol.For(e.App).Get(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), e.Request.PathValue("operationId"))
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, value)
	})
}
