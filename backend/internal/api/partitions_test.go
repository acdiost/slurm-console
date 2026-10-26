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

func TestPartitionDetailUsesExactNodeMembershipAndPreservesUnavailableCapacity(t *testing.T) {
	app, mux := setup(t)
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "nodes") {
			fmt.Fprint(w, slurmEnvelope(`"nodes":[{"name":"member","state":["DOWN"],"cpus":4,"real_memory":1024,"partitions":["gpu","other"]},{"name":"prefix","state":["IDLE"],"cpus":64,"real_memory":8192,"partitions":["gpu-extra"]}]`))
			return
		}
		writeFixture(w, r)
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "viewer", config.ID)
	token := login(t, mux)
	path := "/api/v1/clusters/" + config.ID + "/partitions/"
	res := request(mux, "GET", path+"gpu", "", token)
	var body struct {
		Data struct {
			Item      struct{ Name string }
			Resources struct {
				Status string
				Item   struct {
					Complete        bool
					ConfiguredNodes int
					Summary         struct {
						Total, Unavailable int
						CPUs               struct{ Total, Idle, Unavailable int }
					}
				}
			}
		}
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	b := body.Data.Resources
	if res.Code != 200 || body.Data.Item.Name != "gpu" || b.Status != "ok" || !b.Item.Complete || b.Item.Summary.Total != 1 || b.Item.Summary.Unavailable != 1 || b.Item.Summary.CPUs.Total != 4 || b.Item.Summary.CPUs.Idle != 0 || b.Item.Summary.CPUs.Unavailable != 4 {
		t.Fatalf("resource semantics: %d %s", res.Code, res.Body.String())
	}
	for _, suffix := range []string{"GPU", "gpu-extra"} {
		if got := request(mux, "GET", path+suffix, "", token); got.Code != 404 {
			t.Fatalf("not exact: %s", got.Body.String())
		}
	}
	if got := request(mux, "GET", path+"gpu?nodes=true", "", token); got.Code != 400 {
		t.Fatal("unsupported detail query")
	}
	if got := request(mux, "POST", path+"gpu", "{}", token); got.Code != 404 {
		t.Fatalf("unexpected partition write route: %d", got.Code)
	}
}
func TestPartitionOnlyRoleCannotReadNodesAndRevocationAppliesToCache(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "nodes") {
			calls.Add(1)
		}
		writeFixture(w, r)
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	roles, _ := app.FindCollectionByNameOrId("roles")
	role := core.NewRecord(roles)
	role.Set("code", "partition_only")
	role.Set("name", "Partition only")
	role.Set("scope", "cluster")
	if err := app.Save(role); err != nil {
		t.Fatal(err)
	}
	permission, _ := app.FindFirstRecordByData("permissions", "code", "partitions.read")
	collection, _ := app.FindCollectionByNameOrId("role_permissions")
	rp := core.NewRecord(collection)
	rp.Set("role", role.Id)
	rp.Set("permission", permission.Id)
	if err := app.Save(rp); err != nil {
		t.Fatal(err)
	}
	binding, err := rbac.Grant(app, person.Id, "partition_only", config.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := login(t, mux)
	path := "/api/v1/clusters/" + config.ID + "/partitions/gpu"
	for range 2 {
		res := request(mux, "GET", path, "", token)
		if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"forbidden"`) || !strings.Contains(res.Body.String(), `"item":null`) {
			t.Fatal(res.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("nodes fetched without nodes.read")
	}
	if err := app.Delete(binding); err != nil {
		t.Fatal(err)
	}
	if res := request(mux, "GET", path, "", token); res.Code != 403 {
		t.Fatal("cached detail survived revocation")
	}
}
func TestPartitionResourceFailureAndIncompleteCoverageDoNotBecomeEmptySuccess(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			app, mux := setup(t)
			registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "nodes") {
					if fail {
						w.WriteHeader(503)
					} else {
						fmt.Fprint(w, slurmEnvelope(`"nodes":[]`))
					}
					return
				}
				writeFixture(w, r)
			})
			manager := clusterconfig.Register(app, registry)
			config, _ := manager.Save("", validConnection())
			person := user(t, app, "users")
			grant(t, app, person.Id, "viewer", config.ID)
			token := login(t, mux)
			res := request(mux, "GET", "/api/v1/clusters/"+config.ID+"/partitions/gpu", "", token)
			if res.Code != 200 {
				t.Fatal(res.Body.String())
			}
			body := res.Body.String()
			if fail && (!strings.Contains(body, `"status":"error"`) || !strings.Contains(body, `"item":null`)) {
				t.Fatal(body)
			}
			if !fail && (!strings.Contains(body, `"complete":false`) || !strings.Contains(body, `"configuredNodes":1`)) {
				t.Fatal(body)
			}
		})
	}
}
func TestPartitionSortingValidationAndUnknownLimits(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, slurmEnvelope(`"partitions":[{"name":"unknown","partition":{"state":["UP"]},"nodes":{"total":1},"cpus":{"total":2}},{"name":"unlimited","partition":{"state":["UP"]},"nodes":{"total":1},"cpus":{"total":2},"maximums":{"time":{"set":false,"infinite":true}}},{"name":"finite","partition":{"state":["DRAIN"]},"nodes":{"total":1},"cpus":{"total":2},"maximums":{"time":60}}]`))
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "viewer", config.ID)
	token := login(t, mux)
	path := "/api/v1/clusters/" + config.ID + "/partitions"
	for _, query := range []string{"?sort=invalid", "?order=invalid", "?sort=name&sort=cpus", "?page=0"} {
		if res := request(mux, "GET", path+query, "", token); res.Code != 400 {
			t.Fatal(res.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input reached upstream")
	}
	for _, sample := range []struct{ query, first string }{{"?sort=maxTime&order=asc", "finite"}, {"?sort=maxTime&order=desc", "unlimited"}, {"?state=DRAIN&q=FIN", "finite"}} {
		res := request(mux, "GET", path+sample.query, "", token)
		var body struct {
			Data struct{ Items []struct{ Name string } }
		}
		_ = json.Unmarshal(res.Body.Bytes(), &body)
		if res.Code != 200 || len(body.Data.Items) == 0 || body.Data.Items[0].Name != sample.first {
			t.Fatal(res.Body.String())
		}
		if len(body.Data.Items) > 1 && body.Data.Items[len(body.Data.Items)-1].Name != "unknown" {
			t.Fatal("unknown limit must sort last")
		}
	}
}
func TestPartitionDetailRejectsMixedConfigurationRevisions(t *testing.T) {
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
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "viewer", config.ID)
	token := login(t, mux)
	done := make(chan int, 1)
	go func() { done <- request(mux, "GET", "/api/v1/clusters/"+config.ID+"/partitions/gpu", "", token).Code }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request not started")
	}
	_, err := manager.Disable(config.ID, config.Revision)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != 409 {
		t.Fatalf("mixed revision: %d", code)
	}
}
