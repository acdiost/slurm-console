package api

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/linuxusers"
	"slurm-console/backend/internal/rbac"
)

func registerLinuxControlRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	routes := group.Group("/clusters/{clusterId}")
	routes.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("linux_users.read", true))
	routes.GET("/linux-user-controls", func(e *core.RequestEvent) error {
		if err := clusterQuery(e); err != nil {
			return err
		}
		v, err := linuxusers.For(e.App).Capabilities(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"))
		if err != nil {
			return linuxUserFailure(e, err)
		}
		return response(e, v)
	})
	routes.POST("/linux-user-controls/prepare", func(e *core.RequestEvent) error {
		if err := clusterQuery(e); err != nil {
			return err
		}
		var input struct {
			Action string                `json:"action"`
			Spec   linuxusers.CreateSpec `json:"spec"`
		}
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		v, err := linuxusers.For(e.App).Prepare(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), input.Action, input.Spec)
		if err != nil {
			return linuxUserFailure(e, err)
		}
		return response(e, v)
	})
	routes.POST("/linux-user-controls/execute", func(e *core.RequestEvent) error {
		if err := clusterQuery(e); err != nil {
			return err
		}
		var input linuxusers.ControlInput
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		id, _ := e.Get("requestId").(string)
		v, err := linuxusers.For(e.App).Execute(e.Auth.Id, e.Request.PathValue("clusterId"), id, input)
		if err != nil {
			return linuxUserFailure(e, err)
		}
		return response(e, v)
	})
	routes.GET("/linux-user-operations/{operationId}", func(e *core.RequestEvent) error {
		if err := clusterQuery(e); err != nil {
			return err
		}
		v, err := linuxusers.For(e.App).GetOperation(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), e.Request.PathValue("operationId"))
		if err != nil {
			return linuxUserFailure(e, err)
		}
		return response(e, v)
	})
}
