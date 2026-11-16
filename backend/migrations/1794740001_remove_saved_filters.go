package migrations

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("saved_filters")
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return app.Delete(collection)
	}, func(core.App) error {
		return errors.New("saved filters and their data were removed; restore a consistent backup")
	})
}
