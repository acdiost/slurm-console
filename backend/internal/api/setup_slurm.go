package api

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/rbac"
)

func registerSetupSlurmRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	group.POST("/slurm", func(e *core.RequestEvent) error {
		var input clusterconfig.SetupInput
		if err := platformBody(e, &input, "name", "slug", "endpoint", "authType", "apiVersion", "authenticationMethod", "timeoutSeconds"); err != nil {
			return err
		}
		requestID, _ := e.Get("requestId").(string)
		view, err := clusterconfig.For(e.App).Setup(e.Request.Context(), input, audit.PlatformActor(e.Auth, requestID), func(tx core.App) error {
			return configurationFresh(e, tx, false)
		})
		if errors.Is(err, clusterconfig.ErrSetupUnavailable) {
			e.Set("publicErrorMessage", "无法验证 Slurm 连接。请确认 slurmrestd 已启动、API 可用，本地套接字可由服务访问，或远程 HTTPS 证书及 JWT 凭据有效。")
			e.Set("publicErrorCode", "SLURM_SETUP_UNAVAILABLE")
			return e.Error(503, "Slurm 连接验证失败。", nil)
		}
		if err != nil {
			var apiError *router.ApiError
			if errors.As(err, &apiError) {
				return err
			}
			return configurationError(e, err)
		}
		configurationLog(e, "setup", view)
		return response(e, view)
	}).BindFunc(rbac.Require("platform.clusters.manage", false))
}
