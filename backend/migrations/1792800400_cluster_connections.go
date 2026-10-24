package migrations

import (
	"errors"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		clusters, err := app.FindCollectionByNameOrId("clusters")
		if err != nil {
			return err
		}
		clusters.Fields.Add(&core.TextField{Name: "connection_ref", Max: 64}, &core.TextField{Name: "api_version", Max: 16}, &core.BoolField{Name: "enabled"}, &core.NumberField{Name: "timeout_seconds", OnlyInt: true, Min: floatPtr(0), Max: floatPtr(30)}, &core.NumberField{Name: "revision", OnlyInt: true, Min: floatPtr(0)}, &core.JSONField{Name: "last_probe", MaxSize: 16384})
		return app.Save(clusters)
	}, func(core.App) error {
		return errors.New("connection configuration must not be removed automatically; restore a pre-migration backup")
	})
}
func floatPtr(value float64) *float64 { return &value }
