package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/jobcontrol"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
)

func cancelJobFields(state string, uid int64) string {
	return fmt.Sprintf(`"jobs":[{"job_id":42,"name":"test-job","user_name":"alice","user_id":%d,"partition":"normal","job_state":["%s"],"submit_time":{"set":true,"number":1700000000},"array_job_id":{"set":true,"number":0},"array_task_id":{"set":false,"number":0},"het_job_id":{"set":true,"number":0},"het_job_offset":{"set":true,"number":0},"restart_cnt":0}]`, uid, state)
}
func prepared(t *testing.T, mux http.Handler, path, token string) jobcontrol.Prepared {
	t.Helper()
	res := request(mux, "GET", path+"/cancel-context", "", token)
	if res.Code != 200 {
		t.Fatalf("prepare: %d %s", res.Code, res.Body.String())
	}
	var value struct{ Data jobcontrol.Prepared }
	if err := json.Unmarshal(res.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value.Data
}
func TestJobCancellationAuditsBeforeWriteIsIdempotentAndInvalidatesCachedReads(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	var cancelled, audited atomic.Bool
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			calls.Add(1)
			if r.URL.Path != "/slurm/v0.0.43/job/42" || r.URL.Query().Get("signal") != "KILL" {
				t.Error("arbitrary mutation path or signal")
			}
			count, _ := app.CountRecords("operation_audits")
			audited.Store(count == 1)
			cancelled.Store(true)
			fmt.Fprint(w, slurmEnvelope(`"warnings":[]`))
			return
		}
		state := "PENDING"
		if cancelled.Load() {
			state = "CANCELLED"
		}
		fmt.Fprint(w, slurmEnvelope(cancelJobFields(state, 1001)))
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "operator", config.ID)
	if err := jobcontrol.LinkIdentity(app, person.Id, config.ID, "alice", 1001, "own"); err != nil {
		t.Fatal(err)
	}
	token := login(t, mux)
	base := "/api/v1/clusters/" + config.ID
	path := base + "/jobs/42"
	if request(mux, "GET", base+"/jobs", "", token).Code != 200 {
		t.Fatal("prime cache")
	}
	context := prepared(t, mux, path, token)
	if !context.Allowed || context.OperationID == "" || context.Proof == "" {
		t.Fatalf("no cancel permission: %+v", context)
	}
	input := jobcontrol.Input{Proof: context.Proof, Confirmation: "42", Reason: "功能验证取消"}
	bad := input
	bad.Confirmation = "41"
	if request(mux, "POST", path+"/cancel", payload(t, bad), token).Code != 400 {
		t.Fatal("confirmation ignored")
	}
	if request(mux, "POST", path+"/cancel", strings.TrimSuffix(payload(t, input), "}")+`,"signal":"TERM"}`, token).Code != 400 {
		t.Fatal("arbitrary signal accepted")
	}
	for range 2 {
		res := request(mux, "POST", path+"/cancel", payload(t, input), token)
		if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"confirmed"`) {
			t.Fatalf("cancel: %d %s", res.Code, res.Body.String())
		}
	}
	if calls.Load() != 1 || !audited.Load() {
		t.Fatal("duplicate DELETE or missing durable intent")
	}
	res := request(mux, "GET", base+"/jobs", "", token)
	if !strings.Contains(res.Body.String(), "CANCELLED") {
		t.Fatal("pre-cancel cache survived")
	}
	if request(mux, "GET", base+"/job-operations/"+context.OperationID, "", token).Code != 200 {
		t.Fatal("operation evidence unavailable")
	}
	for _, collection := range []string{"slurm_identities", "operation_audits"} {
		if request(mux, "POST", "/api/collections/"+collection+"/records", `{}`, token).Code != 403 {
			t.Fatal("native security collection write allowed")
		}
	}
	count, _ := app.CountRecords("operation_audits")
	if count != 1 {
		t.Fatal("replay duplicated audit evidence")
	}
}
func TestCancellationRequiresExplicitOwnershipAndFreshConfiguration(t *testing.T) {
	app, mux := setup(t)
	var deletes atomic.Int32
	var state atomic.Value
	state.Store("PENDING")
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes.Add(1)
		}
		fmt.Fprint(w, slurmEnvelope(cancelJobFields(state.Load().(string), 1001)))
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "super_admin", "")
	token := login(t, mux)
	path := "/api/v1/clusters/" + config.ID + "/jobs/42"
	if prepared(t, mux, path, token).Allowed {
		t.Fatal("platform admin implicitly impersonated Slurm user")
	}
	if err := jobcontrol.LinkIdentity(app, person.Id, config.ID, "other", 1002, "own"); err != nil {
		t.Fatal(err)
	}
	if prepared(t, mux, path, token).Allowed {
		t.Fatal("foreign owner cancel allowed")
	}
	if err := jobcontrol.LinkIdentity(app, person.Id, config.ID, "alice", 1001, "own"); err != nil {
		t.Fatal(err)
	}
	previous := prepared(t, mux, path, token)
	state.Store("RUNNING")
	input := jobcontrol.Input{Proof: previous.Proof, Confirmation: "42", Reason: "state change test"}
	if res := request(mux, "POST", path+"/cancel", payload(t, input), token); res.Code != 409 {
		t.Fatalf("stale state accepted: %d %s", res.Code, res.Body.String())
	}
	current := prepared(t, mux, path, token)
	input.Proof = current.Proof
	cfg := config.Input
	cfg.Description = "changed"
	if _, err := manager.Save(config.ID, cfg); err != nil {
		t.Fatal(err)
	}
	if request(mux, "POST", path+"/cancel", payload(t, input), token).Code != 409 {
		t.Fatal("changed cluster accepted")
	}
	if deletes.Load() != 0 {
		t.Fatal("unsafe cancellation reached Slurm")
	}
}
func TestUncertainCancellationIsNotRetriedOrReportedAsSuccess(t *testing.T) {
	app, mux := setup(t)
	var deletes atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes.Add(1)
			w.WriteHeader(502)
			fmt.Fprint(w, "private upstream details")
			return
		}
		fmt.Fprint(w, slurmEnvelope(cancelJobFields("PENDING", 1001)))
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "operator", config.ID)
	_ = jobcontrol.LinkIdentity(app, person.Id, config.ID, "alice", 1001, "own")
	token := login(t, mux)
	path := "/api/v1/clusters/" + config.ID + "/jobs/42"
	context := prepared(t, mux, path, token)
	input := jobcontrol.Input{Proof: context.Proof, Confirmation: "42", Reason: "网络故障测试"}
	for range 2 {
		res := request(mux, "POST", path+"/cancel", payload(t, input), token)
		if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"unknown"`) || strings.Contains(res.Body.String(), "private upstream details") {
			t.Fatalf("unsafe uncertainty handling: %s", res.Body.String())
		}
	}
	if deletes.Load() != 1 {
		t.Fatal("uncertain mutation retried")
	}
	if prepared(t, mux, path, token).Allowed {
		t.Fatal("new confirmation bypassed unresolved operation")
	}
}
func TestMutationInvalidationDropsInFlightPreCancelSnapshot(t *testing.T) {
	app, _ := setup(t)
	started, release := make(chan struct{}), make(chan struct{})
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, slurmEnvelope(cancelJobFields("PENDING", 1001)))
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	service := services.SlurmFor(app)
	done := make(chan error, 1)
	go func() { _, err := service.Read(context.Background(), config.ID, "jobs", ""); done <- err }()
	<-started
	service.Invalidate(config.ID)
	close(release)
	if err := <-done; client.Code(err) != "data_changed" {
		t.Fatalf("in-flight stale read survived mutation: %v", err)
	}
}
func TestViewerCannotCancelAndOtherActorCannotReadAudit(t *testing.T) {
	app, mux := setup(t)
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, slurmEnvelope(cancelJobFields("PENDING", 1001)))
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "viewer", config.ID)
	token := login(t, mux)
	path := "/api/v1/clusters/" + config.ID + "/jobs/42"
	if prepared(t, mux, path, token).Allowed {
		t.Fatal("viewer got cancellation context")
	}
	if request(mux, "POST", path+"/cancel", `{}`, token).Code != 403 {
		t.Fatal("viewer cancellation")
	}
	collection, _ := app.FindCollectionByNameOrId("operation_audits")
	record := core.NewRecord(collection)
	for k, v := range map[string]any{"operation_key": "other-actor-operation", "actor_id": "somebody-else", "cluster_id": config.ID, "job_id": "42", "action": "jobs.cancel", "status": "failed"} {
		record.Set(k, v)
	}
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	if request(mux, "GET", "/api/v1/clusters/"+config.ID+"/job-operations/other-actor-operation", "", token).Code != 403 {
		t.Fatal("other actor read audit")
	}
}

