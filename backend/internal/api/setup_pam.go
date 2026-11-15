package api

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
)

// The setup group already requires an authenticated, active platform user.
func registerSetupPAMRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	group.POST("/pam/check", checkSetupPAM).BindFunc(rbac.Require("platform.settings.manage", false))
	group.POST("/pam/configure", configureSetupPAM).BindFunc(rbac.Require("platform.settings.manage", false))
	group.PATCH("/pam", saveSetupPAM).BindFunc(rbac.Require("platform.settings.manage", false))
}

func pamSetupFailure(e *core.RequestEvent, status auth.PAMStatus, err error) error {
	if errors.Is(err, auth.ErrPAMConflict) {
		return e.Error(409, "PAM 设置已变化，请刷新后重新核对。", nil)
	}
	if errors.Is(err, auth.ErrPAMUnavailable) {
		e.Set("publicErrorMessage", status.Message)
		e.Set("publicErrorCode", "PAM_UNAVAILABLE")
		return e.Error(503, status.Message, nil)
	}
	var apiError *router.ApiError
	if errors.As(err, &apiError) {
		return err
	}
	e.Set("publicErrorMessage", "PAM 设置未保存，请刷新后重试。")
	e.Set("publicErrorCode", "PAM_SETUP_FAILED")
	return e.Error(503, "PAM 设置未保存，请刷新后重试。", nil)
}

func checkSetupPAM(e *core.RequestEvent) error {
	var input struct{}
	if err := platformBody(e, &input); err != nil {
		return err
	}
	status, err := auth.CheckPAMSetup(e.Request.Context(), e.App)
	if err != nil {
		return pamSetupFailure(e, status, err)
	}
	requestID, _ := e.Get("requestId").(string)
	err = audit.Mutate(e.App, audit.PlatformActor(e.Auth, requestID), func(tx core.App) (audit.Event, error) {
		if err := platformFresh(e, tx, "platform.settings.manage"); err != nil {
			return audit.Event{}, err
		}
		message := "系统认证组件检测未通过，请检查安装和权限。"
		if status.Available {
			message = "系统认证组件检测通过；未更改启用状态。"
		}
		return audit.Event{Action: "platform.pam.check", TargetType: "pam_settings", TargetID: auth.PAMSettingsID, Message: message}, nil
	})
	if err != nil {
		return pamSetupFailure(e, status, err)
	}
	return response(e, status)
}

func saveSetupPAM(e *core.RequestEvent) error {
	var input struct {
		Enabled  bool `json:"enabled"`
		Revision int  `json:"revision"`
	}
	if err := platformBody(e, &input, "enabled", "revision"); err != nil {
		return err
	}
	if input.Revision < 1 {
		return e.BadRequestError("PAM 修订号不合法，请刷新后重试。", nil)
	}
	requestID, _ := e.Get("requestId").(string)
	status, err := auth.SavePAMSetup(e.Request.Context(), e.App, input.Enabled, input.Revision, audit.PlatformActor(e.Auth, requestID), func(tx core.App) error {
		return platformFresh(e, tx, "platform.settings.manage")
	})
	if err != nil {
		return pamSetupFailure(e, status, err)
	}
	return response(e, status)
}

func configureSetupPAM(e *core.RequestEvent) error {
	var input struct {
		Revision int `json:"revision"`
	}
	if err := platformBody(e, &input, "revision"); err != nil {
		return err
	}
	if input.Revision < 1 {
		return e.BadRequestError("PAM 修订号不合法，请刷新后重试。", nil)
	}
	requestID, _ := e.Get("requestId").(string)
	status, err := auth.ConfigurePAMSetup(e.Request.Context(), e.App, input.Revision, audit.PlatformActor(e.Auth, requestID), func(tx core.App) error {
		return platformFresh(e, tx, "platform.settings.manage")
	})
	if err != nil {
		return pamSetupFailure(e, status, err)
	}
	return response(e, status)
}
