package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/services"
)

func TestUsageReportsWindowScopeCacheAndRevocation(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	var revoke atomic.Bool
	var change atomic.Bool
	handler := func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ping/") {
			fmt.Fprint(w, strings.Replace(fixturePing, `"release":"25.11.4"`, `"release":"25.11.4","cluster":"fixture"`, 1))
			return
		}
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/slurmdb/v0.0.43/jobs/" || r.URL.Query().Get("cluster") != "fixture" || r.URL.Query().Has("usage_time_as_submit_time") || r.URL.Query().Get("skip_steps") != "true" || r.URL.Query().Has("show_batch_script") {
			t.Error("unapproved report request", r.URL)
		}
		if revoke.Load() {
			rows, _ := app.FindRecordsByFilter("role_permissions", "permission.code='reports.read'", "", 0, 0)
			for _, x := range rows {
				if e := app.Delete(x); e != nil {
					t.Error(e)
				}
			}
		}
		if change.Load() {
			rows, _ := app.FindRecordsByFilter("clusters", "slug='research'", "", 1, 0)
			rows[0].Set("revision", 2)
			if e := app.Save(rows[0]); e != nil {
				t.Error(e)
			}
		}
		fmt.Fprint(w, `{"jobs":[{"job_id":42,"cluster":"fixture","user":"alice","account":"research","partition":"normal","state":{"current":["COMPLETED"]},"time":{"submission":1699000000,"start":1699999950,"end":1700000050,"elapsed":100,"suspended":0},"tres":{"allocated":[{"id":1,"type":"cpu","count":4},{"id":2,"type":"mem","count":1024}]},"script":"PRIVATE"}],"errors":[],"meta":{"plugin":{"type":"openapi/slurmdbd","data_parser":"data_parser/v0.0.43"},"slurm":{"release":"25.11.8"}}}`)
	}
	cfg, e := clusterconfig.Register(app, connectionRegistry(t, handler)).Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	actor := user(t, app, "users")
	grant(t, app, actor.Id, "viewer", cfg.ID)
	token := login(t, mux)
	base := "/api/v1/clusters/" + cfg.ID + "/reports/usage"
	q := "?start=1700000000&end=1700086400"
	res := request(mux, "GET", base+q, "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"resourceSeconds":"200"`) || !strings.Contains(res.Body.String(), `"unknownRecords":1`) || strings.Contains(res.Body.String(), "PRIVATE") {
		t.Fatal(res.Code, res.Body.String())
	}
	res = request(mux, "GET", base+q+"&groupBy=account", "", token)
	if res.Code != 200 || calls.Load() != 1 || !strings.Contains(res.Body.String(), `"cached":true`) || !strings.Contains(res.Body.String(), `"key":"research"`) {
		t.Fatal(res.Code, calls.Load(), res.Body.String())
	}
	for _, bad := range []string{"", "?start=1", "?start=1&end=4000000000", q + "&user=alice,bob", q + "&groupBy=uid", q + "&start=2", q + "&cluster=other", q + "&executionWindow=false", q + "&show_batch_script=true"} {
		if r := request(mux, "GET", base+bad, "", token); r.Code != 400 {
			t.Fatal(bad, r.Code, r.Body.String())
		}
	}
	other := cluster(t, app, "other-report")
	if r := request(mux, "GET", "/api/v1/clusters/"+other.Id+"/reports/usage"+q, "", token); r.Code != 403 {
		t.Fatal("cross cluster", r.Code)
	}
	if r := request(mux, "GET", base+q, "", ""); r.Code != 401 {
		t.Fatal("anonymous", r.Code)
	}
	services.SlurmFor(app).Invalidate(cfg.ID)
	change.Store(true)
	if r := request(mux, "GET", base+q, "", token); r.Code != 409 && r.Code != 503 {
		t.Fatal("configuration changed", r.Code, r.Body.String())
	}
	change.Store(false)
	services.SlurmFor(app).Invalidate(cfg.ID)
	revoke.Store(true)
	if r := request(mux, "GET", base+q, "", token); r.Code != 403 || strings.Contains(r.Body.String(), "alice") {
		t.Fatal("inflight revocation", r.Code, r.Body.String())
	}
	if r := request(mux, "GET", base+q, "", token); r.Code != 403 {
		t.Fatal("cached revocation", r.Code)
	}
}
func TestUsageReportsRequireIndependentReportAndHistoryPermissions(t *testing.T) {
	for _, code := range []string{"reports.read", "job_history.read"} {
		t.Run(code, func(t *testing.T) {
			app, mux, cid, token, _ := accountingSetup(t, "viewer")
			rows, _ := app.FindRecordsByFilter("role_permissions", "permission.code={:code}", "", 0, 0, map[string]any{"code": code})
			for _, r := range rows {
				if e := app.Delete(r); e != nil {
					t.Fatal(e)
				}
			}
			r := request(mux, "GET", "/api/v1/clusters/"+cid+"/reports/usage?start=1700000000&end=1700086400", "", token)
			if r.Code != 403 {
				t.Fatal(code, r.Code, r.Body.String())
			}
		})
	}
}
