package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"net/http"
	"net/http/httptest"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/gpumetrics"
	notices "slurm-console/backend/internal/notifications"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func createRule(t *testing.T, mux http.Handler, token, cid string) notices.Rule {
	t.Helper()
	r := request(mux, "POST", "/api/v1/notification-rules", payload(t, map[string]any{"clusterId": cid, "name": "Node alert", "kind": "node_unavailable", "severity": "critical", "enabled": true, "threshold": 1, "holdMinutes": 0}), token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var v struct{ Data notices.Rule }
	_ = json.Unmarshal(r.Body.Bytes(), &v)
	return v.Data
}
func expireNotificationCheck(t *testing.T, app core.App, id string) {
	t.Helper()
	r, e := app.FindRecordById("notification_rules", id)
	if e != nil {
		t.Fatal(e)
	}
	r.Set("last_checked", time.Now().Add(-10*time.Second))
	if e := app.Save(r); e != nil {
		t.Fatal(e)
	}
}
func TestNotificationsIncidentDedupRecoveryOwnershipAndRevocation(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	c := cluster(t, app, "notify")
	c.Set("enabled", true)
	if e := app.Save(c); e != nil {
		t.Fatal(e)
	}
	binding := grant(t, app, u.Id, "viewer", c.Id)
	token := login(t, mux)
	var mode atomic.Int32
	var reads atomic.Int32
	service := notices.Register(app, func(context.Context, string, string, int, time.Time) notices.Observation {
		reads.Add(1)
		switch mode.Load() {
		case 1:
			return notices.Observation{Summary: "source unavailable"}
		case 2:
			return notices.Observation{Known: true, Summary: "healthy", Samples: []string{}}
		}
		return notices.Observation{Known: true, Breached: true, Count: 1, Samples: []string{"node1"}, Summary: "one DOWN node"}
	})
	rule := createRule(t, mux, token, c.Id)
	events, unsubscribe, e := service.Subscribe(u.Id)
	if e != nil {
		t.Fatal(e)
	}
	defer unsubscribe()
	check := func() {
		t.Helper()
		r := request(mux, "POST", "/api/v1/notification-rules/"+rule.ID+"/check", "{}", token)
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	check()
	check()
	if reads.Load() != 1 {
		t.Fatal("unbounded duplicate check")
	}
	select {
	case <-events:
	default:
		t.Fatal("missing SSE invalidation")
	}
	expireNotificationCheck(t, app, rule.ID)
	check()
	n, _ := app.CountRecords("notifications")
	if n != 1 {
		t.Fatal("duplicate incident", n)
	}
	mode.Store(1)
	expireNotificationCheck(t, app, rule.ID)
	check()
	r, _ := app.FindRecordById("notification_rules", rule.ID)
	if r.GetString("state") != "unknown" || !r.GetBool("incident_active") {
		t.Fatal("false recovery")
	}
	mode.Store(2)
	expireNotificationCheck(t, app, rule.ID)
	check()
	n, _ = app.CountRecords("notifications")
	if n != 2 {
		t.Fatal("missing recovery", n)
	}
	rows, e := app.FindRecordsByFilter("notifications", "", "created", 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	out := request(mux, "GET", "/api/v1/notifications?perPage=1", "", token)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"unreadCount":2`) || !strings.Contains(out.Body.String(), `"totalItems":2`) {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := request(mux, "POST", "/api/v1/notifications/"+rows[0].Id+"/read", `{"read":true}`, token); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := request(mux, "POST", "/api/v1/notifications/read-all", "{}", token); out.Code != 200 || !strings.Contains(out.Body.String(), `"updated":1`) {
		t.Fatal(out.Code, out.Body.String())
	}

	if _, e := app.DB().NewQuery("UPDATE notifications SET created='2000-01-01 00:00:00.000Z' WHERE id={:id}").Bind(dbx.Params{"id": rows[0].Id}).Execute(); e != nil {
		t.Fatal(e)
	}
	if out := request(mux, "GET", "/api/v1/notifications", "", token); out.Code != 200 || !strings.Contains(out.Body.String(), `"totalItems":1`) {
		t.Fatal("expired history visible", out.Code, out.Body.String())
	}
	// Another platform Super Admin cannot read or mutate the owner's notification or rule.
	other := core.NewRecord(u.Collection())
	other.SetEmail("notice-other@example.test")
	other.SetPassword(fixturePassword)
	other.Set("active", true)
	other.Set("auth_source", "local")
	if e := app.Save(other); e != nil {
		t.Fatal(e)
	}
	grant(t, app, other.Id, "super_admin", "")
	otherToken, _ := other.NewAuthToken()
	for _, path := range []string{"/api/v1/notification-rules", "/api/v1/notifications"} {
		out := request(mux, "GET", path, "", otherToken)
		if out.Code != 200 || !strings.Contains(out.Body.String(), `"items":[]`) {
			t.Fatal("cross owner read", out.Code, out.Body.String())
		}
	}
	for _, call := range []struct{ method, path, body string }{{"DELETE", "/api/v1/notification-rules/" + rule.ID, `{"revision":1}`}, {"POST", "/api/v1/notification-rules/" + rule.ID + "/check", "{}"}, {"POST", "/api/v1/notifications/" + rows[0].Id + "/read", `{"read":true}`}} {
		if out := request(mux, call.method, call.path, call.body, otherToken); out.Code != 404 {
			t.Fatal("cross owner mutation", out.Code)
		}
	}
	for _, col := range []string{"notifications", "notification_rules"} {
		for _, method := range []string{"GET", "POST"} {
			if out := request(mux, method, "/api/collections/"+col+"/records", "{}", token); out.Code != 403 {
				t.Fatal("native API open", col, out.Code)
			}
		}
	}
	if e := app.Delete(binding); e != nil {
		t.Fatal(e)
	}
	if out := request(mux, "GET", "/api/v1/notifications", "", token); out.Code != 200 || !strings.Contains(out.Body.String(), `"items":[]`) {
		t.Fatal("revoked notification data escaped", out.Body.String())
	}
	if out := request(mux, "GET", "/api/v1/notification-rules", "", token); out.Code != 200 || !strings.Contains(out.Body.String(), `"usable":false`) || strings.Contains(out.Body.String(), "node1") {
		t.Fatal("revoked rule evidence escaped", out.Body.String())
	}
	expireNotificationCheck(t, app, rule.ID)
	if _, e := service.Evaluate(context.Background(), u.Id, rule.ID); e != notices.ErrForbidden {
		t.Fatal("revocation did not stop reads", e)
	}
	if reads.Load() != 4 {
		t.Fatal("source read after revocation", reads.Load())
	}
	if out := request(mux, "DELETE", "/api/v1/notification-rules/"+rule.ID, `{"revision":1}`, token); out.Code != 200 {
		t.Fatal("owner cleanup denied", out.Code)
	}
	n, _ = app.CountRecords("notifications")
	if n != 2 {
		t.Fatal("rule deletion lost history")
	}
}
func TestNotificationRulesBoundsCASAndNoAutomaticControl(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	a := cluster(t, app, "notice-a")
	a.Set("enabled", true)
	_ = app.Save(a)
	b := cluster(t, app, "notice-b")
	b.Set("enabled", true)
	_ = app.Save(b)
	grant(t, app, u.Id, "viewer", a.Id)
	token := login(t, mux)
	rule := createRule(t, mux, token, a.Id)
	input := map[string]any{"clusterId": a.Id, "name": "new", "kind": "node_unavailable", "severity": "warning", "enabled": false, "threshold": 1, "holdMinutes": 2, "revision": 1}
	if out := request(mux, "PATCH", "/api/v1/notification-rules/"+rule.ID, payload(t, input), token); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := request(mux, "PATCH", "/api/v1/notification-rules/"+rule.ID, payload(t, input), token); out.Code != 409 {
		t.Fatal("stale update", out.Code)
	}
	if out := request(mux, "DELETE", "/api/v1/notification-rules/"+rule.ID, `{"revision":1}`, token); out.Code != 409 {
		t.Fatal("stale delete", out.Code)
	}
	input["clusterId"] = b.Id
	delete(input, "revision")
	if out := request(mux, "POST", "/api/v1/notification-rules", payload(t, input), token); out.Code != 403 {
		t.Fatal("cross cluster", out.Code)
	}
	input["clusterId"] = a.Id
	delete(input, "holdMinutes")
	if out := request(mux, "POST", "/api/v1/notification-rules", payload(t, input), token); out.Code != 400 {
		t.Fatal("implicit zero hold allowed", out.Code)
	}
	input["holdMinutes"] = 2
	input["revision"] = 0
	if out := request(mux, "POST", "/api/v1/notification-rules", payload(t, input), token); out.Code != 400 {
		t.Fatal("create revision accepted", out.Code)
	}
	delete(input, "revision")
	for _, field := range []string{"owner", "endpoint", "recipient", "command", "webhook", "proof"} {
		input[field] = "unsafe"
		if out := request(mux, "POST", "/api/v1/notification-rules", payload(t, input), token); out.Code != 400 {
			t.Fatal("unknown field", field, out.Code)
		}
		delete(input, field)
	}
	for i := 1; i < notices.MaxPerUser; i++ {
		input["name"] = fmt.Sprint("rule", i)
		if out := request(mux, "POST", "/api/v1/notification-rules", payload(t, input), token); out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
	}
	if out := request(mux, "POST", "/api/v1/notification-rules", payload(t, input), token); out.Code != 409 {
		t.Fatal("unbounded rules", out.Code)
	}
	for _, path := range []string{"/api/v1/notifications?owner=another", "/api/v1/notifications?phase=invalid", "/api/v1/notifications?read=unread&read=read", "/api/v1/notification-rules?command=unsafe", "/api/v1/notifications/stream?token=secret"} {
		if out := request(mux, "GET", path, "", token); out.Code != 400 {
			t.Fatal("invalid query", path, out.Code)
		}
	}
	for _, path := range []string{"/api/v1/notifications", "/api/v1/notification-rules", "/api/v1/notifications/stream"} {
		if out := request(mux, "GET", path, "", ""); out.Code != 401 {
			t.Fatal("anonymous", out.Code)
		}
	}
	n, _ := app.CountRecords("operation_audits")
	if n != 0 {
		t.Fatal("preferences performed controls")
	}
}
func TestNotificationSSEHintsAndDisconnect(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	token, _ := u.NewAuthToken()
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/notifications/stream", nil)
	req.Header.Set("Authorization", token)
	resp, e := server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal("SSE unavailable", resp.StatusCode)
	}
	b := make([]byte, 64)
	n, e := resp.Body.Read(b)
	if e != nil || !strings.Contains(string(b[:n]), "event: ready") {
		t.Fatal("missing ready", e)
	}
	notices.For(app).Changed(u.Id)
	n, e = resp.Body.Read(b)
	if e != nil || !strings.Contains(string(b[:n]), "event: changed") || strings.Contains(string(b[:n]), u.Id) {
		t.Fatal("invalid hint", e, string(b[:n]))
	}
	cancel()
}

func TestNotificationRechecksAuthorizationAndConnectionBeforePersist(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	c := cluster(t, app, "inflight-notice")
	c.Set("enabled", true)
	_ = app.Save(c)
	binding := grant(t, app, u.Id, "viewer", c.Id)
	token := login(t, mux)
	rule := createRule(t, mux, token, c.Id)
	s := notices.Register(app, func(context.Context, string, string, int, time.Time) notices.Observation {
		if e := app.Delete(binding); e != nil {
			t.Fatal(e)
		}
		return notices.Observation{Known: true, Breached: true, Count: 1, Samples: []string{"private-node"}}
	})
	if _, e := s.Evaluate(context.Background(), u.Id, rule.ID); e != notices.ErrForbidden {
		t.Fatal("in-flight revocation not enforced", e)
	}
	n, _ := app.CountRecords("notifications")
	if n != 0 {
		t.Fatal("unauthorized notification persisted")
	}
	grant(t, app, u.Id, "viewer", c.Id)
	s = notices.Register(app, func(context.Context, string, string, int, time.Time) notices.Observation {
		r, _ := app.FindRecordById("clusters", c.Id)
		r.Set("revision", r.GetInt("revision")+1)
		if e := app.Save(r); e != nil {
			t.Fatal(e)
		}
		return notices.Observation{Known: true, Breached: true, Count: 1}
	})
	if _, e := s.Evaluate(context.Background(), u.Id, rule.ID); e != notices.ErrConflict {
		t.Fatal("changed source persisted", e)
	}
	u.Set("auth_source", "pam")
	u.Set("username", "notify_fixture")
	if e := app.Save(u); e != nil {
		t.Fatal(e)
	}
	reads := 0
	s = notices.Register(app, func(context.Context, string, string, int, time.Time) notices.Observation {
		reads++
		return notices.Observation{Known: true}
	})
	if _, e := s.Evaluate(context.Background(), u.Id, rule.ID); e != notices.ErrForbidden || reads != 0 {
		t.Fatal("unavailable external account was evaluated", e, reads)
	}
}

func TestNotificationRealSourcesAndIncompleteTelemetry(t *testing.T) {
	app, _ := setup(t)
	var mode atomic.Int32
	now := time.Now().UTC()
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if mode.Load() == 1 {
			w.WriteHeader(503)
			return
		}
		switch r.URL.Path {
		case "/slurm/v0.0.43/nodes/":
			fmt.Fprint(w, slurmEnvelope(strings.Replace(fixtureNodes, `["IDLE"]`, `["DOWN"]`, 1)))
		case "/slurm/v0.0.43/jobs/":
			fmt.Fprint(w, slurmEnvelope(fmt.Sprintf(`"jobs":[{"job_id":7,"job_state":["PENDING"],"submit_time":{"set":true,"infinite":false,"number":%d}},{"job_id":8,"job_state":["PENDING"]}]`, now.Add(-2*time.Hour).Unix())))
		default:
			writeFixture(w, r)
		}
	})
	manager := clusterconfig.Register(app, registry)
	c, e := manager.Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	source := notices.Source(app)
	for _, sample := range []struct {
		kind          string
		threshold     int
		known, breach bool
		count         int
	}{{"cluster_health", 1, true, false, 0}, {"node_unavailable", 1, true, true, 1}, {"node_unavailable", 2, true, false, 1}, {"job_pending", 60, true, true, 1}, {"job_pending", 180, false, false, 0}, {"gpu_temperature", 85, false, false, 0}} {
		o := source(context.Background(), c.ID, sample.kind, sample.threshold, now)
		if o.Known != sample.known || o.Breached != sample.breach || o.Count != sample.count {
			t.Fatalf("%s %d: %+v", sample.kind, sample.threshold, o)
		}
	}
	mode.Store(1)
	r, _ := app.FindRecordById("clusters", c.ID)
	r.Set("revision", r.GetInt("revision")+1)
	_ = app.Save(r)
	if o := source(context.Background(), c.ID, "cluster_health", 1, now); !o.Known || !o.Breached {
		t.Fatal("health connection failure not surfaced", o)
	}
	var gpuMode atomic.Int32
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts := now.Unix()
		value := "90"
		if gpuMode.Load() == 1 {
			ts -= 180
		}
		if gpuMode.Load() == 2 {
			value = "40"
		}
		rows := []map[string]any{{"metric": map[string]string{"__name__": "DCGM_FI_DEV_GPU_TEMP", "cluster": "fixture", "Hostname": "node1", "UUID": "GPU-test"}, "values": []any{[]any{ts, value}}}}
		if gpuMode.Load() == 3 {
			rows = append(rows, map[string]any{"metric": map[string]string{"__name__": "DCGM_FI_DEV_GPU_UTIL", "cluster": "fixture", "Hostname": "node2", "UUID": "GPU-missing"}, "values": []any{[]any{ts, "1"}}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": rows}})
	}))
	defer prom.Close()
	metrics, e := gpumetrics.NewRegistry(map[string]gpumetrics.Profile{c.ID: {Label: "test", Endpoint: prom.URL, ClusterLabel: "cluster", ClusterValue: "fixture", NodeLabel: "Hostname", UtilizationUnit: "percent"}})
	if e != nil {
		t.Fatal(e)
	}
	for _, sample := range []struct {
		mode          int32
		known, breach bool
	}{{0, true, true}, {1, false, false}, {2, true, false}, {3, true, true}} {
		gpuMode.Store(sample.mode)
		gpumetrics.Register(app, metrics)
		o := source(context.Background(), c.ID, "gpu_temperature", 85, now)
		if o.Known != sample.known || o.Breached != sample.breach {
			t.Fatal("GPU evidence", sample, o)
		}
	}
}

func TestNotificationOwnershipPrecedesInflightBusyState(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	c := cluster(t, app, "notice-private-busy")
	c.Set("enabled", true)
	_ = app.Save(c)
	grant(t, app, u.Id, "viewer", c.Id)
	token := login(t, mux)
	other := core.NewRecord(u.Collection())
	other.SetEmail("other-busy@example.test")
	other.SetPassword(fixturePassword)
	other.Set("active", true)
	other.Set("auth_source", "local")
	if e := app.Save(other); e != nil {
		t.Fatal(e)
	}
	grant(t, app, other.Id, "super_admin", "")
	otherToken, _ := other.NewAuthToken()
	entered, finish := make(chan struct{}), make(chan struct{})
	s := notices.Register(app, func(context.Context, string, string, int, time.Time) notices.Observation {
		close(entered)
		<-finish
		return notices.Observation{Known: true, Summary: "healthy"}
	})
	rule := createRule(t, mux, token, c.Id)
	done := make(chan error, 1)
	go func() { _, e := s.Evaluate(context.Background(), u.Id, rule.ID); done <- e }()
	<-entered
	out := request(mux, "POST", "/api/v1/notification-rules/"+rule.ID+"/check", "{}", otherToken)
	close(finish)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if out.Code != 404 {
		t.Fatal("another owner's in-flight state leaked", out.Code, out.Body.String())
	}
}
