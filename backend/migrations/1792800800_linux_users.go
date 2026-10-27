package migrations

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		permissions, err := app.FindCollectionByNameOrId("permissions")
		if err != nil {
			return err
		}
		p := core.NewRecord(permissions)
		p.Set("code", "linux_users.read")
		p.Set("name", "查看 Linux 系统用户")
		p.Set("scope", "cluster")
		if err = app.Save(p); err != nil {
			return err
		}
		bindings, err := app.FindCollectionByNameOrId("role_permissions")
		if err != nil {
			return err
		}
		for _, code := range []string{"super_admin", "cluster_admin", "operator", "viewer"} {
			role, err := app.FindFirstRecordByData("roles", "code", code)
			if err != nil {
				return err
			}
			b := core.NewRecord(bindings)
			b.Set("role", role.Id)
			b.Set("permission", p.Id)
			if err = app.Save(b); err != nil {
				return err
			}
		}
		return nil
	}, func(core.App) error {
		return errors.New("retain Linux user authorization; restore a pre-migration backup")
	})
}
