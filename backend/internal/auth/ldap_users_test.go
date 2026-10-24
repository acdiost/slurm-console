package auth_test

import (
	"context"
	"io"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/ldapauth"
	"slurm-console/backend/internal/rbac"
)

type directoryFixture struct{}

func (*directoryFixture) Lookup(_ context.Context, username string) (ldapauth.Identity, error) {
	return ldapauth.Identity{Username: username, ID: "unique-directory-entry"}, nil
}
func (*directoryFixture) Authenticate(context.Context, string, string, string) error { return nil }
func (*directoryFixture) Check(context.Context, string, string) error                { return nil }
func (*directoryFixture) Probe(context.Context) error                                { return nil }

func TestLDAPLinkPreservesGrantsAndInvalidatesPriorSession(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	auth.RegisterLDAP(app, &directoryFixture{})
	run := func(args ...string) error {
		cmd := auth.UsersCommand(app)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs(args)
		return cmd.Execute()
	}
	if err := run("create-pam", "--email", "ldap@example.com", "--username", "ldap_fixture", "--name", "Directory fixture"); err != nil {
		t.Fatal(err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail("users", "ldap@example.com")
	if err != nil {
		t.Fatal(err)
	}
	oldID := record.Id
	token, err := record.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rbac.Grant(app, record.Id, "super_admin", ""); err != nil {
		t.Fatal(err)
	}
	_ = app.ResetBootstrapState()
	if err := run("link-ldap", "--email", "ldap@example.com", "--username", "ldap_fixture"); err != nil {
		t.Fatal(err)
	}
	if err := run("create-ldap", "--email", "duplicate@example.com", "--username", "another_fixture"); err == nil {
		t.Fatal("same directory entry linked twice")
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	record, err = app.FindAuthRecordByEmail("users", "ldap@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if record.Id != oldID || record.GetString("auth_source") != "ldap" || record.GetString("ldap_entry_id") != "unique-directory-entry" || record.GetString("name") != "Directory fixture" || !record.GetBool("active") {
		t.Fatal("identity link lost original record fields")
	}
	if _, err := app.FindAuthRecordByToken(token, core.TokenTypeAuth); err == nil {
		t.Fatal("prior session still valid")
	}
	access, err := rbac.Resolve(app, record.Id)
	if err != nil || len(access.PlatformRoles) != 1 {
		t.Fatal("grant lost on LDAP link")
	}
}

func TestLDAPProvisioningRequiresConfiguredDirectory(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	auth.RegisterLDAP(app, nil)
	cmd := auth.UsersCommand(app)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"create-ldap", "--email", "ldap@example.com", "--username", "ldap_fixture"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("created an unverified LDAP mapping")
	}
}
