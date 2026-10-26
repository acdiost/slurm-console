package migrations

import (
	"errors"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		audits, err := app.FindCollectionByNameOrId("operation_audits")
		if err != nil {
			return err
		}
		field := audits.Fields.GetByName("job_id").(*core.TextField)
		field.Required = false
		audits.Fields.Add(&core.TextField{Name: "node_name", Max: 255})
		audits.Indexes = append(audits.Indexes, "CREATE INDEX idx_operation_node ON operation_audits (cluster_id,node_name,status)")
		return app.Save(audits)
	}, func(core.App) error {
		return errors.New("node operation evidence must be retained; restore a consistent backup")
	})
}
