package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/rbac"
)

const fixturePing = `{"pings":[{"hostname":"controller","pinged":"UP","mode":"primary"}],"meta":{"plugin":{"data_parser":"data_parser/v0.0.43"},"slurm":{"release":"25.11.4"}},"errors":[]}`

func connectionRegistry(t *testing.T, handler http.HandlerFunc) *clusterconfig.Registry {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "sc-rest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socket := filepath.Join(directory, "rest.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	registry, err := clusterconfig.NewRegistry(map[string]clusterconfig.Profile{"local-test": {Label: "Local test", Endpoint: "unix://" + socket, AuthType: "local", Versions: []string{"v0.0.43"}}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
func validConnection() clusterconfig.Input {
	return clusterconfig.Input{Name: "Research cluster", Slug: "research", ConnectionRef: "local-test", APIVersion: "v0.0.43", AuthenticationMethod: "pam", Enabled: true, TimeoutSeconds: 2}
}
func payload(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestClusterConfigurationRequiresPlatformPermissionAndKeepsNativeCRUDLocked(t *testing.T) {
	app, mux := setup(t)
	registry := connectionRegistry(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(fixturePing)) })
	clusterconfig.Register(app, registry)
	record := user(t, app, "users")
	token := login(t, mux)
	a := cluster(t, app, "alpha")
	grant(t, app, record.Id, "cluster_admin", a.Id)
	for _, path := range []string{"/api/v1/cluster-configurations", "/api/v1/cluster-connection-profiles"} {
		if res := request(mux, "GET", path, "", token); res.Code != 403 {
			t.Fatalf("cluster role gained platform access: %d", res.Code)
		}
		if res := request(mux, "GET", path, "", ""); res.Code != 401 {
			t.Fatal("anonymous configuration access")
		}
	}
	grant(t, app, record.Id, "super_admin", "")
	res := request(mux, "POST", "/api/v1/cluster-configurations", payload(t, validConnection()), token)
	if res.Code != 200 {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	if res := request(mux, "POST", "/api/collections/clusters/records", `{"name":"Bypass","slug":"bypass"}`, token); res.Code != 403 {
		t.Fatal("platform user bypassed configuration API")
	}
	if res := request(mux, "POST", "/api/v1/cluster-configurations", strings.TrimSuffix(payload(t, validConnection()), "}")+`,"endpoint":"http://169.254.169.254"}`, token); res.Code != 400 {
		t.Fatal("client-controlled endpoint accepted")
	}
	input := validConnection()
	input.ConnectionRef = "unknown"
	if res := request(mux, "POST", "/api/v1/cluster-configurations", payload(t, input), token); res.Code != 400 {
		t.Fatal("unapproved connection profile accepted")
	}
}

func TestClusterConfigurationSaveTestDisableAndRevisionPreserveGrants(t *testing.T) {
	app, mux := setup(t)
	registry := connectionRegistry(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(fixturePing)) })
	manager := clusterconfig.Register(app, registry)
	record := user(t, app, "users")
	grant(t, app, record.Id, "super_admin", "")
	token := login(t, mux)
	legacy := cluster(t, app, "research")
	binding := grant(t, app, record.Id, "viewer", legacy.Id)
	input := validConnection()
	view, err := manager.Save(legacy.Id, input)
	if err != nil {
		t.Fatal(err)
	}
	res := request(mux, "POST", "/api/v1/cluster-configurations/"+view.ID+"/test", `{"revision":1}`, token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"connected"`) {
		t.Fatalf("test failed: %d %s", res.Code, res.Body.String())
	}
	res = request(mux, "GET", "/api/v1/cluster-configurations?perPage=1", "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"serverVersion":"25.11.4"`) {
		t.Fatalf("probe result not persisted: %s", res.Body.String())
	}
	input = view.Input
	input.Name = "Updated research"
	updated, err := manager.Save(view.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Save(view.ID, input); !errors.Is(err, clusterconfig.ErrConflict) {
		t.Fatal("stale edit overwrote current configuration")
	}
	if updated.LastProbe.Status != "not_tested" {
		t.Fatal("old health survived configuration edit")
	}
	res = request(mux, "POST", "/api/v1/cluster-configurations/"+view.ID+"/disable", `{"revision":2}`, token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"enabled":false`) {
		t.Fatal("disable failed")
	}
	if _, err := app.FindRecordById("user_cluster_roles", binding.Id); err != nil {
		t.Fatal("disable removed grants")
	}
	access, err := rbac.Resolve(app, record.Id)
	if err != nil || len(access.Clusters) != 1 {
		t.Fatal("RBAC scope changed")
	}
	if res := request(mux, "POST", "/api/v1/cluster-configurations/"+view.ID+"/test", `{"revision":3}`, token); res.Code != 409 {
		t.Fatal("disabled cluster was probed")
	}
}

func TestInFlightProbeCannotOverwriteEditedConfiguration(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	app, _ := setup(t)
	registry := connectionRegistry(t, func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(fixturePing))
	})
	manager := clusterconfig.Register(app, registry)
	view, err := manager.Save("", validConnection())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := manager.Test(context.Background(), view.ID, view.Revision); done <- err }()
	<-started
	input := view.Input
	input.Name = "Changed while testing"
	updated, err := manager.Save(view.ID, input)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, clusterconfig.ErrConflict) {
		t.Fatalf("stale probe committed: %v", err)
	}
	current, err := app.FindRecordById("clusters", view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if manager.View(current).LastProbe.Status != "not_tested" || current.GetInt("revision") != updated.Revision {
		t.Fatal("stale probe overwrote edited state")
	}
}

func TestConfigurationProfilesNeverExposeTokenFileOrCredential(t *testing.T) {
	app, mux := setup(t)
	record := user(t, app, "users")
	grant(t, app, record.Id, "super_admin", "")
	token := login(t, mux)
	secretFile := filepath.Join(t.TempDir(), "private-jwt-secret")
	if err := os.WriteFile(secretFile, []byte("fixture-value-never-returned"), 0600); err != nil {
		t.Fatal(err)
	}
	registry, err := clusterconfig.NewRegistry(map[string]clusterconfig.Profile{"https-test": {Label: "HTTPS test", Endpoint: "https://slurm.example.com", AuthType: "jwt", Username: "monitor", TokenFile: secretFile, Versions: []string{"v0.0.43"}}})
	if err != nil {
		t.Fatal(err)
	}
	clusterconfig.Register(app, registry)
	res := request(mux, "GET", "/api/v1/cluster-connection-profiles", "", token)
	if res.Code != 200 || strings.Contains(res.Body.String(), secretFile) || strings.Contains(res.Body.String(), "fixture-value-never-returned") || strings.Contains(res.Body.String(), "tokenFile") {
		t.Fatal("credentials leaked through profiles")
	}
}

func TestClusterAuthenticationMethodsSaveIndependentlyAndRejectInvalidRequests(t *testing.T) {
	app, mux := setup(t)
	manager := clusterconfig.Register(app, connectionRegistry(t, func(http.ResponseWriter, *http.Request) {
		t.Error("configuration save or preview contacted Slurm")
	}))
	person := user(t, app, "users")
	grant(t, app, person.Id, "super_admin", "")
	token := login(t, mux)
	base := "/api/v1/cluster-configurations"
	saved := make([]clusterconfig.Configuration, 0, 3)
	for _, method := range []string{"pam", "ldap", "freeipa"} {
		input := validConnection()
		input.Slug = "research-" + method
		input.AuthenticationMethod = method
		result := request(mux, "POST", base, payload(t, input), token)
		var body struct{ Data clusterconfig.Configuration }
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &body) != nil || body.Data.AuthenticationMethod != method {
			t.Fatal(method, result.Code, result.Body.String())
		}
		stored, err := app.FindRecordById("clusters", body.Data.ID)
		if err != nil || stored.GetString("authentication_method") != method {
			t.Fatal("authentication choice was not persisted", method, err)
		}
		saved = append(saved, body.Data)
	}
	input := saved[0].Input
	input.AuthenticationMethod = "freeipa"
	result := request(mux, "PATCH", base+"/"+saved[0].ID, payload(t, input), token)
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	for index, want := range []string{"freeipa", "ldap", "freeipa"} {
		stored, err := app.FindRecordById("clusters", saved[index].ID)
		if err != nil || manager.View(stored).AuthenticationMethod != want || stored.GetInt("revision") != saved[index].Revision+map[bool]int{true: 1}[index == 0] {
			t.Fatal("changing cluster A changed another cluster", index, err)
		}
	}
	for _, invalid := range []string{"missing", `""`, `"unknown"`, `"PAM"`, `["pam"]`, `["pam","ldap"]`, "null"} {
		var body map[string]any
		if err := json.Unmarshal([]byte(payload(t, validConnection())), &body); err != nil {
			t.Fatal(err)
		}
		delete(body, "authenticationMethod")
		if invalid != "missing" {
			body["authenticationMethod"] = json.RawMessage(invalid)
		}
		for _, path := range []string{base, base + "/preview"} {
			result := request(mux, "POST", path, payload(t, body), token)
			if result.Code != 400 {
				t.Fatal("invalid authentication choice accepted", invalid, path, result.Code, result.Body.String())
			}
		}
	}
	if count, err := app.CountRecords("clusters"); err != nil || count != 3 {
		t.Fatal("invalid requests changed inventory", count, err)
	}
}

func TestLegacyClusterRemainsVisibleAndAllowsProbeAndDisableWithoutChoosingAuthentication(t *testing.T) {
	app, _ := setup(t)
	manager := clusterconfig.Register(app, connectionRegistry(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixturePing))
	}))
	legacy := cluster(t, app, "legacy-unconfigured")
	legacy.Set("authentication_method", "")
	legacy.Set("connection_ref", "local-test")
	legacy.Set("api_version", "v0.0.43")
	legacy.Set("enabled", true)
	legacy.Set("timeout_seconds", 2)
	if err := app.Save(legacy); err != nil {
		t.Fatal(err)
	}
	if view := manager.View(legacy); view.ID != legacy.Id || view.AuthenticationMethod != "" {
		t.Fatal("legacy view invented an authentication choice", view)
	}
	probed, err := manager.Test(context.Background(), legacy.Id, legacy.GetInt("revision"))
	if err != nil || probed.AuthenticationMethod != "" || probed.LastProbe.Status != "connected" {
		t.Fatal("legacy connection probe failed or chose authentication", err, probed)
	}
	preview, err := manager.PreviewDisable(legacy.Id, legacy.GetInt("revision"))
	if err != nil || preview.After.AuthenticationMethod != "" || preview.After.Enabled {
		t.Fatal("legacy disable preview failed or chose authentication", err, preview)
	}
	disabled, err := manager.Disable(legacy.Id, legacy.GetInt("revision"))
	if err != nil || disabled.AuthenticationMethod != "" || disabled.Enabled {
		t.Fatal("legacy cluster could not be retired without choosing authentication", err, disabled)
	}
}
