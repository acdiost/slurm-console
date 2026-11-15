package migrations

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		c := core.NewBaseCollection("managed_cluster_profiles")
		c.Fields.Add(
			&core.TextField{Name: "ref", Required: true, Max: 63},
			&core.JSONField{Name: "profile", Required: true, MaxSize: 8192},
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		c.AddIndex("idx_managed_cluster_profile_ref", true, "ref", "")
		return app.Save(c)
	}, func(core.App) error {
		return errors.New("managed cluster connections must be retained; restore a consistent backup")
	})
}
