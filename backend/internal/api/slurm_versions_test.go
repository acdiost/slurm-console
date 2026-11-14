package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/slurm/protocol"
)

func TestSlurmRoutesReadAllReviewedVersionsAndCanonicalizeLegacyNames(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 4 {
			w.WriteHeader(404)
			return
		}
		version := parts[2]
		copy := r.Clone(r.Context())
		u := *r.URL
		copy.URL = &u
		copy.URL.Path = strings.Replace(r.URL.Path, version, "v0.0.43", 1)
		response := httptest.NewRecorder()
		if strings.HasSuffix(copy.URL.Path, "/reservations/") {
			fmt.Fprint(response, slurmEnvelope(`"reservations":[]`))
		} else {
			writeFixture(response, copy)
		}
		w.WriteHeader(response.Code)
		fmt.Fprint(w, strings.ReplaceAll(response.Body.String(), "v0.0.43", version))
	})
	p := registry.Profiles["local-test"]
	p.Versions = protocol.Versions()
	registry.Profiles["local-test"] = p
	manager := clusterconfig.Register(app, registry)
	u := user(t, app, "users")
	grant(t, app, u.Id, "super_admin", "")
	token := login(t, mux)
	for _, version := range protocol.Versions() {
		input := validConnection()
		input.Slug = "cluster-" + strings.ReplaceAll(version, ".", "-")
		input.APIVersion = strings.TrimPrefix(version, "v")
		cfg, err := manager.Save("", input)
		if err != nil || cfg.APIVersion != version {
			t.Fatal(cfg, err)
		}
		for _, resource := range []string{"health", "jobs", "jobs/42", "nodes", "nodes/node2", "partitions", "reservations"} {
			res := request(mux, "GET", "/api/v1/clusters/"+cfg.ID+"/"+resource, "", token)
			if res.Code != 200 {
				t.Fatalf("%s %s: %d %s", version, resource, res.Code, res.Body.String())
			}
			if resource == "health" && !strings.Contains(res.Body.String(), `"apiVersion":"`+version+`"`) {
				t.Fatal("health reported wrong version", res.Body.String())
			}
			if resource == "reservations" && !strings.Contains(res.Body.String(), fmt.Sprintf(`"supportsWrite":%t`, protocol.ReservationWrites(version))) {
				t.Fatal("incorrect reservation capability", res.Body.String())
			}
		}
		if !protocol.ReservationWrites(version) {
			before := calls.Load()
			res := request(mux, "POST", "/api/v1/clusters/"+cfg.ID+"/reservation-controls/prepare", payload(t, map[string]any{"action": "reservation_create", "spec": reservationSpec()}), token)
			if res.Code != 503 || !strings.Contains(res.Body.String(), "SLURM_API_UNSUPPORTED") || calls.Load() != before {
				t.Fatal("unsupported reservation preparation queried upstream", res.Code, res.Body.String())
			}
		}
	}
}

func TestVersionDiscoveryRequiresPlatformAccessAndHonorsApprovedVersions(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/openapi/v3" {
			t.Error("unexpected discovery request", r.URL.Path)
		}
		paths := []string{}
		for _, version := range []string{"v0.0.40", "v0.0.44"} {
			for _, resource := range []string{"ping", "jobs", "nodes", "partitions"} {
				paths = append(paths, `"/slurm/`+version+`/`+resource+`/":{"get":{}}`)
			}
		}
		fmt.Fprint(w, `{"openapi":"3.0.2","paths":{`+strings.Join(paths, ",")+`}}`)
	})
	p := registry.Profiles["local-test"]
	p.Versions = []string{"v0.0.40"}
	registry.Profiles["local-test"] = p
	clusterconfig.Register(app, registry)
	u := user(t, app, "users")
	c := cluster(t, app, "alpha")
	grant(t, app, u.Id, "viewer", c.Id)
	token := login(t, mux)
	path := "/api/v1/cluster-connection-profiles/local-test/versions"
	if res := request(mux, "GET", path, "", ""); res.Code != 401 {
		t.Fatal("anonymous discovery", res.Code)
	}
	if res := request(mux, "GET", path, "", token); res.Code != 403 {
		t.Fatal("cluster viewer gained platform discovery", res.Code)
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized discovery contacted Slurm")
	}
	grant(t, app, u.Id, "super_admin", "")
	res := request(mux, "GET", path, "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"versions":["v0.0.40"]`) || strings.Contains(res.Body.String(), "v0.0.44") {
		t.Fatal(res.Code, res.Body.String())
	}
	if res := request(mux, "GET", path+"?endpoint=unapproved", "", token); res.Code != 400 {
		t.Fatal("unexpected discovery selector accepted", res.Code)
	}
	if calls.Load() != 1 {
		t.Fatal("rejected request contacted Slurm", calls.Load())
	}
}
