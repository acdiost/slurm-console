package auth

import (
	"database/sql"
	"errors"
	"net"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/pamauth"
)

const pamProviderKey = "platform.pamProvider"

func ConfigurePAM(app core.App) error {
	state := pamState(app)
	state.lifecycle.Do(func() {
		RegisterPAM(app, nil)
		app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			// First startup may not have run application migrations yet.
			if _, err := app.FindCollectionByNameOrId(pamSettingsCollection); errors.Is(err, sql.ErrNoRows) {
				return nil
			} else if err != nil {
				return err
			}
			if err := ApplyPAMSetup(app); err != nil {
				if errors.Is(err, ErrPAMUnavailable) {
					app.Logger().Warn("PAM unavailable; local administrator login remains available", "error", err)
					return nil
				}
				return err
			}
			return nil
		})
	})
	return nil
}

func RegisterPAM(app core.App, provider pamauth.Provider) {
	app.Store().Set(pamProviderKey, provider)
	pamState(app).hooks.Do(func() {
		app.OnRecordAuthWithPasswordRequest("users").BindFunc(func(e *core.RecordAuthWithPasswordRequestEvent) error {
			if e.Record == nil || e.Record.GetString("auth_source") != "pam" {
				return e.Next()
			}
			if !e.Record.GetBool("active") {
				return e.BadRequestError("Failed to authenticate.", nil)
			}
			if err := checkPAM(e.RequestEvent, e.Record, "authenticate", e.Password); err != nil {
				return err
			}
			return apis.RecordAuthResponse(e.RequestEvent, e.Record, core.MFAMethodPassword, nil)
		})
		app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
			if e.Record.GetString("auth_source") == "pam" && e.AuthMethod != core.MFAMethodPassword {
				if err := checkPAM(e.RequestEvent, e.Record, "account", ""); err != nil {
					return err
				}
			}
			return e.Next()
		})
	})
}

func checkPAM(e *core.RequestEvent, record *core.Record, mode, password string) error {
	provider, _ := e.App.Store().Get(pamProviderKey).(pamauth.Provider)
	if provider == nil {
		return e.Error(503, "System authentication is unavailable.", nil)
	}
	username := record.GetString("username")
	if username == "" {
		return e.UnauthorizedError("Failed to authenticate.", nil)
	}
	host, _, _ := net.SplitHostPort(e.Request.RemoteAddr)
	result, err := provider.Run(e.Request.Context(), pamauth.Request{Mode: mode, Username: username, Password: password, RemoteHost: host})
	if errors.Is(err, pamauth.ErrDenied) || err == nil && (!result.OK || result.Username != username) {
		if mode == "authenticate" {
			return e.BadRequestError("Failed to authenticate.", nil)
		}
		return e.UnauthorizedError("请重新登录平台。", nil)
	}
	if err != nil {
		return e.Error(503, "System authentication is unavailable.", nil)
	}
	return nil
}
