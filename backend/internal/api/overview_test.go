package api_test

import (
	"encoding/json"
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

func TestOverviewSummarizesAllVisibleRecordsAndEnforcesClusterScope(t *testing.T) {
	app, mux := setup(t)
	registry := connectionRegistry(t, writeFixture)
	manager := clusterconfig.Register(app, registry)
	config, err := manager.Save("", validConnection())
	if err != nil {
		t.Fatal(err)
	}
	person := user(t, app, "users")
	grant(t, app, person.Id, "viewer", config.ID)
	token := login(t, mux)
	res := request(mux, "GET", "/api/v1/clusters/"+config.ID+"/overview", "", token)
	if res.Code != 200 {
		t.Fatalf("overview: %d %s", res.Code, res.Body.String())
	}
	var body struct {
		Data struct {
			Nodes struct {
				Status string
				Item   struct {
					Total, Unavailable int
					CPUs               struct{ Total, Allocated, Idle, Unavailable int64 }
				}
			}
			Jobs struct {
				Item struct{ Total, Running, Pending int }
			}
			Partitions struct{ Item struct{ Total, Up int } }
		}
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Nodes.Status != "ok" || body.Data.Nodes.Item.Total != 2 || body.Data.Nodes.Item.Unavailable != 1 || body.Data.Nodes.Item.CPUs.Total != 24 || body.Data.Nodes.Item.CPUs.Idle != 8 || body.Data.Nodes.Item.CPUs.Unavailable != 8 || body.Data.Jobs.Item.Total != 2 || body.Data.Jobs.Item.Running != 1 || body.Data.Jobs.Item.Pending != 1 || body.Data.Partitions.Item.Up != 1 {
		t.Fatalf("overview aggregation: %s", res.Body.String())
	}
	if strings.Contains(res.Body.String(), "not-public") {
		t.Fatal("raw job fields exposed")
	}
	other := cluster(t, app, "other")
	if res := request(mux, "GET", "/api/v1/clusters/"+other.Id+"/overview", "", token); res.Code != 403 {
		t.Fatal("cross-cluster overview")
	}
	if res := request(mux, "GET", "/api/v1/clusters/"+config.ID+"/overview?user=alice", "", token); res.Code != 400 {
		t.Fatal("unsupported filter accepted for total summary")
	}
}
func TestOverviewDoesNotFetchUnauthorizedSectionsAndPreservesPartialSuccess(t *testing.T) {
	app, mux := setup(t)
	var jobsCalls, nodesCalls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "jobs") {
			jobsCalls.Add(1)
		}
		if strings.Contains(r.URL.Path, "nodes") {
			nodesCalls.Add(1)
			w.WriteHeader(403)
			return
		}
		writeFixture(w, r)
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	roles, _ := app.FindCollectionByNameOrId("roles")
	role := core.NewRecord(roles)
	role.Set("code", "overview_only")
	role.Set("name", "Overview only")
	role.Set("scope", "cluster")
	if err := app.Save(role); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"clusters.read", "nodes.read", "partitions.read"} {
		permission, err := app.FindFirstRecordByData("permissions", "code", code)
		if err != nil {
			t.Fatal(err)
		}
		collection, _ := app.FindCollectionByNameOrId("role_permissions")
		binding := core.NewRecord(collection)
		binding.Set("role", role.Id)
		binding.Set("permission", permission.Id)
		if err := app.Save(binding); err != nil {
			t.Fatal(err)
		}
	}
	binding, err := rbac.Grant(app, person.Id, "overview_only", config.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := login(t, mux)
	path := "/api/v1/clusters/" + config.ID + "/overview"
	res := request(mux, "GET", path, "", token)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	var body struct {
		Data struct {
			Health, Nodes, Jobs, Partitions struct {
				Status, Code string
				Item         json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Jobs.Status != "forbidden" || string(body.Data.Jobs.Item) != "null" || jobsCalls.Load() != 0 || nodesCalls.Load() != 1 || body.Data.Nodes.Status != "error" || body.Data.Nodes.Code != "SLURM_AUTH_FAILED" || body.Data.Health.Status != "ok" || body.Data.Partitions.Status != "ok" {
		t.Fatalf("section boundaries: %s", res.Body.String())
	}
	if err := app.Delete(binding); err != nil {
		t.Fatal(err)
	}
	if res := request(mux, "GET", path, "", token); res.Code != 403 {
		t.Fatal("cached overview survived permission revocation")
	}
}
func TestOverviewCannotMixRevisionsWhenConfigurationChangesDuringRequest(t *testing.T) {
	app, mux := setup(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "nodes") {
			once.Do(func() { close(started) })
			<-release
		}
		writeFixture(w, r)
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "viewer", config.ID)
	token := login(t, mux)
	done := make(chan int, 1)
	go func() { done <- request(mux, "GET", "/api/v1/clusters/"+config.ID+"/overview", "", token).Code }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("overview did not start")
	}
	_, err := manager.Disable(config.ID, config.Revision)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != 409 {
		t.Fatalf("configuration change returned stale overview: %d", code)
	}
}
