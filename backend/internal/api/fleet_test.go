package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/rbac"
)

func fleetRegistry(t *testing.T, a, b http.HandlerFunc) *clusterconfig.Registry {
	t.Helper()
	first := connectionRegistry(t, a)
	second := connectionRegistry(t, b)
	combined, err := clusterconfig.NewRegistry(map[string]clusterconfig.Profile{"first": first.Profiles["local-test"], "second": second.Profiles["local-test"]})
	if err != nil {
		t.Fatal(err)
	}
	return combined
}
func fleetConnection(t *testing.T, m *clusterconfig.Manager, slug, ref string) clusterconfig.Configuration {
	t.Helper()
	input := validConnection()
	input.Name = slug
	input.Slug = slug
	input.ConnectionRef = ref
	v, err := m.Save("", input)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestFleetScopesMetadataAndUpstreamReadsAndKeepsClusterCachesSeparate(t *testing.T) {
	app, mux := setup(t)
	var aCalls, bCalls atomic.Int32
	registry := fleetRegistry(t, func(w http.ResponseWriter, r *http.Request) { aCalls.Add(1); writeFixture(w, r) }, func(w http.ResponseWriter, r *http.Request) {
		bCalls.Add(1)
		if strings.Contains(r.URL.Path, "nodes") {
			fmt.Fprint(w, slurmEnvelope(`"nodes":[{"name":"other","state":["IDLE"],"cpus":99,"real_memory":512,"partitions":[]}]`))
			return
		}
		writeFixture(w, r)
	})
	manager := clusterconfig.Register(app, registry)
	a := fleetConnection(t, manager, "alpha", "first")
	b := fleetConnection(t, manager, "beta", "second")
	person := user(t, app, "users")
	binding := grant(t, app, person.Id, "viewer", a.ID)
	token := login(t, mux)
	path := "/api/v1/cluster-fleet"
	res := request(mux, "GET", path, "", token)
	if res.Code != 200 || strings.Contains(res.Body.String(), b.ID) || aCalls.Load() != 3 || bCalls.Load() != 0 {
		t.Fatalf("scope: %d %s", res.Code, res.Body.String())
	}
	grant(t, app, person.Id, "viewer", b.ID)
	res = request(mux, "GET", path, "", token)
	var body struct {
		Data struct {
			TotalItems int
			Items      []struct {
				Cluster struct{ ID string }
				Nodes   struct {
					Item struct{ CPUs struct{ Total int } }
				}
			}
		}
	}
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	if res.Code != 200 || body.Data.TotalItems != 2 || len(body.Data.Items) != 2 || body.Data.Items[0].Nodes.Item.CPUs.Total != 24 || body.Data.Items[1].Nodes.Item.CPUs.Total != 99 || aCalls.Load() != 3 || bCalls.Load() != 3 {
		t.Fatalf("cache isolation: %d %s", res.Code, res.Body.String())
	}
	if err := app.Delete(binding); err != nil {
		t.Fatal(err)
	}
	res = request(mux, "GET", path+"?q=alpha", "", token)
	if res.Code != 200 || strings.Contains(res.Body.String(), a.ID) || !strings.Contains(res.Body.String(), `"totalItems":0`) {
		t.Fatal("revocation/search leaked metadata")
	}
	for _, fragment := range []string{"connectionRef", "endpoint", "authType", "local-test", "unix://"} {
		if strings.Contains(res.Body.String(), fragment) {
			t.Fatal("connection details exposed")
		}
	}
}
func TestFleetMissingResourcePermissionsNeverReadThoseSources(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "ping") {
			t.Error("unauthorized source: " + r.URL.Path)
		}
		calls.Add(1)
		writeFixture(w, r)
	})
	manager := clusterconfig.Register(app, registry)
	c := fleetConnection(t, manager, "alpha", "local-test")
	person := user(t, app, "users")
	roles, _ := app.FindCollectionByNameOrId("roles")
	role := core.NewRecord(roles)
	role.Set("code", "cluster_metadata_only")
	role.Set("name", "Metadata only")
	role.Set("scope", "cluster")
	if err := app.Save(role); err != nil {
		t.Fatal(err)
	}
	permission, _ := app.FindFirstRecordByData("permissions", "code", "clusters.read")
	bindings, _ := app.FindCollectionByNameOrId("role_permissions")
	binding := core.NewRecord(bindings)
	binding.Set("role", role.Id)
	binding.Set("permission", permission.Id)
	if err := app.Save(binding); err != nil {
		t.Fatal(err)
	}
	if _, err := rbac.Grant(app, person.Id, "cluster_metadata_only", c.ID); err != nil {
		t.Fatal(err)
	}
	token := login(t, mux)
	res := request(mux, "GET", "/api/v1/cluster-fleet", "", token)
	if res.Code != 200 || calls.Load() != 1 || strings.Count(res.Body.String(), `"status":"forbidden"`) != 2 || strings.Contains(res.Body.String(), `"cpus"`) {
		t.Fatal(res.Body.String())
	}
}
func TestFleetDisabledUnconfiguredAndPartialFailuresRemainIndependent(t *testing.T) {
	app, mux := setup(t)
	var unavailableCalls atomic.Int32
	registry := fleetRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "nodes") {
			w.WriteHeader(503)
			return
		}
		writeFixture(w, r)
	}, func(w http.ResponseWriter, r *http.Request) { unavailableCalls.Add(1); w.WriteHeader(503) })
	manager := clusterconfig.Register(app, registry)
	fleetConnection(t, manager, "alpha", "first")
	fleetConnection(t, manager, "beta", "second")
	disabled := fleetConnection(t, manager, "disabled", "first")
	_, _ = manager.Disable(disabled.ID, disabled.Revision)
	cluster(t, app, "unconfigured")
	person := user(t, app, "users")
	grant(t, app, person.Id, "super_admin", "")
	token := login(t, mux)
	res := request(mux, "GET", "/api/v1/cluster-fleet", "", token)
	var body struct {
		Data struct {
			Items []struct {
				State string
				Nodes struct {
					Status string
					Item   any
				}
				Jobs struct{ Status string }
			}
			PageSummary struct{ Connected, Partial, Unavailable, Disabled, Unconfigured, NeedsAttention int }
		}
	}
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	summary := body.Data.PageSummary
	if res.Code != 200 || len(body.Data.Items) != 4 || summary.Connected != 1 || summary.Partial != 1 || summary.NeedsAttention != 2 || summary.Unavailable != 1 || summary.Disabled != 1 || summary.Unconfigured != 1 || unavailableCalls.Load() != 1 {
		t.Fatal(res.Body.String())
	}
	if body.Data.Items[0].Nodes.Status != "error" || body.Data.Items[0].Nodes.Item != nil || body.Data.Items[0].Jobs.Status != "ok" {
		t.Fatal("partial success lost")
	}
	for _, mode := range []string{"disabled", "unconfigured"} {
		res := request(mux, "GET", "/api/v1/cluster-fleet?configuration="+mode, "", token)
		if res.Code != 200 || !strings.Contains(res.Body.String(), `"totalItems":1`) {
			t.Fatal(res.Body.String())
		}
	}
}
func TestFleetValidatesFiltersAndPaginatesBeforeUpstreamReads(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); writeFixture(w, r) })
	manager := clusterconfig.Register(app, registry)
	a := fleetConnection(t, manager, "alpha", "local-test")
	fleetConnection(t, manager, "beta", "local-test")
	person := user(t, app, "users")
	grant(t, app, person.Id, "super_admin", "")
	token := login(t, mux)
	for _, q := range []string{"?perPage=25", "?page=0", "?q=x&q=y", "?state=online", "?configuration=invalid", "?sort=endpoint", "?order=invalid", "?q=%00"} {
		if res := request(mux, "GET", "/api/v1/cluster-fleet"+q, "", token); res.Code != 400 {
			t.Fatalf("%s: %s", q, res.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid query reached upstream")
	}
	res := request(mux, "GET", "/api/v1/cluster-fleet?perPage=1&page=2&sort=name&order=desc", "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), a.ID) || !strings.Contains(res.Body.String(), `"totalItems":2`) || calls.Load() != 3 {
		t.Fatal(res.Body.String())
	}
	for _, q := range []string{"%25_", "%27%20OR%201%3D1"} {
		res := request(mux, "GET", "/api/v1/clusters?q="+q, "", token)
		if res.Code != 200 || !strings.Contains(res.Body.String(), `"totalItems":0`) {
			t.Fatal("search not literal")
		}
	}
	if res := request(mux, "GET", "/api/v1/cluster-fleet", "", ""); res.Code != 401 {
		t.Fatal("anonymous fleet")
	}
}
func TestFleetDiscardsOnlyChangedConfigurationButRejectsChangedAuthorization(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(fmt.Sprint(revoke), func(t *testing.T) {
			app, mux := setup(t)
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "nodes") {
					once.Do(func() { close(started) })
					<-release
				}
				writeFixture(w, r)
			})
			manager := clusterconfig.Register(app, registry)
			a := fleetConnection(t, manager, "alpha", "local-test")
			person := user(t, app, "users")
			binding := grant(t, app, person.Id, "viewer", a.ID)
			token := login(t, mux)
			done := make(chan struct {
				code int
				body string
			}, 1)
			go func() {
				res := request(mux, "GET", "/api/v1/cluster-fleet", "", token)
				done <- struct {
					code int
					body string
				}{res.Code, res.Body.String()}
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("fleet source did not start")
			}
			var err error
			if revoke {
				err = app.Delete(binding)
			} else {
				_, err = manager.Disable(a.ID, a.Revision)
			}
			close(release)
			if err != nil {
				t.Fatal(err)
			}
			res := <-done
			if revoke {
				if res.code != 409 || strings.Contains(res.body, `"items"`) {
					t.Fatal("revoked request returned metrics")
				}
			} else if res.code != 200 || !strings.Contains(res.body, `"state":"changed"`) || strings.Contains(res.body, `"cpus"`) {
				t.Fatal(res.body)
			}
		})
	}
}
