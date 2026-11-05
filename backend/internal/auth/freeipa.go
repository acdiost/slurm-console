package auth

import (
	"context"
	"errors"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
	"os"
	"slurm-console/backend/internal/freeipa"
)

func FreeIPA(app core.App) freeipa.Provider {
	p, _ := app.Store().Get(freeipa.StoreKey).(freeipa.Provider)
	return p
}
func ConfigureFreeIPA(app core.App) error {
	var p freeipa.Provider
	if path := os.Getenv("SLURM_CONSOLE_FREEIPA_CONFIG"); path != "" {
		v, e := freeipa.Load(path)
		if e != nil {
			return e
		}
		p = v
	}
	RegisterFreeIPA(app, p)
	return nil
}
func RegisterFreeIPA(app core.App, p freeipa.Provider) {
	app.Store().Set(freeipa.StoreKey, p)
	app.OnRecordAuthWithPasswordRequest("users").BindFunc(func(e *core.RecordAuthWithPasswordRequestEvent) error {
		if e.Record == nil || e.Record.GetString("auth_source") != "freeipa" {
			return e.Next()
		}
		if !e.Record.GetBool("active") {
			return e.BadRequestError("Failed to authenticate.", nil)
		}
		if err := checkFreeIPA(e.RequestEvent, e.Record, e.Password, true); err != nil {
			return err
		}
		return apis.RecordAuthResponse(e.RequestEvent, e.Record, core.MFAMethodPassword, nil)
	})
	app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
		if e.Record.GetString("auth_source") == "freeipa" && e.AuthMethod != core.MFAMethodPassword {
			if err := checkFreeIPA(e.RequestEvent, e.Record, "", false); err != nil {
				return err
			}
		}
		return e.Next()
	})
}
func freeIPAIdentity(ctx context.Context, app core.App, name, id string) error {
	p := FreeIPA(app)
	if p == nil {
		return freeipa.ErrUnavailable
	}
	u, e := p.User(ctx, name)
	if errors.Is(e, freeipa.ErrNotFound) {
		return freeipa.ErrDenied
	}
	if e != nil {
		return e
	}
	if id == "" || u.Username != name || u.ID != id || !u.Enabled || u.AccountExpired || u.PasswordExpired {
		return freeipa.ErrDenied
	}
	return nil
}
func checkFreeIPA(e *core.RequestEvent, r *core.Record, password string, login bool) error {
	p := FreeIPA(e.App)
	if p == nil {
		return e.Error(503, "FreeIPA 认证暂不可用。", nil)
	}
	var err error
	if login {
		err = p.Authenticate(e.Request.Context(), r.GetString("username"), r.GetString("freeipa_entry_id"), password)
	} else {
		err = freeIPAIdentity(e.Request.Context(), e.App, r.GetString("username"), r.GetString("freeipa_entry_id"))
	}
	if errors.Is(err, freeipa.ErrDenied) || errors.Is(err, freeipa.ErrNotFound) {
		if login {
			return e.BadRequestError("Failed to authenticate.", nil)
		}
		return e.UnauthorizedError("FreeIPA 身份已失效，请重新登录。", nil)
	}
	if err != nil {
		return e.Error(503, "FreeIPA 认证暂不可用。", nil)
	}
	return nil
}
func freeIPACheckCommand(app core.App) *cobra.Command {
	return &cobra.Command{Use: "check-freeipa", Short: "Check FreeIPA verified HTTPS and service credentials", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		p := FreeIPA(app)
		if p == nil {
			return errors.New("FreeIPA is not configured")
		}
		if _, e := p.Groups(cmd.Context(), ""); e != nil {
			return errors.New("FreeIPA check failed: verify HTTPS trust, service credentials and directory access")
		}
		cmd.Println("FreeIPA HTTPS, service authentication and directory read passed.")
		return nil
	}}
}
