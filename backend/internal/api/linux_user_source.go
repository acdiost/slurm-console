package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/linuxusers"
	"slurm-console/backend/internal/rbac"
)

func registerLinuxSourceRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}/linux-user-source")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("platform.settings.manage", false)).BindFunc(rbac.Require("linux_users.read", true))
	r.GET("", func(e *core.RequestEvent) error {
		if err := clusterQuery(e); err != nil {
			return err
		}
		v, err := linuxusers.For(e.App).GetSource(e.Auth.Id, e.Request.PathValue("clusterId"))
		if err != nil {
			return linuxSourceFailure(e, err)
		}
		return response(e, v)
	})
	r.PUT("", func(e *core.RequestEvent) error {
		var input linuxusers.SourceInput
		if err := linuxSourceBody(e, &input, "revision", "label", "transport", "host", "port", "username", "privateKey", "knownHosts", "manageLocal"); err != nil {
			return err
		}
		requestID, _ := e.Get("requestId").(string)
		v, err := linuxusers.For(e.App).SaveSource(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), requestID, input)
		if err != nil {
			return linuxSourceFailure(e, err)
		}
		return response(e, v)
	})
	r.DELETE("", func(e *core.RequestEvent) error {
		var input struct {
			Revision int `json:"revision"`
		}
		if err := linuxSourceBody(e, &input, "revision"); err != nil {
			return err
		}
		requestID, _ := e.Get("requestId").(string)
		v, err := linuxusers.For(e.App).DeleteSource(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), requestID, input.Revision)
		if err != nil {
			return linuxSourceFailure(e, err)
		}
		return response(e, v)
	})
}
func linuxSourceBody(e *core.RequestEvent, target any, required ...string) error {
	if err := clusterQuery(e); err != nil {
		return err
	}
	raw, err := io.ReadAll(http.MaxBytesReader(e.Response, e.Request.Body, 7<<20))
	if err != nil || platformJSON(raw, target, required...) != nil {
		return e.BadRequestError("请提交完整有效的身份来源 JSON，不允许路径、未知字段、重复键或空值。", nil)
	}
	return nil
}
func linuxSourceFailure(e *core.RequestEvent, err error) error {
	var failure *linuxusers.Failure
	if errors.As(err, &failure) {
		return linuxUserFailure(e, err)
	}
	// Database/probe errors can contain supplied credentials; never expose or log them.
	return e.Error(503, "身份来源配置未完成，请刷新后重试。", nil)
}
