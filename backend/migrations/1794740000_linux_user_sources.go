package migrations

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		// Nil API rules keep source credentials inaccessible through record APIs.
		c := core.NewBaseCollection("linux_user_sources")
		c.Fields.Add(
			&core.RelationField{Name: "cluster", CollectionId: "", MaxSelect: 1, Required: true, CascadeDelete: true},
			&core.BoolField{Name: "configured"},
			&core.NumberField{Name: "revision", Required: true, OnlyInt: true, Min: floatPtr(1)},
			&core.TextField{Name: "label", Max: 160},
			&core.SelectField{Name: "transport", Values: []string{"local", "ssh"}, MaxSelect: 1, Required: true},
			&core.TextField{Name: "host", Max: 253},
			&core.NumberField{Name: "port", OnlyInt: true},
			&core.TextField{Name: "username", Max: 32},
			&core.TextField{Name: "private_key", Max: 65536, Hidden: true},
			&core.TextField{Name: "known_hosts", Max: 1048576},
			&core.BoolField{Name: "manage_local"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		clusters, err := app.FindCollectionByNameOrId("clusters")
		if err != nil {
			return err
		}
		c.Fields.GetByName("cluster").(*core.RelationField).CollectionId = clusters.Id
		c.AddIndex("idx_linux_user_source_cluster", true, "cluster", "")
		if err = app.Save(c); err != nil {
			return err
		}
		state := core.NewBaseCollection("linux_user_source_import")
		state.Fields.Add(&core.BoolField{Name: "completed"})
		if err = app.Save(state); err != nil {
			return err
		}
		r := core.NewRecord(state)
		r.Id = "linuxsources001"
		return app.Save(r)
	}, func(core.App) error {
		return errors.New("Linux identity sources and import state must be retained; restore a consistent backup")
	})
}
