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
		c := core.NewBaseCollection("operation_templates")
		c.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1, CascadeDelete: true}, &core.RelationField{Name: "cluster", Required: true, CollectionId: clusters.Id, MaxSelect: 1, CascadeDelete: true}, &core.TextField{Name: "name", Required: true, Max: 40}, &core.TextField{Name: "name_key", Required: true, Max: 80}, &core.SelectField{Name: "action", Required: true, MaxSelect: 1, Values: []string{"jobs.cancel", "nodes.drain", "nodes.resume"}}, &core.TextField{Name: "description", Max: 500}, &core.TextField{Name: "default_reason", Max: 220}, &core.JSONField{Name: "checklist", MaxSize: 8192}, &core.NumberField{Name: "revision", Required: true, OnlyInt: true, Min: floatPtr(1)}, &core.AutodateField{Name: "created", OnCreate: true}, &core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		c.Indexes = append(c.Indexes, "CREATE UNIQUE INDEX idx_operation_template_name ON operation_templates (owner,cluster,name_key)", "CREATE INDEX idx_operation_template_owner ON operation_templates (owner,cluster)")
		return app.Save(c)
	}, func(core.App) error {
		return errors.New("operation templates must be retained; restore a consistent backup")
	})
}
