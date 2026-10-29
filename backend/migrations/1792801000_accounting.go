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
		for _, definition := range []struct {
			Code, Name, Scope string
			Roles             []string
		}{
			{"slurm_users.read", "读取 Slurm 记账用户", "cluster", []string{"super_admin", "cluster_admin", "operator", "viewer"}},
			{"platform.accounting.manage", "管理全局 Slurm 记账身份", "platform", []string{"super_admin"}},
		} {
			p := core.NewRecord(permissions)
			p.Set("code", definition.Code)
			p.Set("name", definition.Name)
			p.Set("scope", definition.Scope)
			if err := app.Save(p); err != nil {
				return err
			}
			for _, code := range definition.Roles {
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
		if field, ok := c.Fields.GetByName("target_id").(*core.TextField); ok {
			field.Max = 512
		} else {
			return errors.New("audit target field is unavailable")
		}
		c.Fields.Add(&core.JSONField{Name: "accounting_spec", MaxSize: 16384})
		c.Indexes = append(c.Indexes, "CREATE INDEX idx_accounting_target ON operation_audits (target_type,target_id,status)")
		return app.Save(c)
	}, func(core.App) error {
		return errors.New("retain accounting operation evidence; restore a consistent backup")
	})
}
