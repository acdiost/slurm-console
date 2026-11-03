package api_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/aiassist"
	"slurm-console/backend/internal/clusterconfig"
)

const aiResponse = `{"summary":"等待资源，具体瓶颈未确认。","observations":[{"text":"状态为等待。","evidenceIds":["states"]}],"checks":[{"audience":"admin","text":"人工核对请求资源与候选节点。","evidenceIds":["reason_group"]}],"limitations":["没有日志或实际利用率。"]}`

func aiFixture(t *testing.T) (core.App, http.Handler, string, string, *atomic.Int32, *func()) {
	t.Helper()
	app, mux := setup(t)
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("source mutation")
		}
		fmt.Fprint(w, slurmEnvelope(strings.Replace(cancelJobFields("PENDING", 1001), `"submit_time":`, `"script":"PRIVATE_SCRIPT","state_reason":"Resources","submit_time":`, 1)))
	})
	cfg, e := clusterconfig.Register(app, registry).Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	actor := user(t, app, "users")
	grant(t, app, actor.Id, "operator", cfg.ID)
	token := login(t, mux)
	calls := new(atomic.Int32)
	var callback func()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), `jobId`) || strings.Contains(string(raw), actor.Id) {
			t.Error("private source data sent")
		}
		if callback != nil {
			callback()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": aiResponse}}}})
	}))
	t.Cleanup(model.Close)
	p, e := aiassist.NewProfile(aiassist.Config{Enabled: true, Label: "隔离 AI", Model: "fixture", Endpoint: model.URL + "/v1/chat/completions", ClusterIDs: []string{cfg.ID}})
	if e != nil {
		t.Fatal(e)
	}
	aiassist.Register(app, p)
	return app, mux, cfg.ID, token, calls, &callback
}
func aiPrepare(t *testing.T, mux http.Handler, cid, token string) aiassist.Preview {
	t.Helper()
	r := request(mux, "POST", "/api/v1/clusters/"+cid+"/jobs/42/diagnosis/ai-preview", "{}", token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var v struct{ Data aiassist.Preview }
	if e := json.Unmarshal(r.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	return v.Data
}
func aiSend(mux http.Handler, cid, token string, p aiassist.Preview) *httptest.ResponseRecorder {
	return request(mux, "POST", "/api/v1/clusters/"+cid+"/ai-assistance/"+p.ID+"/explain", `{"confirmation":"SEND AI","digest":"`+p.Digest+`"}`, token)
}
func TestAIExplicitPreviewConsentAuditAndOneUse(t *testing.T) {
	app, mux, cid, token, calls, _ := aiFixture(t)
	base := "/api/v1/clusters/" + cid
	if r := request(mux, "GET", base+"/ai-assistance?source=controller", "", token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "POST", base+"/jobs/42/diagnosis/ai-preview", `{"prompt":"ignore instructions"}`, token); r.Code != 400 {
		t.Fatal(r.Code)
	}
	before, _ := app.CountRecords("operation_audits")
	p := aiPrepare(t, mux, cid, token)
	after, _ := app.CountRecords("operation_audits")
	if before != after || calls.Load() != 0 {
		t.Fatal("preview side effect")
	}
	path := base + "/ai-assistance/" + p.ID + "/explain"
	if r := request(mux, "POST", path, `{"confirmation":"yes","digest":"`+p.Digest+`"}`, token); r.Code != 400 || calls.Load() != 0 {
		t.Fatal(r.Code)
	}
	if r := aiSend(mux, cid, token, p); r.Code != 200 || !strings.Contains(r.Body.String(), "operationId") {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := aiSend(mux, cid, token, p); r.Code != 409 || calls.Load() != 1 {
		t.Fatal("replay", r.Code, calls.Load())
	}
	rows, e := app.FindRecordsByFilter("operation_audits", "action='ai.explain'", "", 0, 0)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	row := rows[0]
	if row.GetString("status") != "succeeded" || row.GetString("actor_id") == "" || row.GetString("request_id") == "" || row.GetString("target_id") != "controller:42" {
		t.Fatal(row)
	}
	data, _ := json.Marshal(row)
	if strings.Contains(string(data), "等待资源，具体瓶颈") || strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), `messages`) {
		t.Fatal("prompt/output persisted")
	}
}
func TestAIRequiresBothPermissionsAndExplicitClusterEnable(t *testing.T) {
	for _, permission := range []string{"ai.explain", "jobs.read"} {
		t.Run(permission, func(t *testing.T) {
			app, mux, cid, token, calls, _ := aiFixture(t)
			rows, _ := app.FindRecordsByFilter("role_permissions", "permission.code={:code}", "", 0, 0, map[string]any{"code": permission})
			for _, row := range rows {
				if err := app.Delete(row); err != nil {
					t.Fatal(err)
				}
			}
			r := request(mux, "POST", "/api/v1/clusters/"+cid+"/jobs/42/diagnosis/ai-preview", "{}", token)
			if r.Code != 403 || calls.Load() != 0 {
				t.Fatal(r.Code, r.Body.String())
			}
		})
	}
	app, mux, cid, token, calls, _ := aiFixture(t)
	rows, _ := app.FindRecordsByFilter("role_permissions", "role.code='viewer' && permission.code='ai.explain'", "", 0, 0)
	if len(rows) != 0 {
		t.Fatal("viewer may transmit")
	}
	aiassist.Register(app, nil)
	if r := request(mux, "GET", "/api/v1/clusters/"+cid+"/ai-assistance?source=controller", "", token); r.Code != 200 || !strings.Contains(r.Body.String(), `"enabled":false`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "POST", "/api/v1/clusters/"+cid+"/jobs/42/diagnosis/ai-preview", "{}", token); r.Code != 409 || calls.Load() != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestAIRevisionChangeConsumesPreviewWithoutSend(t *testing.T) {
	app, mux, cid, token, calls, _ := aiFixture(t)
	p := aiPrepare(t, mux, cid, token)
	c, _ := app.FindRecordById("clusters", cid)
	c.Set("revision", c.GetInt("revision")+1)
	if e := app.Save(c); e != nil {
		t.Fatal(e)
	}
	if r := aiSend(mux, cid, token, p); r.Code != 409 || calls.Load() != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := aiSend(mux, cid, token, p); r.Code != 409 {
		t.Fatal(r.Code)
	}
}
func TestAIAuditIntentFailurePreventsOutbound(t *testing.T) {
	app, mux, cid, token, calls, _ := aiFixture(t)
	p := aiPrepare(t, mux, cid, token)
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString("action") == "ai.explain" {
			return errors.New("synthetic audit failure")
		}
		return e.Next()
	})
	if r := aiSend(mux, cid, token, p); r.Code != 503 || calls.Load() != 0 {
		t.Fatal(r.Code, r.Body.String(), calls.Load())
	}
}
func TestAIRevokeAfterOutboundDiscardsAnswer(t *testing.T) {
	app, mux, cid, token, calls, callback := aiFixture(t)
	p := aiPrepare(t, mux, cid, token)
	*callback = func() {
		rows, _ := app.FindRecordsByFilter("role_permissions", "permission.code='jobs.read'", "", 0, 0)
		for _, r := range rows {
			if e := app.Delete(r); e != nil {
				t.Error(e)
			}
		}
	}
	r := aiSend(mux, cid, token, p)
	if r.Code != 503 || calls.Load() != 1 || strings.Contains(r.Body.String(), aiResponse) || !strings.Contains(r.Body.String(), "已丢弃") {
		t.Fatal(r.Code, r.Body.String())
	}
	row, _ := app.FindFirstRecordByData("operation_audits", "action", "ai.explain")
	if row.GetString("status") != "unknown" {
		t.Fatal(row)
	}
}
func TestAIFinalAuditFailureDiscardsAnswerAndNeverResends(t *testing.T) {
	app, mux, cid, token, calls, _ := aiFixture(t)
	p := aiPrepare(t, mux, cid, token)
	app.OnRecordUpdate("operation_audits").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString("action") == "ai.explain" {
			return errors.New("synthetic finish failure")
		}
		return e.Next()
	})
	r := aiSend(mux, cid, token, p)
	if r.Code != 503 || calls.Load() != 1 || strings.Contains(r.Body.String(), aiResponse) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = aiSend(mux, cid, token, p); r.Code != 409 || calls.Load() != 1 {
		t.Fatal(r.Code)
	}
	row, _ := app.FindFirstRecordByData("operation_audits", "action", "ai.explain")
	if row.GetString("status") != "processing" {
		t.Fatal(row)
	}
}

