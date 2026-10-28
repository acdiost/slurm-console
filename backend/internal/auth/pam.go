package auth

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/pamauth"
)

const pamProviderKey = "platform.pamProvider"

func ConfigurePAM(app core.App) error {
	enabled := false
	if raw := os.Getenv("SLURM_CONSOLE_PAM_ENABLED"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return errors.New("invalid SLURM_CONSOLE_PAM_ENABLED")
		}
		enabled = value
	}
	if !enabled {
		RegisterPAM(app, nil)
		return nil
	}
	path := os.Getenv("SLURM_CONSOLE_PAM_HELPER")
	if path == "" {
		path = "/usr/local/libexec/slurm-console-pam-helper"
	}
	sudo := os.Geteuid() != 0
	if raw := os.Getenv("SLURM_CONSOLE_PAM_SUDO"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return errors.New("invalid SLURM_CONSOLE_PAM_SUDO")
		}
		sudo = value && os.Geteuid() != 0
	}
	provider, err := pamauth.NewProcess(path, sudo)
	if err != nil {
		return err
	}
	if _, err := provider.Run(context.Background(), pamauth.Request{Mode: "probe"}); err != nil {
		return errors.New("PAM helper is unavailable: check Linux CGO build, root-owned policy and execution permissions")
	}
	RegisterPAM(app, provider)
	return nil
}

func RegisterPAM(app core.App, provider pamauth.Provider) {
	app.Store().Set(pamProviderKey, provider)
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
