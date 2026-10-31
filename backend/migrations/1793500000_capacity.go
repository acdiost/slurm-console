package migrations

import (
	"errors"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		pcol, e := app.FindCollectionByNameOrId("permissions")
		if e != nil {
			return e
		}
		p := core.NewRecord(pcol)
		p.Set("code", "capacity.read")
		p.Set("name", "读取集群容量分析与汇总历史")
		p.Set("scope", "cluster")
		if e := app.Save(p); e != nil {
			return e
		}
		bindings, e := app.FindCollectionByNameOrId("role_permissions")
		if e != nil {
			return e
		}
		for _, code := range []string{"super_admin", "cluster_admin", "operator", "viewer"} {
			role, e := app.FindFirstRecordByData("roles", "code", code)
			if e != nil {
				return e
			}
			r := core.NewRecord(bindings)
			r.Set("role", role.Id)
			r.Set("permission", p.Id)
			if e := app.Save(r); e != nil {
				return e
			}
		}
		clusters, e := app.FindCollectionByNameOrId("clusters")
		if e != nil {
			return e
		}
		c := core.NewBaseCollection("capacity_samples")
		c.Fields.Add(&core.RelationField{Name: "cluster", Required: true, CollectionId: clusters.Id, MaxSelect: 1, CascadeDelete: true}, &core.TextField{Name: "source_key", Required: true, Min: 64, Max: 64}, &core.NumberField{Name: "bucket", Required: true, OnlyInt: true, Min: floatPtr(1)}, &core.DateField{Name: "observed_at", Required: true}, &core.JSONField{Name: "data", Required: true, MaxSize: 4096}, &core.AutodateField{Name: "created", OnCreate: true})
		c.Indexes = append(c.Indexes, "CREATE UNIQUE INDEX idx_capacity_sample_slot ON capacity_samples (cluster,source_key,bucket)", "CREATE INDEX idx_capacity_sample_retention ON capacity_samples (bucket,cluster)", "CREATE INDEX idx_capacity_sample_cluster ON capacity_samples (cluster,bucket)")
		return app.Save(c)
	}, func(core.App) error {
		return errors.New("capacity history and permission bindings must be retained; restore a consistent backup")
	})
}