func TestCancellationReconcilesByReadWithoutRepeatingMutation(t *testing.T) {
	app, mux := setup(t)
	var deletes atomic.Int32
	var cancelled atomic.Bool
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes.Add(1)
			fmt.Fprint(w, slurmEnvelope(`"warnings":[]`))
			return
		}
		state := "PENDING"
		if cancelled.Load() {
			state = "CANCELLED"
		}
		fmt.Fprint(w, slurmEnvelope(cancelJobFields(state, 1001)))
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "operator", config.ID)
	if err := jobcontrol.LinkIdentity(app, person.Id, config.ID, "alice", 1001, "own"); err != nil {
		t.Fatal(err)
	}
	token := login(t, mux)
	base := "/api/v1/clusters/" + config.ID
	path := base + "/jobs/42"
	confirmation := prepared(t, mux, path, token)
	input := jobcontrol.Input{Proof: confirmation.Proof, Confirmation: "42", Reason: "asynchronous cancellation"}
	res := request(mux, "POST", path+"/cancel", payload(t, input), token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"accepted"`) {
		t.Fatal(res.Body.String())
	}
	cancelled.Store(true)
	res = request(mux, "GET", base+"/job-operations/"+confirmation.OperationID, "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"confirmed"`) {
		t.Fatal(res.Body.String())
	}
	if deletes.Load() != 1 {
		t.Fatal("read reconciliation repeated DELETE")
	}
}
func TestAuditFailureAndRevokedIdentityPreventCancellation(t *testing.T) {
	app, mux := setup(t)
	var deletes atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes.Add(1)
		}
		fmt.Fprint(w, slurmEnvelope(cancelJobFields("PENDING", 1001)))
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "operator", config.ID)
	if err := jobcontrol.LinkIdentity(app, person.Id, config.ID, "alice", 1001, "own"); err != nil {
		t.Fatal(err)
	}
	token := login(t, mux)
	path := "/api/v1/clusters/" + config.ID + "/jobs/42"
	confirmation := prepared(t, mux, path, token)
	input := jobcontrol.Input{Proof: confirmation.Proof, Confirmation: "42", Reason: "audit fail closed"}
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error { return fmt.Errorf("audit storage unavailable") })
	res := request(mux, "POST", path+"/cancel", payload(t, input), token)
	if res.Code != 503 || !strings.Contains(res.Body.String(), "SLURM_AUDIT_UNAVAILABLE") {
		t.Fatal(res.Body.String())
	}
	if err := jobcontrol.LinkIdentity(app, person.Id, config.ID, "other", 1002, "own"); err != nil {
		t.Fatal(err)
	}
	if request(mux, "POST", path+"/cancel", payload(t, input), token).Code != 403 {
		t.Fatal("changed identity accepted")
	}
	if deletes.Load() != 0 {
		t.Fatal("mutation sent despite audit failure or revoked mapping")
	}
}

