package migrations

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection := core.NewBaseCollection("pam_settings")
		collection.Fields.Add(
			&core.BoolField{Name: "has_override"},
			&core.BoolField{Name: "enabled"},
			&core.NumberField{Name: "revision", Required: true, OnlyInt: true, Min: floatPtr(1)},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(collection); err != nil {
			return err
		}
		record := core.NewRecord(collection)
		record.Id = "pamsettings0001"
		record.Set("revision", 1)
		// Absence of an override preserves existing manual PAM environment config.
		record.Set("has_override", false)
		return app.Save(record)
	}, func(core.App) error {
		return errors.New("PAM settings must be retained; restore a consistent backup")
	})
}
