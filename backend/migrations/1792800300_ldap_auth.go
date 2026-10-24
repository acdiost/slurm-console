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
		users.Fields.GetByName("auth_source").(*core.SelectField).Values = []string{"local", "pam", "ldap"}
		users.Fields.Add(&core.TextField{Name: "ldap_entry_id", Max: 2048, Hidden: true})
		users.Indexes = append(users.Indexes, "CREATE UNIQUE INDEX idx_users_ldap_entry_id ON users (ldap_entry_id) WHERE auth_source = 'ldap' AND ldap_entry_id != ''")
		return app.Save(users)
	}, func(core.App) error {
		return errors.New("LDAP identity mappings must not be removed automatically; restore a pre-migration backup")
	})
}
