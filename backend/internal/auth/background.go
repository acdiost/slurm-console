package auth

import (
	"context"
	"errors"
	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/pamauth"
)

// CheckBackgroundUser applies external account eligibility without a browser or password.
// Callers still resolve current RBAC separately and recheck before persisting results.
func CheckBackgroundUser(ctx context.Context, app core.App, user *core.Record) error {
	denied := errors.New("background user is unavailable")
	if user == nil || user.Collection().Name != "users" || !user.GetBool("active") {
		return denied
	}
	switch user.GetString("auth_source") {
	case "local":
		return nil
	case "pam":
		p, _ := app.Store().Get(pamProviderKey).(pamauth.Provider)
		name := user.GetString("username")
		if p == nil || name == "" {
			return denied
		}
		r, e := p.Run(ctx, pamauth.Request{Mode: "account", Username: name})
		if e != nil || !r.OK || r.Username != name {
			return denied
		}
		return nil
	case "ldap":
		p := ldapProvider(app)
		name, id := user.GetString("username"), user.GetString("ldap_entry_id")
		if p == nil || name == "" || id == "" {
			return denied
		}
		if p.Check(ctx, name, id) != nil {
			return denied
		}
		return nil
	default:
		return denied
	}
}
