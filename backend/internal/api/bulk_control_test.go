package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"slurm-console/backend/internal/bulkcontrol"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/jobcontrol"
)

func bulkView(t *testing.T, mux http.Handler, method, path, body, token string) bulkcontrol.View {
	t.Helper()
	r := request(mux, method, path, body, token)
	if r.Code != 200 {
		t.Fatalf("%s %s: %d %s", method, path, r.Code, r.Body.String())
	}
	var v struct{ Data bulkcontrol.View }
	if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v.Data
}
func TestBulkCancellationPartialResultsIdempotencyAndReadRecovery(t *testing.T) {
	app, mux := setup(t)
	var mu sync.Mutex
	cancelled := map[string]bool{}
	calls := map[string]int{}
	var audited atomic.Bool
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/slurm/v0.0.43/job/")
		if r.Method == "DELETE" {
			count, _ := app.CountRecords("operation_audits")
			if count > 0 {
				audited.Store(true)
			}
			mu.Lock()
			calls[id]++
			if id != "43" {
				cancelled[id] = true
			}
			mu.Unlock()
			if id == "43" {
				w.WriteHeader(403)
				fmt.Fprint(w, "private rejection")
				return
			}
			fmt.Fprint(w, slurmEnvelope(`"warnings":[]`))
			return
		}
		mu.Lock()
		state := "PENDING"
		if cancelled[id] {
			state = "CANCELLED"
		}
		mu.Unlock()
		fields := strings.Replace(cancelJobFields(state, 1001), `"job_id":42`, `"job_id":`+id, 1)
		fmt.Fprint(w, slurmEnvelope(fields))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, err := manager.Save("", validConnection())
	if err != nil {
		t.Fatal(err)
	}
	actor := user(t, app, "users")
	grant(t, app, actor.Id, "operator", cfg.ID)
	if err := jobcontrol.LinkIdentity(app, actor.Id, cfg.ID, "alice", 1001, "own"); err != nil {
		t.Fatal(err)
	}
	token := login(t, mux)
	base := "/api/v1/clusters/" + cfg.ID + "/bulk-control"
	p := bulkView(t, mux, "POST", base+"/prepare", `{"action":"jobs.cancel","targets":["42","43"]}`, token)
	if !p.Allowed || p.ID == "" || len(p.Items) != 2 {
		t.Fatal(p)
	}
	if strings.Contains(payload(t, p), "Proof") || strings.Contains(payload(t, p), `"proof"`) {
		t.Fatal("member proofs exposed")
	}
	input := `{"confirmation":"CANCEL 2","reason":"批量取消验证"}`
	if r := request(mux, "POST", base+"/"+p.ID+"/execute", `{"confirmation":"CANCEL 3","reason":"批量取消验证"}`, token); r.Code != 400 {
		t.Fatal("count confirmation ignored")
	}
	for range 2 {
		bulkView(t, mux, "POST", base+"/"+p.ID+"/execute", input, token)
	}
	var done bulkcontrol.View
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		done = bulkView(t, mux, "GET", base+"/"+p.ID, "", token)
		if done.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if done.Status != "completed" || done.Counts["confirmed"] != 1 || done.Counts["failed"] != 1 {
		t.Fatalf("partial outcome %+v", done)
	}
	mu.Lock()
	deletes := calls["42"] + calls["43"]
	mu.Unlock()
	if deletes != 2 || !audited.Load() {
		t.Fatal("write repeated or not audited", deletes)
	}
	if r := request(mux, "POST", base+"/"+p.ID+"/execute", `{"confirmation":"CANCEL 2","reason":"变更原因"}`, token); r.Code != 409 {
		t.Fatal("changed replay accepted")
	}
	ids := p.Items[0].OperationID + "," + p.Items[1].OperationID
	app.Store().Remove("platform.bulkControl") // Group contexts are not a durable task queue.
	if r := request(mux, "GET", base+"/"+p.ID, "", token); r.Code != 404 {
		t.Fatal("lost in-memory group survived restart")
	}
	recovered := bulkView(t, mux, "GET", base+"/operations?resource=job&ids="+ids, "", token)
	if recovered.Counts["confirmed"] != 1 || recovered.Counts["failed"] != 1 {
		t.Fatal(recovered)
	}
	mu.Lock()
	deletes = calls["42"] + calls["43"]
	mu.Unlock()
	if deletes != 2 {
		t.Fatal("read recovery repeated a write")
	}
	other := cluster(t, app, "other-bulk")
	if r := request(mux, "GET", "/api/v1/clusters/"+other.Id+"/bulk-control/operations?resource=job&ids="+ids, "", token); r.Code != 403 {
		t.Fatal("cross cluster results")
	}
	records, _ := app.FindRecordsByFilter("role_permissions", "permission.code='jobs.read'", "", 0, 0)
	for _, r := range records {
		if err := app.Delete(r); err != nil {
			t.Fatal(err)
		}
	}
	if r := request(mux, "GET", base+"/operations?resource=job&ids="+ids, "", token); r.Code != 403 {
		t.Fatal("revoked read bypass")
	}
}
func TestBulkValidatesWholeSelectionAndRetainsSingleJobSafety(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		id := strings.TrimPrefix(r.URL.Path, "/slurm/v0.0.43/job/")
		uid := int64(1001)
		if id == "43" {
			uid = 0
		}
		fmt.Fprint(w, slurmEnvelope(strings.Replace(cancelJobFields("PENDING", uid), `"job_id":42`, `"job_id":`+id, 1)))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, _ := manager.Save("", validConnection())
	actor := user(t, app, "users")
	grant(t, app, actor.Id, "operator", cfg.ID)
	_ = jobcontrol.LinkIdentity(app, actor.Id, cfg.ID, "alice", 1001, "own")
	token := login(t, mux)
	base := "/api/v1/clusters/" + cfg.ID + "/bulk-control"
	for _, body := range []string{`{"action":"jobs.cancel","targets":[]}`, `{"action":"jobs.cancel","targets":["42","42"]}`, `{"action":"jobs.cancel","targets":["42,43"]}`, `{"action":"jobs.cancel","targets":["42_1"]}`, `{"action":"nodes.resume","targets":["ALL"]}`, `{"action":"nodes.down","targets":["node1"]}`, `{"action":"jobs.cancel","targets":["42"],"signal":"TERM"}`} {
		if r := request(mux, "POST", base+"/prepare", body, token); r.Code != 400 {
			t.Fatal(body, r.Code, r.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid selector reached source")
	}
	p := bulkView(t, mux, "POST", base+"/prepare", `{"action":"jobs.cancel","targets":["42","43"]}`, token)
	if p.Allowed || p.Status != "blocked" || p.ID != "" || p.Items[1].Allowed {
		t.Fatal("root job accepted", p)
	}
	if r := request(mux, "POST", base+"/prepare", `{"action":"jobs.cancel","targets":["42"]}`, ""); r.Code != 401 {
		t.Fatal("anonymous preparation")
	}
	// Revocation before preparation is checked before upstream reads.
	records, _ := app.FindRecordsByFilter("role_permissions", "permission.code='jobs.cancel'", "", 0, 0)
	for _, r := range records {
		_ = app.Delete(r)
	}
	before := calls.Load()
	if _, err := bulkcontrol.For(app).Prepare(context.Background(), actor.Id, cfg.ID, "jobs.cancel", []string{"42"}); err == nil || calls.Load() != before {
		t.Fatal("prepare bypassed revoked control permission")
	}
}

func TestBulkRechecksRevocationForQueuedTargets(t *testing.T) {
	app, mux := setup(t)
	var mu sync.Mutex
	cancelled := map[string]bool{}
	writes := map[string]int{}
	arrived := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var started atomic.Int32
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/slurm/v0.0.43/job/")
		if r.Method == "DELETE" {
			mu.Lock()
			writes[id]++
			cancelled[id] = true
			mu.Unlock()
			if started.Add(1) == 3 {
				close(arrived)
			}
			<-release
			fmt.Fprint(w, slurmEnvelope(`"warnings":[]`))
			return
		}
		mu.Lock()
		state := "PENDING"
		if cancelled[id] {
			state = "CANCELLED"
		}
		mu.Unlock()
		fmt.Fprint(w, slurmEnvelope(strings.Replace(cancelJobFields(state, 1001), `"job_id":42`, `"job_id":`+id, 1)))
	})
	cfg, _ := clusterconfig.Register(app, registry).Save("", validConnection())
	actor := user(t, app, "users")
	grant(t, app, actor.Id, "operator", cfg.ID)
	if err := jobcontrol.LinkIdentity(app, actor.Id, cfg.ID, "alice", 1001, "own"); err != nil {
		t.Fatal(err)
	}
	token := login(t, mux)
	base := "/api/v1/clusters/" + cfg.ID + "/bulk-control"
	p := bulkView(t, mux, "POST", base+"/prepare", `{"action":"jobs.cancel","targets":["42","43","44","45"]}`, token)
	bulkView(t, mux, "POST", base+"/"+p.ID+"/execute", `{"confirmation":"CANCEL 4","reason":"队列撤权验证"}`, token)
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("three worker slots did not start")
	}
	records, _ := app.FindRecordsByFilter("role_permissions", "permission.code='jobs.cancel'", "", 0, 0)
	for _, r := range records {
		if err := app.Delete(r); err != nil {
			t.Fatal(err)
		}
	}
	once.Do(func() { close(release) })
	var result bulkcontrol.View
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result = bulkView(t, mux, "GET", base+"/"+p.ID, "", token)
		if result.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if result.Status != "completed" || result.Items[3].Status != "failed" || result.Items[3].Code != "operation_forbidden" {
		t.Fatal("queued revocation bypass", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes["45"] != 0 {
		t.Fatal("queued target sent after revocation")
	}
}
