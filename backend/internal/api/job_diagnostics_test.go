package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/services"
	"strings"
	"sync/atomic"
	"testing"
)

func TestJobDiagnosisStrictReadOnlyScopeCacheAndFreshRevocation(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	var callback func()
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/slurm/v0.0.43/job/42" {
			t.Error("unexpected diagnostic source", r.Method, r.URL)
			w.WriteHeader(404)
			return
		}
		if callback != nil {
			x := callback
			callback = nil
			x()
		}
		fields := strings.Replace(cancelJobFields("PENDING", 1001), `"submit_time":`, `"script":"PRIVATE","state_reason":"Resources","submit_time":`, 1)
		fmt.Fprint(w, slurmEnvelope(fields))
	})
	cfg, e := clusterconfig.Register(app, registry).Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	actor := user(t, app, "users")
	grant(t, app, actor.Id, "viewer", cfg.ID)
	token := login(t, mux)
	bindings, _ := app.FindRecordsByFilter("role_permissions", "permission.code='job_history.read' || permission.code='nodes.read' || permission.code='metrics.read'", "", 0, 0)
	for _, b := range bindings {
		if e := app.Delete(b); e != nil {
			t.Fatal(e)
		}
	}
	base := "/api/v1/clusters/" + cfg.ID + "/jobs/42/diagnosis"
	for _, bad := range []string{"?query=secret", "?start=1", "?node=x"} {
		if r := request(mux, "GET", base+bad, "", token); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	if r := request(mux, "GET", strings.Replace(base, "/42/", "/42_0/", 1), "", token); r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatal("invalid source call")
	}
	r := request(mux, "GET", base, "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"code":"pending_reason"`) || strings.Contains(r.Body.String(), "PRIVATE") {
		t.Fatal(r.Code, r.Body.String())
	}
	count := calls.Load()
	r = request(mux, "GET", base, "", token)
	if r.Code != 200 || calls.Load() != count || !strings.Contains(r.Body.String(), `"cached":true`) {
		t.Fatal(r.Code, r.Body.String())
	}
	other := cluster(t, app, "other-diagnostic")
	if r := request(mux, "GET", strings.Replace(base, cfg.ID, other.Id, 1), "", token); r.Code != 403 {
		t.Fatal(r.Code)
	}
	if r := request(mux, "GET", base, "", ""); r.Code != 401 {
		t.Fatal(r.Code)
	}
	before, _ := app.CountRecords("operation_audits")
	services.SlurmFor(app).Invalidate(cfg.ID)
	callback = func() {
		v, _ := app.FindRecordById("clusters", cfg.ID)
		v.Set("revision", v.GetInt("revision")+1)
		if e := app.Save(v); e != nil {
			t.Error(e)
		}
	}
	r = request(mux, "GET", base, "", token)
	if r.Code != 409 || strings.Contains(r.Body.String(), "pending_reason") {
		t.Fatal(r.Code, r.Body.String())
	}
	services.SlurmFor(app).Invalidate(cfg.ID)
	callback = func() {
		rows, _ := app.FindRecordsByFilter("role_permissions", "permission.code='jobs.read'", "", 0, 0)
		for _, v := range rows {
			if e := app.Delete(v); e != nil {
				t.Error(e)
			}
		}
	}
	r = request(mux, "GET", base, "", token)
	if r.Code != 403 || strings.Contains(r.Body.String(), "pending_reason") {
		t.Fatal(r.Code, r.Body.String())
	}
	after, _ := app.CountRecords("operation_audits")
	if before != after {
		t.Fatal("read wrote audit")
	}
}
func TestHistoryDiagnosisRequiresExactAttemptAndIndependentPermission(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	var callback func()
	reg := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ping/") {
			fmt.Fprint(w, strings.Replace(fixturePing, `"release":"25.11.4"`, `"release":"25.11.4","cluster":"fixture"`, 1))
			return
		}
		if r.Method != "GET" || r.URL.Path != "/slurmdb/v0.0.43/jobs/" || r.URL.Query().Get("step") != "42" || r.URL.Query().Get("cluster") != "fixture" {
			t.Error("unscoped history", r.Method, r.URL)
			w.WriteHeader(404)
			return
		}
		calls.Add(1)
		if callback != nil {
			x := callback
			callback = nil
			x()
		}
		fmt.Fprint(w, `{"jobs":[{"job_id":42,"cluster":"fixture","name":"first attempt","state":{"current":["COMPLETED"]},"time":{"submission":1700000010,"start":1700000020,"end":1700000080,"elapsed":60,"suspended":0,"total":{"seconds":12,"microseconds":0}},"exit_code":{"return_code":{"set":true,"number":0}},"derived_exit_code":{"return_code":{"set":true,"number":2}},"tres":{"allocated":[{"id":1,"type":"cpu","count":2}]},"steps":[{"step":{"id":"42.batch","name":"batch"},"state":["FAILED"],"exit_code":{"return_code":{"set":true,"number":2}}}]},{"job_id":42,"cluster":"fixture","name":"other attempt","state":{"current":["OUT_OF_MEMORY"]},"time":{"submission":1700000090,"start":1700000100,"end":1700000120}}],"errors":[],"meta":{"plugin":{"type":"openapi/slurmdbd","data_parser":"data_parser/v0.0.43"},"slurm":{"release":"25.11.8"}}}`)
	})
	cfg, e := clusterconfig.Register(app, reg).Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	actor := user(t, app, "users")
	grant(t, app, actor.Id, "viewer", cfg.ID)
	token := login(t, mux)
	bindings, _ := app.FindRecordsByFilter("role_permissions", "permission.code='jobs.read' || permission.code='nodes.read' || permission.code='metrics.read'", "", 0, 0)
	for _, b := range bindings {
		if e := app.Delete(b); e != nil {
			t.Fatal(e)
		}
	}
	base := "/api/v1/clusters/" + cfg.ID + "/job-history"
	query := "?start=1700000000&end=1700086400&jobId=42"
	r := request(mux, "GET", base+query, "", token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var page struct {
		Data struct{ Items []struct{ Key, Name string } }
	}
	_ = json.Unmarshal(r.Body.Bytes(), &page)
	key := ""
	for _, j := range page.Data.Items {
		if j.Name == "first attempt" {
			key = j.Key
		}
	}
	if key == "" {
		t.Fatal("key missing")
	}
	path := base + "/" + key + "/diagnosis"
	before := calls.Load()
	for _, bad := range []string{"", "?jobId=42", "?start=1&end=2", "?jobId=42&start=1&end=9999999", "?jobId=42&jobId=43&start=1&end=2", "?cluster=foreign"} {
		if x := request(mux, "GET", path+bad, "", token); x.Code != 400 {
			t.Fatal(x.Code, x.Body.String())
		}
	}
	if calls.Load() != before {
		t.Fatal("bad conditions reached source")
	}
	r = request(mux, "GET", path+query, "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"code":"derived_exit"`) || !strings.Contains(r.Body.String(), `"code":"step_failures"`) || strings.Contains(r.Body.String(), "other attempt") || strings.Contains(r.Body.String(), `"code":"out_of_memory"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if calls.Load() != before || !strings.Contains(r.Body.String(), `"cached":true`) {
		t.Fatal("cache not reused")
	}
	if x := request(mux, "GET", base+"/"+strings.Repeat("0", 64)+"/diagnosis"+query, "", token); x.Code != 404 {
		t.Fatal(x.Code, x.Body.String())
	}
	services.SlurmFor(app).Invalidate(cfg.ID)
	callback = func() {
		actor.Set("active", false)
		if e := app.Save(actor); e != nil {
			t.Error(e)
		}
	}
	r = request(mux, "GET", path+query, "", token)
	if r.Code != 403 || strings.Contains(r.Body.String(), "first attempt") {
		t.Fatal(r.Code, r.Body.String())
	}
}
