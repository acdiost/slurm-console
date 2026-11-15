package api_test

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"slurm-console/backend/internal/clusterconfig"
)

func TestSetupStatusRequiresCurrentPlatformManagementPermission(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	token := login(t, mux)
	if res := request(mux, "GET", "/api/v1/setup/status", "", ""); res.Code != 401 {
		t.Fatal("anonymous setup access", res.Code)
	}
	if res := request(mux, "GET", "/api/v1/setup/status", "", token); res.Code != 403 {
		t.Fatal("ordinary user read installation state", res.Code)
	}
	binding := grant(t, app, u.Id, "super_admin", "")
	if res := request(mux, "GET", "/api/v1/setup/status", "", token); res.Code != 200 {
		t.Fatal("administrator setup unavailable", res.Code, res.Body.String())
	}
	if err := app.Delete(binding); err != nil {
		t.Fatal(err)
	}
	if res := request(mux, "GET", "/api/v1/setup/status", "", token); res.Code != 403 {
		t.Fatal("revoked manager retained setup access", res.Code)
	}
}

func TestSetupStatusTracksUsableConfigurationWithoutProbingUpstream(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	grant(t, app, u.Id, "super_admin", "")
	token := login(t, mux)
	assertStatus := func(needed, configured bool, clusters, profiles int) {
		t.Helper()
		res := request(mux, "GET", "/api/v1/setup/status", "", token)
		var result struct {
			Data struct {
				NeedsSetup bool `json:"needsSetup"`
				Slurm      struct {
					Configured   bool `json:"configured"`
					ClusterCount int  `json:"clusterCount"`
					ProfileCount int  `json:"profileCount"`
				} `json:"slurm"`
			} `json:"data"`
		}
		if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &result) != nil {
			t.Fatal("setup status", res.Code, res.Body.String())
		}
		actual := result.Data
		if actual.NeedsSetup != needed || actual.Slurm.Configured != configured || actual.Slurm.ClusterCount != clusters || actual.Slurm.ProfileCount != profiles {
			t.Fatalf("unexpected setup status: %s", res.Body.String())
		}
	}
	assertStatus(true, false, 0, 0)
	cluster(t, app, "unconfigured")
	assertStatus(true, false, 1, 0)
	var upstreamCalls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		_, _ = w.Write([]byte(fixturePing))
	})
	manager := clusterconfig.Register(app, registry)
	assertStatus(true, false, 1, 1)
	configured, err := manager.Save("", validConnection())
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(false, true, 2, 1)
	if _, err := manager.Disable(configured.ID, configured.Revision); err != nil {
		t.Fatal(err)
	}
	assertStatus(true, false, 2, 1)
	if calls := upstreamCalls.Load(); calls != 0 {
		t.Fatalf("setup status unexpectedly contacted slurmrestd %d times", calls)
	}
}
