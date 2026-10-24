package auth

import (
	"errors"
	"os"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
	"slurm-console/backend/internal/ldapauth"
)

const ldapProviderKey = "platform.ldapProvider"

func ConfigureLDAP(app core.App) error {
	path := os.Getenv("SLURM_CONSOLE_LDAP_CONFIG")
	if path == "" {
		RegisterLDAP(app, nil)
		return nil
	}
	provider, err := ldapauth.Load(path)
	if err != nil {
		return err
	}
	// Configuration errors fail startup; a remote outage does not disable PAM or
	// local recovery accounts. LDAP requests fail closed with 503 during outages.
	RegisterLDAP(app, provider)
	return nil
}

func RegisterLDAP(app core.App, provider ldapauth.Provider) {
	app.Store().Set(ldapProviderKey, provider)
	app.OnRecordAuthWithPasswordRequest("users").BindFunc(func(e *core.RecordAuthWithPasswordRequestEvent) error {
		if e.Record == nil || e.Record.GetString("auth_source") != "ldap" {
			return e.Next()
		}
		if !e.Record.GetBool("active") {
			return e.BadRequestError("Failed to authenticate.", nil)
		}
		if err := checkLDAP(e.RequestEvent, e.Record, e.Password, true); err != nil {
			return err
		}
		return apis.RecordAuthResponse(e.RequestEvent, e.Record, core.MFAMethodPassword, nil)
	})
	app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
		if e.Record.GetString("auth_source") == "ldap" && e.AuthMethod != core.MFAMethodPassword {
			if err := checkLDAP(e.RequestEvent, e.Record, "", false); err != nil {
				return err
			}
		}
		return e.Next()
	})
}

func ldapProvider(app core.App) ldapauth.Provider {
	provider, _ := app.Store().Get(ldapProviderKey).(ldapauth.Provider)
	return provider
}

func checkLDAP(e *core.RequestEvent, record *core.Record, password string, login bool) error {
	provider := ldapProvider(e.App)
	if provider == nil {
		return e.Error(503, "Directory authentication is unavailable.", nil)
	}
	username, id := record.GetString("username"), record.GetString("ldap_entry_id")
	var err error
	if username == "" || id == "" {
		err = ldapauth.ErrDenied
	} else if login {
		err = provider.Authenticate(e.Request.Context(), username, id, password)
	} else {
		err = provider.Check(e.Request.Context(), username, id)
	}
	if errors.Is(err, ldapauth.ErrDenied) {
		if login {
			return e.BadRequestError("Failed to authenticate.", nil)
		}
		return e.UnauthorizedError("请重新登录平台。", nil)
	}
	if err != nil {
		return e.Error(503, "Directory authentication is unavailable.", nil)
	}
	return nil
}

func ldapCheckCommand(app core.App) *cobra.Command {
	return &cobra.Command{Use: "check-ldap", Short: "Check verified TLS, directory service bind and base DN read access", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		provider := ldapProvider(app)
		if provider == nil {
			return errors.New("LDAP is not configured")
		}
		if err := provider.Probe(cmd.Context()); err != nil {
			return errors.New("LDAP check failed: verify TLS trust, service credentials and base DN access")
		}
		cmd.Println("LDAP TLS, service bind and base DN check passed.")
		return nil
	}}
}
