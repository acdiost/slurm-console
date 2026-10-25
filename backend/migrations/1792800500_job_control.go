package migrations

import (
	"errors"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		clusters, err := app.FindCollectionByNameOrId("clusters")
		if err != nil {
			return err
		}
		identities := core.NewBaseCollection("slurm_identities")
		identities.Fields.Add(&core.RelationField{Name: "user", Required: true, CollectionId: users.Id, MaxSelect: 1, CascadeDelete: true}, &core.RelationField{Name: "cluster", Required: true, CollectionId: clusters.Id, MaxSelect: 1, CascadeDelete: true}, &core.TextField{Name: "username", Required: true, Max: 64}, &core.NumberField{Name: "uid", Required: true, OnlyInt: true, Min: floatPtr(1), Max: floatPtr(4294967295)}, &core.SelectField{Name: "scope", Required: true, MaxSelect: 1, Values: []string{"own", "all"}}, &core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		identities.Indexes = append(identities.Indexes, "CREATE UNIQUE INDEX idx_slurm_identity_user_cluster ON slurm_identities (user, cluster)", "CREATE UNIQUE INDEX idx_slurm_identity_uid_cluster ON slurm_identities (uid, cluster)")
		if err := app.Save(identities); err != nil {
			return err
		}
		audits := core.NewBaseCollection("operation_audits")
		audits.Fields.Add(&core.TextField{Name: "operation_key", Required: true, Max: 64}, &core.TextField{Name: "actor_id", Required: true, Max: 32}, &core.TextField{Name: "cluster_id", Required: true, Max: 32}, &core.NumberField{Name: "cluster_revision", OnlyInt: true}, &core.TextField{Name: "action", Required: true, Max: 64}, &core.TextField{Name: "job_id", Required: true, Max: 16}, &core.TextField{Name: "request_id", Max: 64}, &core.TextField{Name: "status", Required: true, Max: 32}, &core.TextField{Name: "message", Max: 500}, &core.TextField{Name: "reason", Max: 300}, &core.TextField{Name: "input_hash", Max: 64}, &core.TextField{Name: "fingerprint", Max: 64}, &core.JSONField{Name: "observed_states", MaxSize: 4096}, &core.AutodateField{Name: "created", OnCreate: true}, &core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		audits.Indexes = append(audits.Indexes, "CREATE UNIQUE INDEX idx_operation_key ON operation_audits (operation_key)", "CREATE INDEX idx_operation_job ON operation_audits (cluster_id,job_id,fingerprint,status)")
		return app.Save(audits)
	}, func(core.App) error {
		return errors.New("job identity and audit evidence must be retained; restore a consistent backup")
	})
}
