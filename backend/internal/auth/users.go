package auth

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"slurm-console/backend/internal/audit"
)

// UsersCommand provisions platform accounts without putting passwords in argv.
func UsersCommand(app core.App) *cobra.Command {
	root := &cobra.Command{Use: "users", Short: "Manage platform accounts"}
	var email, name string
	create := &cobra.Command{
		Use: "create", Short: "Create a platform user (password from hidden terminal input or stdin)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.Bootstrap(); err != nil {
				return err
			}
			defer app.ResetBootstrapState()
			if err := app.RunAllMigrations(); err != nil {
				return err
			}
			var secret []byte
			var err error
			if input, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(input.Fd())) {
				fmt.Fprint(cmd.OutOrStdout(), "Password (12–71 characters): ")
				secret, err = term.ReadPassword(int(input.Fd()))
				fmt.Fprintln(cmd.OutOrStdout())
			} else {
				secret, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), 4097))
			}
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			defer clear(secret)
			if len(secret) > 4096 {
				return fmt.Errorf("password input too long")
			}
			password := strings.TrimSuffix(strings.TrimSuffix(string(secret), "\n"), "\r")
			collection, err := app.FindCollectionByNameOrId("users")
			if err != nil {
				return err
			}
			record := core.NewRecord(collection)
			record.SetEmail(strings.TrimSpace(email))
			record.Set("name", strings.TrimSpace(name))
			record.Set("active", true)
			record.Set("auth_source", "local")
			record.SetPassword(password)
			if err := audit.SaveLocal(app, record, audit.Event{Action: "users.create", TargetType: "user", Message: "本地命令创建平台账户。", Changes: []audit.Change{{Field: "authSource", After: "local"}}}); err != nil {
				return fmt.Errorf("create user: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Platform user created; cluster permissions are not assigned.")
			return nil
		},
	}
	create.Flags().StringVar(&email, "email", "", "Platform login email")
	create.Flags().StringVar(&name, "name", "", "Display name")
	_ = create.MarkFlagRequired("email")
	root.AddCommand(create)
	root.AddCommand(externalUserCommand(app, "pam", false), externalUserCommand(app, "pam", true))
	root.AddCommand(externalUserCommand(app, "ldap", false), externalUserCommand(app, "ldap", true), ldapCheckCommand(app))
	return root
}
