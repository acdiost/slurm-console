package api_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/services"
)

func setupSlurmFixture(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/openapi") {
		paths := map[string]any{}
		for _, version := range []string{"v0.0.40", "v0.0.43"} {
			for _, resource := range []string{"ping", "jobs", "nodes", "partitions"} {
				paths["/slurm/"+version+"/"+resource+"/"] = map[string]any{"get": map[string]any{}}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"openapi": "3.0.2", "paths": paths})
		return
	}
	writeFixture(w, r)
}

func assertSlurmSetupEmpty(t *testing.T, app core.App, beforeProfiles int) {
	t.Helper()
	for _, collection := range []string{"managed_cluster_profiles", "clusters", "operation_audits", "cluster_config_versions"} {
		rows, err := app.FindAllRecords(collection)
		if err != nil || len(rows) != 0 {
			t.Fatalf("%s was left behind: %d %v", collection, len(rows), err)
		}
	}
	if len(clusterconfig.For(app).Registry.Views()) != beforeProfiles {
		t.Fatal("failed setup changed live registry")
	}
	entries, err := os.ReadDir(filepath.Join(app.DataDir(), "managed-slurm"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("failed setup left credential files")
	}
}

func TestSlurmSetupRequiresPermissionAndRejectsInvalidInputsBeforeConnecting(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	token := login(t, mux)
	var calls atomic.Int32
	reg := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); setupSlurmFixture(w, r) })
	p, _ := reg.Get("local-test")
	in := clusterconfig.SetupInput{Name: "Research cluster", Slug: "research", Endpoint: p.Endpoint, AuthType: "local", APIVersion: "auto", TimeoutSeconds: 2}
	if out := request(mux, "POST", "/api/v1/setup/slurm", payload(t, in), ""); out.Code != 401 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := request(mux, "POST", "/api/v1/setup/slurm", payload(t, in), token); out.Code != 403 {
		t.Fatal(out.Code, out.Body.String())
	}
	grant(t, app, u.Id, "super_admin", "")
	for _, change := range []func(*clusterconfig.SetupInput){
		func(i *clusterconfig.SetupInput) {
			i.Endpoint = "http://example.test"
			i.AuthType = "jwt"
			i.Username = "monitor"
			i.Token = "secret"
		},
		func(i *clusterconfig.SetupInput) { i.Token = "secret" },
		func(i *clusterconfig.SetupInput) { i.Username = "root" },
		func(i *clusterconfig.SetupInput) { i.CAPEM = "certificate" },
		func(i *clusterconfig.SetupInput) { i.APIVersion = "v0.0.99" },
		func(i *clusterconfig.SetupInput) { i.TimeoutSeconds = 0 },
		func(i *clusterconfig.SetupInput) { i.Slug = "../unsafe" },
	} {
		bad := in
		change(&bad)
		out := request(mux, "POST", "/api/v1/setup/slurm", payload(t, bad), token)
		if out.Code != 400 {
			t.Fatal(out.Code, out.Body.String())
		}
	}
	for _, body := range []string{
		`{}`,
		strings.TrimSuffix(payload(t, in), "}") + `,"endpoint":"unix:///duplicate.sock"}`,
		strings.TrimSuffix(payload(t, in), "}") + `,"helper":"/arbitrary/path"}`,
	} {
		if out := request(mux, "POST", "/api/v1/setup/slurm", body, token); out.Code != 400 {
			t.Fatal("incomplete, duplicate or unknown input accepted", out.Code, out.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid or unauthorized setup reached provider")
	}
	assertSlurmSetupEmpty(t, app, 0)
}

func TestSlurmSetupLocalPersistsAndImmediatelyServesNormalClusterAPIs(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	grant(t, app, u.Id, "super_admin", "")
	token := login(t, mux)
	envRegistry := connectionRegistry(t, setupSlurmFixture)
	clusterconfig.Register(app, envRegistry)
	p, _ := envRegistry.Get("local-test")
	in := clusterconfig.SetupInput{Name: "Research cluster", Slug: "research", Endpoint: p.Endpoint, AuthType: "local", APIVersion: "auto", TimeoutSeconds: 2}
	out := request(mux, "POST", "/api/v1/setup/slurm", payload(t, in), token)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	var saved struct{ Data clusterconfig.Configuration }
	if err := json.Unmarshal(out.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Data.APIVersion != "v0.0.43" || saved.Data.Revision != 1 || !saved.Data.Enabled || !saved.Data.ProfileAvailable || saved.Data.LastProbe.Status != "connected" {
		t.Fatal(out.Body.String())
	}
	for _, route := range []string{"/api/v1/cluster-fleet", "/api/v1/clusters/" + saved.Data.ID + "/health", "/api/v1/clusters/" + saved.Data.ID + "/nodes", "/api/v1/clusters/" + saved.Data.ID + "/jobs"} {
		res := request(mux, "GET", route, "", token)
		if res.Code != 200 || strings.Contains(res.Body.String(), "profile_unavailable") {
			t.Fatal(route, res.Code, res.Body.String())
		}
	}
	for _, collection := range []string{"managed_cluster_profiles", "operation_audits", "cluster_config_versions"} {
		rows, err := app.FindAllRecords(collection)
		if err != nil || len(rows) != 1 {
			t.Fatal(collection, len(rows), err)
		}
	}
	if duplicate := request(mux, "POST", "/api/v1/setup/slurm", payload(t, in), token); duplicate.Code != 409 {
		t.Fatal(duplicate.Code, duplicate.Body.String())
	}
	// A fresh environment registry simulates startup with only saved database state.
	envProfile, _ := envRegistry.Get("local-test")
	fresh, _ := clusterconfig.NewRegistry(map[string]clusterconfig.Profile{"local-test": envProfile})
	clusterconfig.Register(app, fresh)
	if err := clusterconfig.LoadManaged(app); err != nil {
		t.Fatal(err)
	}
	if err := clusterconfig.LoadManaged(app); err != nil {
		t.Fatal("reload is not idempotent", err)
	}
	if len(fresh.Views()) != 2 {
		t.Fatal("environment profile was lost or managed profile duplicated")
	}
	res := request(mux, "GET", "/api/v1/clusters/"+saved.Data.ID+"/health", "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"connected"`) {
		t.Fatal(res.Code, res.Body.String())
	}
	colliding, _ := clusterconfig.NewRegistry(map[string]clusterconfig.Profile{saved.Data.ConnectionRef: envProfile})
	clusterconfig.Register(app, colliding)
	if err := clusterconfig.LoadManaged(app); !errors.Is(err, clusterconfig.ErrConflict) {
		t.Fatal("environment collision overwritten", err)
	}
	if len(colliding.Views()) != 1 {
		t.Fatal("failed load partially published")
	}
}

func TestSlurmSetupJWTProtectsCredentialsAndRollsBackEveryFailureStage(t *testing.T) {
	for _, mode := range []string{"success", "untrusted-ca", "discovery", "probe", "secret-reflection", "profile-save", "cluster-save", "audit", "history", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			app, mux := setup(t)
			u := user(t, app, "users")
			binding := grant(t, app, u.Id, "super_admin", "")
			token := login(t, mux)
			const secret = "private-jwt-do-not-disclose"
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-SLURM-USER-TOKEN") != secret || r.Header.Get("X-SLURM-USER-NAME") != "monitor" {
					t.Error("missing Slurm JWT headers")
				}
				if mode == "discovery" && strings.HasPrefix(r.URL.Path, "/openapi") {
					w.WriteHeader(401)
					fmt.Fprint(w, secret)
					return
				}
				if strings.Contains(r.URL.Path, "/ping/") {
					if mode == "probe" {
						w.WriteHeader(500)
						fmt.Fprint(w, secret)
						return
					}
					if mode == "secret-reflection" {
						fmt.Fprint(w, strings.ReplaceAll(fixturePing, "controller", secret))
						return
					}
					if mode == "revoked" {
						if err := app.Delete(binding); err != nil {
							t.Error(err)
						}
					}
				}
				setupSlurmFixture(w, r)
			}))
			defer server.Close()
			ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
			in := clusterconfig.SetupInput{Name: "Remote cluster", Slug: "remote", Endpoint: server.URL, AuthType: "jwt", Username: "monitor", Token: secret, CAPEM: ca, APIVersion: "0.0.43", TimeoutSeconds: 2}
			if mode == "discovery" {
				in.APIVersion = "auto"
			}
			if mode == "untrusted-ca" {
				in.CAPEM = ""
			}
			failCollection := map[string]string{"profile-save": "managed_cluster_profiles", "cluster-save": "clusters", "audit": "operation_audits", "history": "cluster_config_versions"}[mode]
			if failCollection != "" {
				app.OnRecordCreate(failCollection).BindFunc(func(*core.RecordEvent) error { return errors.New("injected storage failure") })
			}
			out := request(mux, "POST", "/api/v1/setup/slurm", payload(t, in), token)
			if strings.Contains(out.Body.String(), secret) || strings.Contains(out.Body.String(), app.DataDir()) {
				t.Fatal("private setup data leaked", out.Body.String())
			}
			if mode != "success" {
				want := 503
				if failCollection != "" {
					want = 500
				}
				if mode == "revoked" {
					want = 403
				}
				if out.Code != want {
					t.Fatal(mode, out.Code, out.Body.String())
				}
				assertSlurmSetupEmpty(t, app, 0)
				return
			}
			if out.Code != 200 {
				t.Fatal(out.Code, out.Body.String())
			}
			var saved struct{ Data clusterconfig.Configuration }
			if err := json.Unmarshal(out.Body.Bytes(), &saved); err != nil {
				t.Fatal(err)
			}
			p, ok := clusterconfig.For(app).Registry.Get(saved.Data.ConnectionRef)
			if !ok {
				t.Fatal("profile was not published")
			}
			data, err := os.ReadFile(p.TokenFile)
			if err != nil || string(data) != secret {
				t.Fatal("credential file missing", err)
			}
			info, err := os.Stat(p.TokenFile)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("credential permissions", err)
			}
			for _, collection := range []string{"managed_cluster_profiles", "operation_audits", "cluster_config_versions"} {
				rows, err := app.FindAllRecords(collection)
				if err != nil {
					t.Fatal(err)
				}
				encoded, _ := json.Marshal(rows)
				if strings.Contains(string(encoded), secret) {
					t.Fatal("credential persisted in database", collection)
				}
			}
			for _, route := range []string{"/api/v1/cluster-connection-profiles", "/api/v1/cluster-configurations", "/api/v1/setup/status", "/api/v1/cluster-configurations/" + saved.Data.ID + "/versions"} {
				res := request(mux, "GET", route, "", token)
				if res.Code != 200 || strings.Contains(res.Body.String(), secret) || strings.Contains(res.Body.String(), p.TokenFile) {
					t.Fatal(route, res.Code, res.Body.String())
				}
			}
			empty, _ := clusterconfig.NewRegistry(nil)
			clusterconfig.Register(app, empty)
			if err := clusterconfig.LoadManaged(app); err != nil {
				t.Fatal(err)
			}
			res := request(mux, "GET", "/api/v1/clusters/"+saved.Data.ID+"/health", "", token)
			if res.Code != 200 {
				t.Fatal("reloaded JWT connection unavailable", res.Code, res.Body.String())
			}
			dataDir := app.DataDir()
			if err := app.ResetBootstrapState(); err != nil {
				t.Fatal(err)
			}
			restarted := core.NewBaseApp(core.BaseAppConfig{DataDir: dataDir})
			if err := restarted.Bootstrap(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = restarted.ResetBootstrapState() })
			if err := clusterconfig.LoadManaged(restarted); err != nil {
				t.Fatal(err)
			}
			if _, err := services.SlurmFor(restarted).Read(context.Background(), saved.Data.ID, "health", ""); err != nil {
				t.Fatal("persisted connection failed after restart", err)
			}
		})
	}
}

func TestSlurmSetupManualVersionWorksWithoutOpenAPIDocument(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	grant(t, app, u.Id, "super_admin", "")
	token := login(t, mux)
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeFixture(w, r)
	})
	profile, _ := registry.Get("local-test")
	in := clusterconfig.SetupInput{Name: "Manual cluster", Slug: "manual", Endpoint: profile.Endpoint, AuthType: "local", APIVersion: "0.0.43", TimeoutSeconds: 2}
	out := request(mux, "POST", "/api/v1/setup/slurm", payload(t, in), token)
	var saved struct{ Data clusterconfig.Configuration }
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &saved) != nil || saved.Data.APIVersion != "v0.0.43" || saved.Data.LastProbe.Status != "connected" {
		t.Fatal("manual version requires discovery or did not verify ping", out.Code, out.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatal("manual setup made discovery or duplicate requests", calls.Load())
	}
}
