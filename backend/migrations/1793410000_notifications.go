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
		clusters, e := app.FindCollectionByNameOrId("clusters")
		if e != nil {
			return e
		}
		rules := core.NewBaseCollection("notification_rules")
		rules.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1, CascadeDelete: true}, &core.RelationField{Name: "cluster", Required: true, CollectionId: clusters.Id, MaxSelect: 1, CascadeDelete: true}, &core.TextField{Name: "name", Required: true, Max: 40}, &core.TextField{Name: "kind", Required: true, Max: 32}, &core.TextField{Name: "severity", Required: true, Max: 16}, &core.BoolField{Name: "enabled"}, &core.NumberField{Name: "threshold", Required: true, OnlyInt: true, Min: floatPtr(1), Max: floatPtr(10000)}, &core.NumberField{Name: "hold_minutes", OnlyInt: true, Min: floatPtr(0), Max: floatPtr(60)}, &core.NumberField{Name: "revision", Required: true, OnlyInt: true, Min: floatPtr(1)}, &core.TextField{Name: "state", Required: true, Max: 16}, &core.BoolField{Name: "incident_active"}, &core.TextField{Name: "incident_id", Max: 64}, &core.DateField{Name: "pending_since"}, &core.DateField{Name: "last_checked"}, &core.TextField{Name: "summary", Max: 300}, &core.NumberField{Name: "affected_count", OnlyInt: true}, &core.JSONField{Name: "samples", MaxSize: 2048}, &core.AutodateField{Name: "created", OnCreate: true}, &core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		rules.Indexes = append(rules.Indexes, "CREATE INDEX idx_notification_rules_owner ON notification_rules (owner)", "CREATE INDEX idx_notification_rules_worker ON notification_rules (enabled,last_checked)")
		if e := app.Save(rules); e != nil {
			return e
		}
		n := core.NewBaseCollection("notifications")
		n.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1, CascadeDelete: true}, &core.RelationField{Name: "cluster", Required: true, CollectionId: clusters.Id, MaxSelect: 1, CascadeDelete: true}, &core.TextField{Name: "rule_id", Required: true, Max: 64}, &core.TextField{Name: "rule_name", Required: true, Max: 40}, &core.TextField{Name: "kind", Required: true, Max: 32}, &core.TextField{Name: "severity", Required: true, Max: 16}, &core.TextField{Name: "phase", Required: true, Max: 16}, &core.TextField{Name: "incident_id", Required: true, Max: 64}, &core.TextField{Name: "message", Required: true, Max: 300}, &core.NumberField{Name: "affected_count", OnlyInt: true}, &core.JSONField{Name: "samples", MaxSize: 2048}, &core.BoolField{Name: "read"}, &core.AutodateField{Name: "created", OnCreate: true})
		n.Indexes = append(n.Indexes, "CREATE UNIQUE INDEX idx_notification_incident_phase ON notifications (rule_id,incident_id,phase)", "CREATE INDEX idx_notifications_owner ON notifications (owner,read,created)", "CREATE INDEX idx_notifications_retention ON notifications (created)")
		return app.Save(n)
	}, func(core.App) error {
		return errors.New("notification history must be retained; restore a consistent backup")
	})
}
