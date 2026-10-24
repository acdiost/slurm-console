package auth

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

func pamUserCommand(app core.App, link bool) *cobra.Command {
	var email, username, name string
	use, short := "create-pam", "Create a platform account linked to a non-root PAM identity (no password input)"
	if link {
		use, short = "link-pam", "Switch an existing platform account to PAM, preserving its ID and permissions"
	}
	cmd := &cobra.Command{
		Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			email, username = strings.TrimSpace(email), strings.TrimSpace(username)
			if username == "root" || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,62}[$]?$`).MatchString(username) {
				return fmt.Errorf("username must be a non-root Linux account name (1–64 characters)")
			}
			if err := app.Bootstrap(); err != nil {
				return err
			}
			defer app.ResetBootstrapState()
			if err := app.RunAllMigrations(); err != nil {
				return err
			}
			var record *core.Record
			if link {
				var err error
				record, err = app.FindAuthRecordByEmail("users", email)
				if err != nil {
					return fmt.Errorf("existing platform account not found")
				}
			} else {
				collection, err := app.FindCollectionByNameOrId("users")
				if err != nil {
					return err
				}
				record = core.NewRecord(collection)
				record.SetEmail(email)
				record.Set("name", strings.TrimSpace(name))
				record.Set("active", true)
			}
			record.Set("auth_source", "pam")
			record.Set("username", username)
			// PocketBase requires a hash. It never verifies this random secret for
			// PAM accounts; neither the system password nor this secret is retained.
			record.SetPassword(rand.Text() + rand.Text())
			record.RefreshTokenKey()
			if err := app.Save(record); err != nil {
				return fmt.Errorf("save PAM mapping: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "PAM identity linked; permissions and account activation are unchanged. System account validity is checked at login.")
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Platform account email")
	cmd.Flags().StringVar(&username, "username", "", "Exact non-root Linux/PAM username")
	_ = cmd.MarkFlagRequired("email")
	_ = cmd.MarkFlagRequired("username")
	if !link {
		cmd.Flags().StringVar(&name, "name", "", "Display name")
	}
	return cmd
}
