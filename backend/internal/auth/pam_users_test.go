package auth_test

import (
	"io"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
)

func TestLinkPAMPreservesIdentityAndPermissionsAndInvalidatesLocalSession(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	run := func(args ...string) {
		t.Helper()
		cmd := auth.UsersCommand(app)
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	run("create-pam", "--email", "pam@example.com", "--username", "pam_fixture", "--name", "PAM fixture")
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail("users", "pam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	id := record.Id
	record.Set("auth_source", "local")
	record.SetPassword("original-password-123")
	record.Set("active", false)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	token, err := record.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rbac.Grant(app, id, "super_admin", ""); err != nil {
		t.Fatal(err)
	}
	_ = app.ResetBootstrapState()
	run("link-pam", "--email", "pam@example.com", "--username", "pam_fixture")
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	record, err = app.FindAuthRecordByEmail("users", "pam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if record.Id != id || record.GetString("auth_source") != "pam" || record.GetBool("active") || record.ValidatePassword("original-password-123") {
		t.Fatal("link changed activation, identity, or retained local password")
	}
	if _, err := app.FindAuthRecordByToken(token, core.TokenTypeAuth); err == nil {
		t.Fatal("old session remained valid")
	}
	// Resolve correctly denies disabled users. Re-enable only after verifying the
	// link preserved activation, then confirm the original grant is still present.
	record.Set("active", true)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	access, err := rbac.Resolve(app, id)
	if err != nil || len(access.PlatformRoles) != 1 {
		t.Fatal("RBAC mapping lost")
	}
}

func TestPAMMappingRejectsRootAndDuplicateUsername(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	for i, sample := range []struct {
		email, username string
		fail            bool
	}{{"root@example.com", "root", true}, {"first@example.com", "fixture", false}, {"duplicate@example.com", "fixture", true}, {"ambiguous@example.com", "user@example.com", true}} {
		cmd := auth.UsersCommand(app)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs([]string{"create-pam", "--email", sample.email, "--username", sample.username})
		if err := cmd.Execute(); (err != nil) != sample.fail {
			t.Fatalf("mapping case %d: %v", i, err)
		}
	}
}
