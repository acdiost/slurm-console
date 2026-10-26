package api

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/nodecontrol"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/slurm/client"
)

func registerNodeControlRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	routes := group.Group("/clusters/{clusterId}")
	routes.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("nodes.read", true))
	routes.GET("/nodes/{nodeName}/control-context", func(e *core.RequestEvent) error {
		if err := slurmQuery(e, "action"); err != nil {
			return err
		}
		name := e.Request.PathValue("nodeName")
		if !client.ValidNodeTarget(name) {
			return e.BadRequestError("节点名称不合法。", nil)
		}
		value, err := nodecontrol.For(e.App).Prepare(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), name, e.Request.URL.Query().Get("action"))
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, value)
	})
	for _, action := range []string{"drain", "resume"} {
		routes.POST("/nodes/{nodeName}/"+action, func(e *core.RequestEvent) error {
			if err := slurmQuery(e); err != nil {
				return err
			}
			name := e.Request.PathValue("nodeName")
			if !client.ValidNodeTarget(name) {
				return e.BadRequestError("节点名称不合法。", nil)
			}
			var input nodecontrol.Input
			if err := readConfigurationBody(e, &input); err != nil {
				return err
			}
			requestID, _ := e.Get("requestId").(string)
			value, err := nodecontrol.For(e.App).Execute(e.Auth.Id, e.Request.PathValue("clusterId"), name, action, requestID, input)
			if err != nil {
				return slurmFailure(e, err)
			}
			return response(e, value)
		}).BindFunc(rbac.Require("nodes."+action, true))
	}
	routes.GET("/node-operations/{operationId}", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		id := e.Request.PathValue("operationId")
		if len(id) > 64 {
			return e.BadRequestError("操作编号不合法。", nil)
		}
		value, err := nodecontrol.For(e.App).Get(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), id)
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, value)
	})
}
