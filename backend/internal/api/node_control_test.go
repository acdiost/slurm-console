package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/nodecontrol"
	"slurm-console/backend/internal/services"
)

func nodeFields(states, reason string) string {
	return fmt.Sprintf(`"nodes":[{"name":"node1","hostname":"node1","address":"10.0.0.1","version":"25.11.8","state":%s,"reason":%q,"cpus":8,"effective_cpus":8,"alloc_cpus":2,"real_memory":8192,"alloc_memory":512,"partitions":["normal"],"gres":"gpu:a100:2","gres_used":"gpu:a100:1(IDX:0)","boot_time":{"set":true,"number":1700000000}}]`, states, reason)
}
func nodePrepared(t *testing.T, mux http.Handler, path, action, token string) nodecontrol.Prepared {
	t.Helper()
	r := request(mux, "GET", path+"/control-context?action="+action, "", token)
	if r.Code != 200 {
		t.Fatalf("prepare: %d %s", r.Code, r.Body.String())
	}
	var value struct{ Data nodecontrol.Prepared }
	if err := json.Unmarshal(r.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value.Data
}
func TestNodeDrainResumeDurableIdempotentAndInvalidatesQueries(t *testing.T) {
	app, mux := setup(t)
	var posts atomic.Int32
	var state, reason atomic.Value
	state.Store(`["MIXED"]`)
	reason.Store("")
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			var body map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid node body")
			}
			if r.URL.Path != "/slurm/v0.0.43/node/node1" || r.URL.RawQuery != "" {
				t.Error("arbitrary node path")
			}
			var states []string
			_ = json.Unmarshal(body["state"], &states)
			if len(states) != 1 || (states[0] != "DRAIN" && states[0] != "RESUME") {
				t.Error("unreviewed state")
			}
			var rows []*core.Record
			_ = app.RecordQuery("operation_audits").Where(dbx.HashExp{"status": "processing"}).All(&rows)
			if len(rows) != 1 || rows[0].GetString("node_name") != "node1" {
				t.Error("missing pre-write audit")
			}
			if states[0] == "DRAIN" {
				var text string
				_ = json.Unmarshal(body["reason"], &text)
				state.Store(`["MIXED","DRAIN"]`)
				reason.Store(text)
				if len(body) != 2 {
					t.Error("arbitrary node properties")
				}
			} else {
				state.Store(`["MIXED"]`)
				reason.Store("")
				if len(body) != 1 {
					t.Error("resume modified reason or other properties")
				}
			}
			fmt.Fprint(w, slurmEnvelope(`"warnings":[]`))
			return
		}
		fmt.Fprint(w, slurmEnvelope(nodeFields(state.Load().(string), reason.Load().(string))))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "operator", cfg.ID)
	token := login(t, mux)
	base := "/api/v1/clusters/" + cfg.ID
	path := base + "/nodes/node1"
	listing := request(mux, "GET", base+"/nodes", "", token)
	if listing.Code != 200 || !strings.Contains(listing.Body.String(), `"gpus":{"total":2,"allocated":1`) {
		t.Fatalf("node resources: %s", listing.Body.String())
	}
	prepared := nodePrepared(t, mux, path, "drain", token)
	if !prepared.Allowed {
		t.Fatal(prepared.Message)
	}
	input := nodecontrol.Input{Proof: prepared.Proof, Confirmation: "node1", Reason: "maintenance test"}
	bad := input
	bad.Confirmation = "node2"
	if request(mux, "POST", path+"/drain", payload(t, bad), token).Code != 400 {
		t.Fatal("confirmation ignored")
	}
	if request(mux, "POST", path+"/resume", payload(t, input), token).Code != 409 {
		t.Fatal("cross-action proof accepted")
	}
	if request(mux, "POST", path+"/drain", strings.TrimSuffix(payload(t, input), "}")+`,"state":["DOWN"]}`, token).Code != 400 {
		t.Fatal("arbitrary node properties accepted")
	}
	for range 2 {
		res := request(mux, "POST", path+"/drain", payload(t, input), token)
		if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"confirmed"`) {
			t.Fatal(res.Body.String())
		}
	}
	if posts.Load() != 1 {
		t.Fatal("idempotency failed")
	}
	if !strings.Contains(request(mux, "GET", base+"/nodes", "", token).Body.String(), "DRAIN") {
		t.Fatal("stale node cache survived write")
	}
	if request(mux, "GET", base+"/job-operations/"+prepared.OperationID, "", token).Code != 404 {
		t.Fatal("node audit exposed through jobs permission")
	}
	resume := nodePrepared(t, mux, path, "resume", token)
	if !resume.Allowed {
		t.Fatal(resume.Message)
	}
	input.Proof = resume.Proof
	res := request(mux, "POST", path+"/resume", payload(t, input), token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"confirmed"`) || posts.Load() != 2 {
		t.Fatal(res.Body.String())
	}
	if request(mux, "GET", base+"/node-operations/"+resume.OperationID, "", token).Code != 200 {
		t.Fatal("audit unreadable")
	}
}

