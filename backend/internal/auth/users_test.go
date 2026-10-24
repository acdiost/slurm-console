package auth_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/auth"
	_ "slurm-console/backend/migrations"
)

func TestCreateAccountFromStdin(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	cmd := auth.UsersCommand(app)
	cmd.SetArgs([]string{"create", "--email", "operator@example.com", "--name", "Operator"})
	password := "stdin-only-password-123"
	cmd.SetIn(strings.NewReader(password + "\n"))
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), password) {
		t.Fatal("password leaked to command output")
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	record, err := app.FindAuthRecordByEmail("users", "operator@example.com")
	if err != nil || !record.GetBool("active") || !record.ValidatePassword(password) {
		t.Fatalf("invalid provisioned account: %v", err)
	}
}

func TestCreateRejectsWeakPassword(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	cmd := auth.UsersCommand(app)
	cmd.SetArgs([]string{"create", "--email", "operator@example.com"})
	cmd.SetIn(strings.NewReader("short\n"))
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	if err := cmd.Execute(); err == nil {
		t.Fatal("weak password accepted")
	}
}
