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
		c := core.NewBaseCollection("saved_filters")
		c.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1, CascadeDelete: true}, &core.TextField{Name: "module", Required: true, Max: 32}, &core.RelationField{Name: "cluster", CollectionId: clusters.Id, MaxSelect: 1, CascadeDelete: true}, &core.TextField{Name: "name", Required: true, Max: 40}, &core.TextField{Name: "name_key", Required: true, Max: 80}, &core.JSONField{Name: "conditions", MaxSize: 4096}, &core.BoolField{Name: "is_default"}, &core.NumberField{Name: "revision", Required: true, OnlyInt: true, Min: floatPtr(1)}, &core.AutodateField{Name: "created", OnCreate: true}, &core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		c.Indexes = append(c.Indexes, "CREATE UNIQUE INDEX idx_saved_filter_name ON saved_filters (owner,module,cluster,name_key)", "CREATE UNIQUE INDEX idx_saved_filter_default ON saved_filters (owner,module,cluster) WHERE is_default=1", "CREATE INDEX idx_saved_filter_owner ON saved_filters (owner)")
		return app.Save(c)
	}, func(core.App) error {
		return errors.New("saved user filters must be retained; restore a consistent backup")
	})
}
