package audit_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/jobcontrol"
	"slurm-console/backend/internal/rbac"
	_ "slurm-console/backend/migrations"
)

func run(t *testing.T, cmd *cobra.Command, args ...string) {
	t.Helper()
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader("private-account-password-12345\n"))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
func TestAdministrativeCommandsPersistSelectedEvidence(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	run(t, auth.UsersCommand(app), "create", "--email", "audit@example.com", "--name", "Audited user")
	run(t, rbac.Commands(app), "register-cluster", "--slug", "test-cluster", "--name", "Test cluster")
	run(t, rbac.Commands(app), "grant", "--email", "audit@example.com", "--role", "viewer", "--cluster", "test-cluster")
	run(t, jobcontrol.Command(app), "link", "--email", "audit@example.com", "--cluster", "test-cluster", "--username", "worker", "--uid", "1001")
	run(t, jobcontrol.Command(app), "unlink", "--email", "audit@example.com", "--cluster", "test-cluster")
	run(t, rbac.Commands(app), "revoke", "--email", "audit@example.com", "--role", "viewer", "--cluster", "test-cluster")
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	rows, err := app.FindRecordsByFilter("operation_audits", "", "created,id", 0, 0)
	if err != nil || len(rows) != 6 {
		t.Fatalf("audit count %d %v", len(rows), err)
	}
	actions := map[string]bool{}
	keys := map[string]bool{}
	for _, r := range rows {
		actions[r.GetString("action")] = true
		if r.GetString("actor_source") != "local" || !strings.HasPrefix(r.GetString("actor_id"), "local:uid:") || r.GetString("request_id") == "" || r.GetString("target_id") == "" || keys[r.GetString("operation_key")] {
			t.Fatal("missing attribution")
		}
		keys[r.GetString("operation_key")] = true
		if strings.Contains(fmt.Sprint(r.PublicExport()), "private-account-password") {
			t.Fatal("password retained in evidence")
		}
	}
	for _, action := range []string{"users.create", "clusters.register", "access.grant", "identities.link", "identities.unlink", "access.revoke"} {
		if !actions[action] {
			t.Fatal(action)
		}
	}
	for _, r := range rows {
		if r.GetString("action") == "identities.unlink" && !strings.Contains(fmt.Sprint(r.GetRaw("changes")), "worker") {
			t.Fatal("unlinked identity lost evidence")
		}
	}
}
func TestAccessCommandRollsBackIfAuditFails(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	run(t, auth.UsersCommand(app), "create", "--email", "rollback@example.com")
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error { return fmt.Errorf("audit offline") })
	cmd := rbac.Commands(app)
	cmd.SetArgs([]string{"grant", "--email", "rollback@example.com", "--role", "super_admin"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	if err := cmd.Execute(); err == nil {
		t.Fatal("unpersisted audit allowed grant")
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	n, err := app.CountRecords("user_platform_roles")
	if err != nil || n != 0 {
		t.Fatal("role grant survived audit rollback")
	}
}
