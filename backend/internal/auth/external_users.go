package auth

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

func externalUserCommand(app core.App, source string, link bool) *cobra.Command {
	var email, username, name string
	label := strings.ToUpper(source)
	use, short := "create-"+source, "Create a platform account linked to a "+label+" identity (no password input)"
	if link {
		use, short = "link-"+source, "Switch an existing platform account to "+label+", preserving its ID and permissions"
	}
	cmd := &cobra.Command{
		Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			email, username = strings.TrimSpace(email), strings.TrimSpace(username)
			if source == "pam" && username == "root" || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,62}[$]?$`).MatchString(username) {
				return fmt.Errorf("username must be an eligible account name (1–64 characters; PAM excludes root)")
			}
			entryID := ""
			if source == "ldap" {
				provider := ldapProvider(app)
				if provider == nil {
					return fmt.Errorf("configure SLURM_CONSOLE_LDAP_CONFIG before linking LDAP accounts")
				}
				identity, err := provider.Lookup(cmd.Context(), username)
				if err != nil || identity.Username != username || identity.ID == "" {
					return fmt.Errorf("LDAP identity not found, not eligible, or directory unavailable")
				}
				entryID = identity.ID
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
			record.Set("auth_source", source)
			record.Set("username", username)
			record.Set("ldap_entry_id", entryID)
			// PocketBase requires a hash. It never verifies this random secret for
			// external accounts; neither the directory/system password nor this
			// random secret is retained.
			record.SetPassword(rand.Text() + rand.Text())
			record.RefreshTokenKey()
			if err := app.Save(record); err != nil {
				return fmt.Errorf("save %s mapping: %w", label, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s identity linked; permissions and account activation are unchanged. External account validity is checked at login.\n", label)
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Platform account email")
	cmd.Flags().StringVar(&username, "username", "", "Exact external username (PAM excludes root)")
	_ = cmd.MarkFlagRequired("email")
	_ = cmd.MarkFlagRequired("username")
	if !link {
		cmd.Flags().StringVar(&name, "name", "", "Display name")
	}
	return cmd
}
