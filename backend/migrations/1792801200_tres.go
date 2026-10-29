package migrations

import (
	"errors"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		pcol, err := app.FindCollectionByNameOrId("permissions")
		if err != nil {
			return err
		}
		bcol, err := app.FindCollectionByNameOrId("role_permissions")
		if err != nil {
			return err
		}
		for _, d := range []struct {
			Code, Name string
			Roles      []string
		}{{"tres.read", "读取 TRES 资源与限额", []string{"super_admin", "cluster_admin", "operator", "viewer"}}, {"tres.update", "管理 TRES 本层限额", []string{"super_admin", "cluster_admin"}}} {
			p := core.NewRecord(pcol)
			p.Set("code", d.Code)
			p.Set("name", d.Name)
			p.Set("scope", "cluster")
			if err := app.Save(p); err != nil {
				return err
			}
			for _, code := range d.Roles {
				role, err := app.FindFirstRecordByData("roles", "code", code)
				if err != nil {
					return err
				}
				b := core.NewRecord(bcol)
				b.Set("role", role.Id)
				b.Set("permission", p.Id)
				if err := app.Save(b); err != nil {
					return err
				}
			}
		}
		return nil
	}, func(core.App) error { return errors.New("retain TRES operation evidence; restore a consistent backup") })
}
