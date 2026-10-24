package migrations

import (
	"errors"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

func init() {
	m.Register(func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		// PocketBase's initial users collection permits registration by default.
		// Platform accounts are provisioned by operators, with no public CRUD access.
		users.ListRule, users.ViewRule, users.CreateRule = nil, nil, nil
		users.UpdateRule, users.DeleteRule, users.ManageRule = nil, nil, nil
		users.Fields.Add(&core.BoolField{Name: "active"})
		users.AuthRule = types.Pointer("active = true")
		users.PasswordAuth.Enabled = true
		users.PasswordAuth.IdentityFields = []string{"email"}
		users.AuthToken.Duration = 3600
		users.OAuth2.Enabled, users.OTP.Enabled, users.MFA.Enabled = false, false, false
		// SMTP setup and account recovery are separate deployment concerns.
		users.AuthAlert.Enabled = false
		password := users.Fields.GetByName("password").(*core.PasswordField)
		password.Min, password.Max = 12, 71
		if err := app.Save(users); err != nil {
			return err
		}
		settings := app.Settings()
		settings.Meta.AppName = "Slurm Console"
		settings.RateLimits.Enabled = true
		settings.RateLimits.Rules = []core.RateLimitRule{
			{Label: "users:auth", MaxRequests: 5, Duration: 60},
			{Label: "*:auth", MaxRequests: 5, Duration: 60},
			{Label: "/api/", MaxRequests: 300, Duration: 10},
		}
		return app.Save(settings)
	}, func(app core.App) error {
		// Keep accounts and deny registration during rollback; removing active
		// would silently re-enable disabled accounts. Restore from backup instead.
		return errors.New("platform authentication migration cannot be rolled back automatically; restore a pre-migration backup")
	})
}
