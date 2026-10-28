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
		bindings, err := app.FindCollectionByNameOrId("role_permissions")
		if err != nil {
			return err
		}
		for _, action := range []string{"create", "lock", "unlock"} {
			p := core.NewRecord(permissions)
			p.Set("code", "linux_users."+action)
			p.Set("scope", "cluster")
			p.Set("name", map[string]string{"create": "创建本地账户", "lock": "锁定本地账户", "unlock": "解锁本地账户"}[action])
			if err := app.Save(p); err != nil {
				return err
			}
			for _, code := range []string{"super_admin", "cluster_admin"} {
				role, err := app.FindFirstRecordByData("roles", "code", code)
				if err != nil {
					return err
				}
				b := core.NewRecord(bindings)
				b.Set("role", role.Id)
				b.Set("permission", p.Id)
				if err := app.Save(b); err != nil {
					return err
				}
			}
		}
		c, err := app.FindCollectionByNameOrId("operation_audits")
		if err != nil {
			return err
		}
		c.Indexes = append(c.Indexes, "CREATE INDEX idx_linux_user_operation ON operation_audits (cluster_id,target_type,target_id,status)")
		return app.Save(c)
	}, func(core.App) error {
		return errors.New("retain local account operation evidence; restore a consistent application backup")
	})
}
