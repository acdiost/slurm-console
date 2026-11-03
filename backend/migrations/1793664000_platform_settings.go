package migrations

import (
	"errors"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"slurm-console/backend/internal/platformsettings"
)

func init() {
	m.Register(func(app core.App) error {
		c := core.NewBaseCollection("platform_settings")
		c.Fields.Add(&core.TextField{Name: "name", Required: true, Min: 2, Max: 64}, &core.TextField{Name: "description", Max: 240}, &core.TextField{Name: "login_notice", Max: 1000}, &core.TextField{Name: "support_contact", Max: 160}, &core.NumberField{Name: "revision", Required: true, OnlyInt: true, Min: floatPtr(1)}, &core.JSONField{Name: "ai_override", MaxSize: 16384}, &core.AutodateField{Name: "created", OnCreate: true}, &core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		if e := app.Save(c); e != nil {
			return e
		}
		r := core.NewRecord(c)
		r.Id = platformsettings.ID
		platformsettings.Set(r, platformsettings.Identity{Name: "Slurm Console", Description: "连接集群、洞察资源、管理调度。\n为团队提供清晰、一致的算力运维工作空间。"})
		r.Set("revision", 1)
		if e := app.Save(r); e != nil {
			return e
		}
		users, e := app.FindCollectionByNameOrId("users")
		if e != nil {
			return e
		}
		users.Fields.Add(&core.NumberField{Name: "management_revision", OnlyInt: true, Min: floatPtr(0)})
		if e = app.Save(users); e != nil {
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
		role, e := app.FindFirstRecordByData("roles", "code", "super_admin")
		if e != nil {
			return e
		}
		for _, item := range [][2]string{{"platform.settings.read", "读取平台设置"}, {"platform.settings.manage", "管理平台设置"}} {
			p := core.NewRecord(permissions)
			p.Set("code", item[0])
			p.Set("name", item[1])
			p.Set("scope", "platform")
			if e = app.Save(p); e != nil {
				return e
			}
			b := core.NewRecord(bindings)
			b.Set("role", role.Id)
			b.Set("permission", p.Id)
			if e = app.Save(b); e != nil {
				return e
			}
		}
		return nil
	}, func(core.App) error {
		return errors.New("platform settings and user revisions must be retained; restore a consistent backup")
	})
}
