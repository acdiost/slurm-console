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
		users.Fields.Add(&core.TextField{Name: "username", Max: 64, Pattern: `^[A-Za-z_][A-Za-z0-9_.-]{0,62}[$]?$`}, &core.SelectField{Name: "auth_source", Required: true, MaxSelect: 1, Values: []string{"local", "pam"}})
		users.Indexes = append(users.Indexes, "CREATE UNIQUE INDEX idx_users_pam_username ON users (username) WHERE username != ''")
		users.PasswordAuth.IdentityFields = []string{"email", "username"}
		if err := app.Save(users); err != nil {
			return err
		}
		_, err = app.DB().NewQuery("UPDATE users SET auth_source = 'local' WHERE auth_source = ''").Execute()
		return err
	}, func(core.App) error {
		return errors.New("PAM identity mappings must not be removed automatically; restore a pre-migration backup")
	})
}