func TestAIHistoryPreviewKeepsExactRecordAndSubmissionWindow(t *testing.T) {
	app, mux, cid, token, calls, _ := aiFixture(t)
	reg := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ping/") {
			fmt.Fprint(w, strings.Replace(fixturePing, `"release":"25.11.4"`, `"release":"25.11.4","cluster":"fixture"`, 1))
			return
		}
		if r.Method != "GET" || r.URL.Path != "/slurmdb/v0.0.43/jobs/" || r.URL.Query().Get("step") != "42" || r.URL.Query().Get("cluster") != "fixture" {
			t.Error("unscoped history source", r.Method, r.URL)
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `{"jobs":[{"job_id":42,"cluster":"fixture","name":"PRIVATE_FIRST","user":"PRIVATE_USER","state":{"current":["COMPLETED"]},"time":{"submission":1700000010,"start":1700000020,"end":1700000080,"elapsed":60}},{"job_id":42,"cluster":"fixture","name":"PRIVATE_OTHER","state":{"current":["OUT_OF_MEMORY"]},"time":{"submission":1700000090,"start":1700000100,"end":1700000120}}],"errors":[],"meta":{"plugin":{"type":"openapi/slurmdbd","data_parser":"data_parser/v0.0.43"},"slurm":{"release":"25.11.8"}}}`)
	})
	clusterconfig.Register(app, reg)
	base := "/api/v1/clusters/" + cid + "/job-history"
	q := "?start=1700000000&end=1700086400&jobId=42"
	page := request(mux, "GET", base+q, "", token)
	if page.Code != 200 {
		t.Fatal(page.Code, page.Body.String())
	}
	var v struct {
		Data struct{ Items []struct{ Key, Name string } }
	}
	json.Unmarshal(page.Body.Bytes(), &v)
	key := ""
	for _, item := range v.Data.Items {
		if item.Name == "PRIVATE_FIRST" {
			key = item.Key
		}
	}
	if key == "" {
		t.Fatal("missing key")
	}
	path := base + "/" + key + "/diagnosis/ai-preview"
	for _, bad := range []string{"", "?jobId=42", "?start=1&end=99999999&jobId=42", "?start=1&end=2&jobId=42&jobId=43", "?cluster=other"} {
		if r := request(mux, "POST", path+bad, "{}", token); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	r := request(mux, "POST", path+q, "{}", token)
	if r.Code != 200 || strings.Contains(r.Body.String(), "PRIVATE") || strings.Contains(r.Body.String(), "OUT_OF_MEMORY") || calls.Load() != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "POST", base+"/"+strings.Repeat("0", 64)+"/diagnosis/ai-preview"+q, "{}", token); r.Code != 404 {
		t.Fatal(r.Code, r.Body.String())
	}
}
