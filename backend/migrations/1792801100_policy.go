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
			{"fairshare.read", "读取集群 Fairshare", "cluster", []string{"super_admin", "cluster_admin", "operator", "viewer"}},
			{"platform.qos.read", "读取共享记账库全部 QOS", "platform", []string{"super_admin"}},
			{"platform.qos.manage", "管理共享记账库 QOS", "platform", []string{"super_admin"}},
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
		return nil
	}, func(core.App) error {
		return errors.New("retain QOS operation evidence and permissions; restore a consistent backup")
	})
}