func TestConcurrentConfirmationsSendOnlyOneCancellation(t *testing.T) {
	app, mux := setup(t)
	started, release := make(chan struct{}), make(chan struct{})
	var deletes atomic.Int32
	var cancelled atomic.Bool
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes.Add(1)
			close(started)
			<-release
			cancelled.Store(true)
			fmt.Fprint(w, slurmEnvelope(`"warnings":[]`))
			return
		}
		state := "PENDING"
		if cancelled.Load() {
			state = "CANCELLED"
		}
		fmt.Fprint(w, slurmEnvelope(cancelJobFields(state, 1001)))
	})
	manager := clusterconfig.Register(app, registry)
	config, _ := manager.Save("", validConnection())
	person := user(t, app, "users")
	grant(t, app, person.Id, "operator", config.ID)
	if err := jobcontrol.LinkIdentity(app, person.Id, config.ID, "alice", 1001, "own"); err != nil {
		t.Fatal(err)
	}
	token := login(t, mux)
	path := "/api/v1/clusters/" + config.ID + "/jobs/42"
	first, second := prepared(t, mux, path, token), prepared(t, mux, path, token)
	input := jobcontrol.Input{Proof: first.Proof, Confirmation: "42", Reason: "concurrent cancellation"}
	body := payload(t, input)
	finished := make(chan int, 1)
	go func() { finished <- request(mux, "POST", path+"/cancel", body, token).Code }()
	<-started
	input.Proof = second.Proof
	other := request(mux, "POST", path+"/cancel", payload(t, input), token)
	close(release)
	if other.Code != 429 {
		t.Fatalf("concurrent mutation accepted: %d %s", other.Code, other.Body.String())
	}
	if code := <-finished; code != 200 {
		t.Fatalf("first mutation failed: %d", code)
	}
	if deletes.Load() != 1 {
		t.Fatal("concurrent confirmations sent duplicate DELETE")
	}
}
