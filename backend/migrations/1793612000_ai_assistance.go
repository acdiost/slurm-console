package migrations

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		c, e := app.FindCollectionByNameOrId("permissions")
		if e != nil {
			return e
		}
		p := core.NewRecord(c)
		p.Set("code", "ai.explain")
		p.Set("name", "确认发送脱敏证据并生成 AI 作业解释")
		p.Set("scope", "cluster")
		if e = app.Save(p); e != nil {
			return e
		}
		bindings, e := app.FindCollectionByNameOrId("role_permissions")
		if e != nil {
			return e
		}
		for _, code := range []string{"super_admin", "cluster_admin", "operator"} {
			role, e := app.FindFirstRecordByData("roles", "code", code)
			if e != nil {
				return e
			}
			r := core.NewRecord(bindings)
			r.Set("role", role.Id)
			r.Set("permission", p.Id)
			if e = app.Save(r); e != nil {
				return e
			}
		}
		return nil
	}, func(core.App) error {
		return errors.New("AI permission bindings must be retained; restore a consistent backup")
	})
}
