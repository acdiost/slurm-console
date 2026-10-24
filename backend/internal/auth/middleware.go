package auth

import "github.com/pocketbase/pocketbase/core"

// RequireActiveUser complements RequireAuth("users") for already issued tokens.
func RequireActiveUser(e *core.RequestEvent) error {
	if e.Auth == nil || e.Auth.Collection().Name != "users" || !e.Auth.GetBool("active") {
		return e.UnauthorizedError("请重新登录平台。", nil)
	}
	if e.Auth.GetString("auth_source") == "pam" {
		if err := checkPAM(e, e.Auth, "account", ""); err != nil {
			return err
		}
	}
	return e.Next()
}
