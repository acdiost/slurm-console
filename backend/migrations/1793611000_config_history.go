package migrations

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"slurm-console/backend/internal/confighistory"
)

func init() {
	m.Register(func(app core.App) error {
		c := core.NewBaseCollection("cluster_config_versions")
		c.Fields.Add(&core.TextField{Name: "cluster_id", Required: true, Min: 15, Max: 15}, &core.NumberField{Name: "revision", OnlyInt: true, Min: floatPtr(0)}, &core.NumberField{Name: "schema_version", Required: true, OnlyInt: true, Min: floatPtr(1)}, &core.SelectField{Name: "kind", Required: true, MaxSelect: 1, Values: []string{"baseline", "change"}}, &core.TextField{Name: "action", Required: true, Max: 64}, &core.TextField{Name: "actor_id", Max: 64}, &core.TextField{Name: "actor_label", Max: 200}, &core.TextField{Name: "actor_source", Max: 32}, &core.TextField{Name: "operation_id", Max: 64}, &core.TextField{Name: "request_id", Max: 64}, &core.JSONField{Name: "snapshot", Required: true, MaxSize: 16384}, &core.AutodateField{Name: "created", OnCreate: true})
		c.Indexes = append(c.Indexes, "CREATE UNIQUE INDEX idx_cluster_config_version ON cluster_config_versions (cluster_id,revision)")
		if e := app.Save(c); e != nil {
			return e
		}
		last := ""
		for {
			rows, e := app.FindRecordsByFilter("clusters", "id>{:last}", "id", 200, 0, dbx.Params{"last": last})
			if e != nil {
				return e
			}
			for _, r := range rows {
				if e := confighistory.Capture(app, r, nil); e != nil {
					return e
				}
				last = r.Id
			}
			if len(rows) < 200 {
				break
			}
		}
		return nil
	}, func(core.App) error {
		return errors.New("configuration versions must be retained; restore a consistent backup")
	})
}
