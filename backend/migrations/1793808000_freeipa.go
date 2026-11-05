package migrations

import (
	"errors"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		users, e := app.FindCollectionByNameOrId("users")
		if e != nil {
			return e
		}
		users.Fields.GetByName("auth_source").(*core.SelectField).Values = []string{"local", "pam", "ldap", "freeipa"}
		users.Fields.Add(&core.TextField{Name: "freeipa_entry_id", Max: 128, Hidden: true})
		users.Indexes = append(users.Indexes, "CREATE UNIQUE INDEX idx_users_freeipa_entry ON users (freeipa_entry_id) WHERE auth_source='freeipa' AND freeipa_entry_id!=''")
		if e = app.Save(users); e != nil {
			return e
		}
		roles, e := app.FindCollectionByNameOrId("roles")
		if e != nil {
			return e
		}
		clusters, e := app.FindCollectionByNameOrId("clusters")
		if e != nil {
			return e
		}
		c := core.NewBaseCollection("directory_role_mappings")
		c.Fields.Add(&core.TextField{Name: "group_name", Required: true, Max: 64}, &core.TextField{Name: "group_id", Required: true, Max: 128, Hidden: true}, &core.RelationField{Name: "role", Required: true, CollectionId: roles.Id, MaxSelect: 1, CascadeDelete: true}, &core.RelationField{Name: "cluster", CollectionId: clusters.Id, MaxSelect: 1, CascadeDelete: true}, &core.AutodateField{Name: "created", OnCreate: true}, &core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		c.Indexes = append(c.Indexes, "CREATE UNIQUE INDEX idx_directory_role_mapping ON directory_role_mappings (group_id,role,cluster)")
		if e = app.Save(c); e != nil {
			return e
		}
		permissions, e := app.FindCollectionByNameOrId("permissions")
		if e != nil {
			return e
		}
		bindings, e := app.FindCollectionByNameOrId("role_permissions")
		if e != nil {
			return e
		}
		admin, e := app.FindFirstRecordByData("roles", "code", "super_admin")
		if e != nil {
			return e
		}
		for _, v := range [][2]string{{"platform.directory.read", "读取 FreeIPA 目录"}, {"platform.directory.manage", "管理 FreeIPA 目录"}} {
			p := core.NewRecord(permissions)
			p.Set("code", v[0])
			p.Set("name", v[1])
			p.Set("scope", "platform")
			if e = app.Save(p); e != nil {
				return e
			}
			b := core.NewRecord(bindings)
			b.Set("role", admin.Id)
			b.Set("permission", p.Id)
			if e = app.Save(b); e != nil {
				return e
			}
		}
		return nil
	}, func(core.App) error {
		return errors.New("FreeIPA identities, mappings and audit history must be retained; restore a consistent backup")
	})
}
