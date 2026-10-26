package migrations

import (
	"errors"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("operation_audits")
		if err != nil {
			return err
		}
		c.Fields.Add(&core.TextField{Name: "actor_label", Max: 200}, &core.TextField{Name: "actor_source", Max: 32}, &core.TextField{Name: "cluster_name", Max: 100}, &core.TextField{Name: "cluster_slug", Max: 64}, &core.TextField{Name: "target_type", Max: 32}, &core.TextField{Name: "target_id", Max: 255}, &core.JSONField{Name: "changes", MaxSize: 16384})
		c.Indexes = append(c.Indexes, "CREATE INDEX idx_audit_created ON operation_audits (created,id)", "CREATE INDEX idx_audit_cluster_created ON operation_audits (cluster_id,created,id)", "CREATE INDEX idx_audit_actor_created ON operation_audits (actor_id,created,id)", "CREATE INDEX idx_audit_action_created ON operation_audits (action,created,id)")
		// Platform events need not have a cluster. Text IDs retain history after deletion.
		c.Fields.GetByName("cluster_id").(*core.TextField).Required = false
		return app.Save(c)
	}, func(core.App) error {
		return errors.New("audit evidence must be retained; restore a consistent backup")
	})
}
