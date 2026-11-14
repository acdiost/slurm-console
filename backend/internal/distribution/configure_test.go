package distribution

import (
	"bufio"
	"bytes"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/platformsettings"
	"slurm-console/backend/internal/rbac"
	_ "slurm-console/backend/migrations"
)

func wizardApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.ResetBootstrapState() })
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	return app
}

func runWizard(app core.App, dir string, lines ...string) (string, error) {
	var out bytes.Buffer
	p := &prompts{in: bufio.NewReader(strings.NewReader(strings.Join(lines, "\n") + "\n")), out: &out}
	err := configure(app, dir, p)
	return out.String(), err
}

func TestConfigureDefaultsGenerateOneAdminAndPreservePasswordOnRerun(t *testing.T) {
	app, dir := wizardApp(t), t.TempDir()
	out, err := runWizard(app, dir, "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	marker := "首次管理员密码（仅显示本次，请保存）: "
	_, secret, ok := strings.Cut(out, marker)
	if !ok {
		t.Fatal("generated credential was not shown")
	}
	secret, _, _ = strings.Cut(secret, "\n")
	u, err := app.FindAuthRecordByEmail("users", "admin@example.com")
	if err != nil || !u.ValidatePassword(secret) {
		t.Fatal("default admin password unusable", err)
	}
	access, err := rbac.Resolve(app, u.Id)
	if err != nil || !access.Allows("platform.users.manage", "") {
		t.Fatal("admin was not granted permissions", err)
	}
	out, err = runWizard(app, dir, "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	u2, _ := app.FindAuthRecordByEmail("users", u.Email())
	if u2.Id != u.Id || !u2.ValidatePassword(secret) || strings.Contains(out, secret) {
		t.Fatal("rerun replaced or exposed existing credentials")
	}
	for _, collection := range []string{"users", "user_platform_roles"} {
		if n, err := app.CountRecords(collection); err != nil || n != 1 {
			t.Fatal("rerun duplicated records", collection, n, err)
		}
	}
}

func TestConfigureLocalConnectionRerunPreservesVersionSecretsAndOtherEnv(t *testing.T) {
	app, dir := wizardApp(t), t.TempDir()
	env := "# keep integrations\nSLURM_CONSOLE_LDAP_CONFIG=/etc/slurm-console/ldap.json\n"
	if err := os.WriteFile(filepath.Join(dir, "slurm-console.env"), []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	password := "wizard-fixture-password"
	out, err := runWizard(app, dir, "Research Console", "operator@example.com", "Operator", password, password, "y", "", "Alpha", "unix:///run/test.sock", "v0.0.44", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, password) {
		t.Fatal("password echoed")
	}
	for range 2 {
		if _, err = runWizard(app, dir, "", "", "", "", "y", "", "", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	u, _ := app.FindAuthRecordByEmail("users", "operator@example.com")
	if !u.ValidatePassword(password) {
		t.Fatal("blank password changed credentials")
	}
	clusters, err := app.FindAllRecords("clusters")
	if err != nil || len(clusters) != 1 || clusters[0].GetString("api_version") != "v0.0.44" {
		t.Fatal("rerun changed version or duplicated cluster", err)
	}
	registry, err := clusterconfig.LoadRegistry(filepath.Join(dir, "cluster-profiles.json"))
	if err != nil || registry.Profiles["primary"].LocalUser != "root" {
		t.Fatal("socket is not configured as root", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "slurm-console.env"))
	if err != nil || !strings.Contains(string(data), env) || envValue(string(data), "SLURM_CONSOLE_CLUSTER_PROFILES") != filepath.Join(dir, "cluster-profiles.json") {
		t.Fatal("existing integration configuration lost", err)
	}
	settings, _ := platformsettings.Read(app)
	if settings.Name != "Research Console" {
		t.Fatal("blank platform name did not preserve value")
	}
}

func TestConfigureJWTStoredPrivatelyWithoutEchoAndBlankPreservesIt(t *testing.T) {
	app, dir := wizardApp(t), t.TempDir()
	jwt := "fixture.jwt.credential"
	out, err := runWizard(app, dir, "", "", "", "", "y", "", "", "https://slurm.example.com", "", "slurm", "", jwt, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, jwt) {
		t.Fatal("JWT echoed")
	}
	path := filepath.Join(dir, "primary.jwt")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != jwt {
		t.Fatal("credential not persisted", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("credential permissions are not private")
	}
	if _, err = runWizard(app, dir, "", "", "", "", "y", "", "", "", "", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != jwt {
		t.Fatal("blank JWT overwrote existing credential")
	}
}

func TestConfigureAutoDiscoversLegacyVersionUsingNewPrivateJWT(t *testing.T) {
	app, dir := wizardApp(t), t.TempDir()
	const jwt = "fixture.new.jwt"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi/v3" || r.Header.Get("X-SLURM-USER-TOKEN") != jwt || r.Header.Get("X-SLURM-USER-NAME") != "slurm" {
			t.Error("discovery did not use configured identity", r.URL.Path)
			w.WriteHeader(403)
			return
		}
		fmt.Fprint(w, `{"openapi":"3.0.2","paths":{`)
		for i, resource := range []string{"ping", "jobs", "nodes", "partitions"} {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `"/slurm/v0.0.40/%s/":{"get":{}}`, resource)
		}
		fmt.Fprint(w, `}}`)
	}))
	t.Cleanup(server.Close)
	caPath := filepath.Join(dir, "test-ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runWizard(app, dir, "", "", "", "", "y", "", "", server.URL, "auto", "slurm", caPath, jwt, "")
	if err != nil || !strings.Contains(out, "本次选择 v0.0.40") || strings.Contains(out, jwt) {
		t.Fatal("auto version selection failed or leaked JWT", err)
	}
	clusters, err := app.FindAllRecords("clusters")
	if err != nil || len(clusters) != 1 || clusters[0].GetString("api_version") != "v0.0.40" {
		t.Fatal("detected version not persisted", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".discover-token-") {
			t.Fatal("temporary discovery credential was retained")
		}
	}
	// A blank version on rerun preserves the detected selection, without discovery.
	if _, err := runWizard(app, dir, "", "", "", "", "y", "", "", "", "", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	current, _ := app.FindRecordById("clusters", clusters[0].Id)
	if current.GetString("api_version") != "v0.0.40" {
		t.Fatal("rerun changed the selected version")
	}
}

func TestConfigureReusesExternalAdministratorWithoutCreatingLocalAccount(t *testing.T) {
	app, dir := wizardApp(t), t.TempDir()
	if _, err := runWizard(app, dir, "", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	u, err := app.FindAuthRecordByEmail("users", "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	u.Set("auth_source", "pam")
	u.Set("username", "existing_operator")
	if err = app.Save(u); err != nil {
		t.Fatal(err)
	}
	passwordHash := u.GetString("password")
	if _, err = runWizard(app, dir, "", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	current, err := app.FindRecordById("users", u.Id)
	if err != nil || current.GetString("auth_source") != "pam" || current.GetString("username") != "existing_operator" || current.GetString("password") != passwordHash {
		t.Fatal("external identity changed", err)
	}
	if n, _ := app.CountRecords("users"); n != 1 {
		t.Fatal("created an unwanted local account")
	}
	if _, err = runWizard(app, dir, "", "", "", "replace-external-password"); err == nil {
		t.Fatal("accepted password change for external account")
	}
}

func TestConfigureInvalidPasswordDoesNotCommitPartialChanges(t *testing.T) {
	app, dir := wizardApp(t), t.TempDir()
	before, _ := platformsettings.Read(app)
	_, err := runWizard(app, dir, "Changed name", "", "", "short", "short", "y", "", "", "", "", "")
	if err == nil {
		t.Fatal("weak password accepted")
	}
	for _, collection := range []string{"users", "clusters", "user_platform_roles"} {
		if n, err := app.CountRecords(collection); err != nil || n != 0 {
			t.Fatal("partial records committed", collection, n, err)
		}
	}
	after, _ := platformsettings.Read(app)
	if after.Name != before.Name {
		t.Fatal("partial settings committed")
	}
	if _, err := os.Stat(filepath.Join(dir, "cluster-profiles.json")); !os.IsNotExist(err) {
		t.Fatal("invalid setup wrote profile")
	}
}

func TestPromptsEOFIsCancellationAndEnvironmentPathsRoundTrip(t *testing.T) {
	p := &prompts{in: bufio.NewReader(strings.NewReader("")), out: io.Discard}
	if _, err := p.read("value", "default", false); err == nil {
		t.Fatal("EOF silently applied defaults")
	}
	value := "/etc/slurm-console/folder with spaces/cluster.json"
	if got := envValue(setEnv("UNRELATED=true\nKEY=old\n", "KEY", value), "KEY"); got != value {
		t.Fatal(got)
	}
}
