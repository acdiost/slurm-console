package auth

import "github.com/pocketbase/pocketbase/core"

// RequireActiveUser complements RequireAuth("users") for already issued tokens.
func RequireActiveUser(e *core.RequestEvent) error {
	if e.Auth == nil || e.Auth.Collection().Name != "users" || !e.Auth.GetBool("active") {
		return e.UnauthorizedError("请重新登录平台。", nil)
	}
	return e.Next()
}
