package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
)

const fixtureJobs = `"jobs":[{"job_id":42,"name":"training","user_name":"alice","account":"research","partition":"gpu","job_state":["RUNNING"],"cpus":{"set":true,"infinite":false,"number":8},"node_count":{"set":true,"infinite":false,"number":1},"time_limit":{"set":false,"infinite":true,"number":0},"environment":["not-public"],"script":"not-public"},{"job_id":7,"name":"analysis","user_name":"bob","partition":"cpu","job_state":["PENDING"],"state_reason":"Resources"}]`
const fixtureNodes = `"nodes":[{"name":"node2","hostname":"node2","state":["MIXED","DRAIN"],"cpus":16,"alloc_cpus":8,"real_memory":32768,"alloc_memory":4096,"free_mem":{"set":false,"infinite":false,"number":0},"partitions":["gpu"]},{"name":"node1","hostname":"node1","state":["IDLE"],"cpus":8,"real_memory":8192,"partitions":["cpu"]}]`
const fixturePartitions = `"partitions":[{"name":"gpu","partition":{"state":["UP"]},"nodes":{"configured":"node2","total":1},"cpus":{"total":16},"maximums":{"time":{"set":false,"infinite":true,"number":0}}}]`

func slurmEnvelope(fields string) string {
	return `{` + fields + `,"meta":{"plugin":{"data_parser":"data_parser/v0.0.43"},"slurm":{"release":"25.11.8","cluster":"fixture"}},"errors":[]}`
}
func writeFixture(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/slurm/v0.0.43/jobs/":
		fmt.Fprint(w, slurmEnvelope(fixtureJobs))
	case "/slurm/v0.0.43/job/42":
		fmt.Fprint(w, slurmEnvelope(fixtureJobs))
	case "/slurm/v0.0.43/nodes/", "/slurm/v0.0.43/node/node2":
		fmt.Fprint(w, slurmEnvelope(fixtureNodes))
	case "/slurm/v0.0.43/partitions/":
		fmt.Fprint(w, slurmEnvelope(fixturePartitions))
	case "/slurm/v0.0.43/ping/":
		fmt.Fprint(w, fixturePing)
	default:
		w.WriteHeader(404)
	}
}
func TestSlurmReadRoutesEnforceScopePaginateFilterAndNormalize(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); writeFixture(w, r) })
	manager := clusterconfig.Register(app, registry)
	config, err := manager.Save("", validConnection())
	if err != nil {
		t.Fatal(err)
	}
	person := user(t, app, "users")
	binding := grant(t, app, person.Id, "viewer", config.ID)
	token := login(t, mux)
	base := "/api/v1/clusters/" + config.ID
	for _, suffix := range []string{"/jobs", "/jobs/42", "/nodes", "/nodes/node2", "/partitions", "/health"} {
		res := request(mux, "GET", base+suffix, "", token)
		if res.Code != 200 {
			t.Fatalf("%s: %d %s", suffix, res.Code, res.Body.String())
		}
		if strings.Contains(res.Body.String(), "not-public") || strings.Contains(res.Body.String(), "data_parser") {
			t.Fatal("raw upstream fields escaped")
		}
	}
	res := request(mux, "GET", base+"/jobs?user=alice&state=RUNNING&perPage=1", "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"id":"42"`) || !strings.Contains(res.Body.String(), `"totalItems":1`) || !strings.Contains(res.Body.String(), `"infinite":true`) || !strings.Contains(res.Body.String(), `"cached":true`) {
		t.Fatalf("job filter/cache: %s", res.Body.String())
	}
	res = request(mux, "GET", base+"/nodes?state=DRAIN&partition=gpu", "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"name":"node2"`) || !strings.Contains(res.Body.String(), `"value":null`) || strings.Contains(res.Body.String(), `"name":"node1"`) {
		t.Fatal("node flags/filter/unset normalization failed")
	}
	res = request(mux, "GET", base+"/jobs?page=2&perPage=1", "", token)
	if !strings.Contains(res.Body.String(), `"id":"7"`) {
		t.Fatal("deterministic pagination failed")
	}
	before := calls.Load()
	other := cluster(t, app, "other")
	if res := request(mux, "GET", "/api/v1/clusters/"+other.Id+"/jobs", "", token); res.Code != 403 {
		t.Fatal("cross-cluster access")
	}
	for _, path := range []string{"/jobs?page=0", "/jobs?user=a&user=b", "/jobs?endpoint=evil", "/jobs/4294967296", "/nodes/bad%3Fname"} {
		res := request(mux, "GET", base+path, "", token)
		if res.Code != 400 && res.Code != 404 {
			t.Fatalf("invalid request accepted: %s %d", path, res.Code)
		}
	}
	if calls.Load() != before {
		t.Fatal("rejected input reached upstream")
	}
	if err := app.Delete(binding); err != nil {
		t.Fatal(err)
	}
	if res := request(mux, "GET", base+"/jobs", "", token); res.Code != 403 {
		t.Fatal("cached response bypassed revoked permission")
	}
	if res := request(mux, "GET", base+"/jobs", "", ""); res.Code != 401 {
		t.Fatal("anonymous Slurm read")
	}
	if res := request(mux, "POST", base+"/jobs/42/cancel", "{}", token); res.Code != 404 {
		t.Fatal("mutation exposed before identity/audit integration")
	}
}
func TestSlurmCacheUsesRevisionAndDoesNotCacheFailures(t *testing.T) {
	app, mux := setup(t)
	var mode, calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if mode.Load() == 0 {
			w.WriteHeader(403)
			fmt.Fprint(w, "upstream-private-details")
			return
		}
		writeFixture(w, r)
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "viewer", config.ID)
	token := login(t, mux)
	path := "/api/v1/clusters/" + config.ID + "/jobs"
	failed := request(mux, "GET", path, "", token)
	if failed.Code != 503 || !strings.Contains(failed.Body.String(), `"code":"SLURM_AUTH_FAILED"`) || strings.Contains(failed.Body.String(), "upstream-private-details") {
		t.Fatal("unsafe or unclassified upstream error")
	}
	mode.Store(1)
	if request(mux, "GET", path, "", token).Code != 200 {
		t.Fatal("cached upstream failure")
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected request count")
	}
	input := config.Input
	input.Description = "changed"
	updated, err := manager.Save(config.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if request(mux, "GET", path, "", token).Code != 200 || calls.Load() != 3 {
		t.Fatal("old configuration cache reused")
	}
	if _, err := manager.Disable(config.ID, updated.Revision); err != nil {
		t.Fatal(err)
	}
	if res := request(mux, "GET", path, "", token); res.Code != 409 || calls.Load() != 3 {
		t.Fatal("disabled configuration served cached or remote data")
	}
}
func TestSlurmCoalescesQueriesAndCallerCancellationDoesNotPoisonOthers(t *testing.T) {
	app, _ := setup(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-release
		writeFixture(w, r)
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	service := services.SlurmFor(app)
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := service.Read(ctx, config.ID, "jobs", ""); first <- err }()
	<-started
	cancel()
	if err := <-first; err != context.Canceled {
		t.Fatal("caller cancellation ignored")
	}
	var group sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		group.Go(func() { _, err := service.Read(context.Background(), config.ID, "jobs", ""); results <- err })
	}
	close(release)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("same query caused %d controller requests", calls.Load())
	}
}
func TestSlurmInFlightResultCannotSurviveDisable(t *testing.T) {
	app, _ := setup(t)
	started, release := make(chan struct{}), make(chan struct{})
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-release; writeFixture(w, r) })
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	done := make(chan error, 1)
	go func() {
		_, err := services.SlurmFor(app).Read(context.Background(), config.ID, "jobs", "")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not start")
	}
	_, err := manager.Disable(config.ID, config.Revision)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; client.Code(err) != "configuration_changed" {
		t.Fatalf("stale read survived disable: %v", err)
	}
}

func TestSlurmBoundsConcurrentUpstreamRequests(t *testing.T) {
	app, _ := setup(t)
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) { started <- struct{}{}; <-release; writeFixture(w, r) })
	manager := clusterconfig.Register(app, registry)
	config, err := manager.Save("", validConnection())
	if err != nil {
		t.Fatal(err)
	}
	service := services.SlurmFor(app)
	done := make(chan error, 4)
	for _, kind := range []string{"jobs", "nodes", "partitions", "health"} {
		go func() { _, err := service.Read(context.Background(), config.ID, kind, ""); done <- err }()
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("concurrent reads did not start")
		}
	}
	if _, err := service.Read(context.Background(), config.ID, "node", "node2"); client.Code(err) != "busy" {
		t.Fatal("unbounded controller concurrency")
	}
	once.Do(func() { close(release) })
	for range 4 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