func TestNodeControlScopeAndLiteralTargets(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, slurmEnvelope(nodeFields(`["IDLE"]`, "")))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "viewer", cfg.ID)
	token := login(t, mux)
	base := "/api/v1/clusters/" + cfg.ID
	if nodePrepared(t, mux, base+"/nodes/node1", "drain", token).Allowed {
		t.Fatal("viewer allowed")
	}
	before := calls.Load()
	if request(mux, "POST", base+"/nodes/node1/drain", `{}`, token).Code != 403 {
		t.Fatal("viewer mutation allowed")
	}
	for _, path := range []string{"/nodes/ALL/control-context?action=drain", "/nodes/node1/control-context?action=down", "/nodes?sort=script", "/nodes?order=random"} {
		if request(mux, "GET", base+path, "", token).Code != 400 {
			t.Fatal("invalid target or action accepted", path)
		}
	}
	other := cluster(t, app, "other")
	if request(mux, "GET", "/api/v1/clusters/"+other.Id+"/nodes/node1/control-context?action=drain", "", token).Code != 403 {
		t.Fatal("cross-cluster query")
	}
	if calls.Load() != before {
		t.Fatal("rejected input reached upstream")
	}
}
func TestNodeChangedSnapshotAndRevokedGrantPreventWrite(t *testing.T) {
	app, mux := setup(t)
	var reason atomic.Value
	reason.Store("")
	var posts atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
		}
		fmt.Fprint(w, slurmEnvelope(nodeFields(`["IDLE"]`, reason.Load().(string))))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	binding := grant(t, app, person.Id, "operator", cfg.ID)
	token := login(t, mux)
	path := "/api/v1/clusters/" + cfg.ID + "/nodes/node1"
	confirmation := nodePrepared(t, mux, path, "drain", token)
	input := nodecontrol.Input{Proof: confirmation.Proof, Confirmation: "node1", Reason: "maintenance"}
	reason.Store("other operator maintenance")
	if request(mux, "POST", path+"/drain", payload(t, input), token).Code != 409 {
		t.Fatal("stale maintenance snapshot accepted")
	}
	confirmation = nodePrepared(t, mux, path, "drain", token)
	input.Proof = confirmation.Proof
	if err := app.Delete(binding); err != nil {
		t.Fatal(err)
	}
	if request(mux, "POST", path+"/drain", payload(t, input), token).Code != 403 {
		t.Fatal("revoked grant accepted")
	}
	if posts.Load() != 0 {
		t.Fatal("unsafe node write")
	}
}
func TestNodeAuditFailurePreventsUpstreamMutation(t *testing.T) {
	app, mux := setup(t)
	var posts atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
		}
		fmt.Fprint(w, slurmEnvelope(nodeFields(`["IDLE"]`, "")))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "operator", cfg.ID)
	token := login(t, mux)
	path := "/api/v1/clusters/" + cfg.ID + "/nodes/node1"
	confirmation := nodePrepared(t, mux, path, "drain", token)
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error { return fmt.Errorf("audit offline") })
	res := request(mux, "POST", path+"/drain", payload(t, nodecontrol.Input{Proof: confirmation.Proof, Confirmation: "node1", Reason: "maintenance"}), token)
	if res.Code != 503 || posts.Load() != 0 || !strings.Contains(res.Body.String(), "SLURM_AUDIT_UNAVAILABLE") {
		t.Fatal(res.Body.String())
	}
}
func TestNodeUncertainWriteBlocksNewConfirmationAndReconcilesReadOnly(t *testing.T) {
	app, mux := setup(t)
	var drained atomic.Bool
	var posts atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			w.WriteHeader(502)
			return
		}
		state, reason := `["IDLE"]`, ""
		if drained.Load() {
			state, reason = `["IDLE","DRAIN"]`, "maintenance"
		}
		fmt.Fprint(w, slurmEnvelope(nodeFields(state, reason)))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "operator", cfg.ID)
	token := login(t, mux)
	base := "/api/v1/clusters/" + cfg.ID
	path := base + "/nodes/node1"
	confirmation := nodePrepared(t, mux, path, "drain", token)
	input := nodecontrol.Input{Proof: confirmation.Proof, Confirmation: "node1", Reason: "maintenance"}
	for range 2 {
		res := request(mux, "POST", path+"/drain", payload(t, input), token)
		if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"unknown"`) {
			t.Fatal(res.Body.String())
		}
	}
	if nodePrepared(t, mux, path, "drain", token).Allowed || posts.Load() != 1 {
		t.Fatal("uncertain write retried")
	}
	drained.Store(true)
	res := request(mux, "GET", base+"/node-operations/"+confirmation.OperationID, "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"confirmed"`) || posts.Load() != 1 {
		t.Fatal("read reconciliation failed", res.Body.String())
	}
	if !nodePrepared(t, mux, path, "resume", token).Allowed {
		t.Fatal("confirmed drain still blocks resume")
	}
}
func TestResumeWaitsForNodeRegistrationAndKnownUpstreamDenialIsFailed(t *testing.T) {
	app, mux := setup(t)
	var resumed, registered, deny atomic.Bool
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if deny.Load() {
				w.WriteHeader(500)
				fmt.Fprint(w, strings.Replace(slurmEnvelope(`"warnings":[]`), `"errors":[]`, `"errors":[{"error_number":2010,"description":"private upstream denial"}]`, 1))
				return
			}
			resumed.Store(true)
			fmt.Fprint(w, slurmEnvelope(`"warnings":[]`))
			return
		}
		state := `["DOWN"]`
		if resumed.Load() {
			state = `["IDLE","NOT_RESPONDING"]`
		}
		if registered.Load() {
			state = `["IDLE"]`
		}
		fmt.Fprint(w, slurmEnvelope(nodeFields(state, "maintenance")))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "operator", cfg.ID)
	token := login(t, mux)
	base := "/api/v1/clusters/" + cfg.ID
	path := base + "/nodes/node1"
	confirmation := nodePrepared(t, mux, path, "resume", token)
	input := nodecontrol.Input{Proof: confirmation.Proof, Confirmation: "node1", Reason: "recovered"}
	res := request(mux, "POST", path+"/resume", payload(t, input), token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"accepted"`) {
		t.Fatal(res.Body.String())
	}
	registered.Store(true)
	res = request(mux, "GET", base+"/node-operations/"+confirmation.OperationID, "", token)
	if !strings.Contains(res.Body.String(), `"status":"confirmed"`) {
		t.Fatal(res.Body.String())
	}
	deny.Store(true)
	confirmation = nodePrepared(t, mux, path, "drain", token)
	input.Proof = confirmation.Proof
	res = request(mux, "POST", path+"/drain", payload(t, input), token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"failed"`) || strings.Contains(res.Body.String(), "private upstream denial") {
		t.Fatal(res.Body.String())
	}
}
func TestNodeControlConcurrentConfirmationsSendOnlyOneWrite(t *testing.T) {
	app, mux := setup(t)
	started, release := make(chan struct{}), make(chan struct{})
	var posts atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			close(started)
			<-release
			fmt.Fprint(w, slurmEnvelope(`"warnings":[]`))
			return
		}
		fmt.Fprint(w, slurmEnvelope(nodeFields(`["IDLE"]`, "")))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "operator", cfg.ID)
	token := login(t, mux)
	path := "/api/v1/clusters/" + cfg.ID + "/nodes/node1"
	first, second := nodePrepared(t, mux, path, "drain", token), nodePrepared(t, mux, path, "drain", token)
	input := nodecontrol.Input{Proof: first.Proof, Confirmation: "node1", Reason: "maintenance"}
	body := payload(t, input)
	done := make(chan int, 1)
	go func() { done <- request(mux, "POST", path+"/drain", body, token).Code }()
	<-started
	input.Proof = second.Proof
	res := request(mux, "POST", path+"/drain", payload(t, input), token)
	close(release)
	if res.Code != 429 || <-done != 200 || posts.Load() != 1 {
		t.Fatal("concurrent write was not isolated")
	}
}
func TestNodeInvalidationDropsInFlightSnapshot(t *testing.T) {
	app, _ := setup(t)
	started, release := make(chan struct{}), make(chan struct{})
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, slurmEnvelope(nodeFields(`["IDLE"]`, "")))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, _ := manager.Save("", validConnection())
	service := services.SlurmFor(app)
	done := make(chan error, 1)
	go func() { _, err := service.Read(context.Background(), cfg.ID, "node", "node1"); done <- err }()
	<-started
	service.Invalidate(cfg.ID)
	close(release)
	if <-done == nil {
		t.Fatal("pre-control snapshot survived invalidation")
	}
}
