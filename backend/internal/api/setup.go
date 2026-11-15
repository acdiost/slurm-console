package api

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/rbac"
)

func registerSetupRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	routes := group.Group("/setup")
	routes.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	routes.GET("/status", setupStatus)
	registerSetupPAMRoutes(routes)
	registerSetupSlurmRoutes(routes)
}

func setupStatus(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	access, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return e.Error(503, "接入状态暂不可用。", nil)
	}
	if !access.Allows("platform.settings.manage", "") && !access.Allows("platform.clusters.manage", "") {
		return e.ForbiddenError("需要平台设置或集群配置管理权限。", nil)
	}
	pam, err := auth.PAMSetupStatus(e.App)
	if err != nil {
		return e.Error(503, "认证接入状态暂不可用。", nil)
	}
	profiles := clusterconfig.For(e.App).Registry.Views()
	refs := make([]any, len(profiles))
	for i, profile := range profiles {
		refs[i] = profile.Ref
	}
	var configured int64
	if len(refs) > 0 {
		configured, err = e.App.CountRecords("clusters", dbx.HashExp{"enabled": true}, dbx.In("connection_ref", refs...))
		if err != nil {
			return e.Error(503, "集群接入状态暂不可用。", nil)
		}
	}
	total, err := e.App.CountRecords("clusters")
	if err != nil {
		return e.Error(503, "集群接入状态暂不可用。", nil)
	}
	return response(e, map[string]any{
		"needsSetup": configured == 0,
		"pam":        pam,
		"slurm": map[string]any{
			"configured":   configured > 0,
			"profileCount": len(profiles),
			"clusterCount": total,
		},
	})
}
