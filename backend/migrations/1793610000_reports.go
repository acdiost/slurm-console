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
		p.Set("code", "reports.read")
		p.Set("name", "读取资源分配时长报表")
		p.Set("scope", "cluster")
		if e = app.Save(p); e != nil {
			return e
		}
		bindings, e := app.FindCollectionByNameOrId("role_permissions")
		if e != nil {
			return e
		}
		for _, code := range []string{"super_admin", "cluster_admin", "operator", "viewer"} {
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
		return errors.New("report permission bindings must be retained; restore a consistent backup")
	})
}
