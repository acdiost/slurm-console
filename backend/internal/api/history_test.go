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

func TestHistoryScopeDetailCacheAndRevocation(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	var revoke atomic.Bool
	handler := func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ping/") {
			fmt.Fprint(w, strings.Replace(fixturePing, `"release":"25.11.4"`, `"release":"25.11.4","cluster":"fixture"`, 1))
			return
		}
		if r.URL.Path != "/slurmdb/v0.0.43/jobs/" {
			t.Error("unexpected request", r.URL)
			w.WriteHeader(404)
			return
		}
		calls.Add(1)
		if r.URL.Query().Get("cluster") != "fixture" || r.URL.Query().Get("show_duplicates") != "true" || r.URL.Query().Has("show_batch_script") {
			t.Error("unscoped source", r.URL)
		}
		if revoke.Load() {
			bindings, _ := app.FindRecordsByFilter("role_permissions", "permission.code='job_history.read'", "", 0, 0)
			for _, b := range bindings {
				if err := app.Delete(b); err != nil {
					t.Error(err)
				}
			}
		}
		fmt.Fprint(w, `{"jobs":[{"job_id":42,"cluster":"fixture","name":"archive","user":"alice","account":"research","state":{"current":["COMPLETED"]},"time":{"submission":1700000010,"start":1700000020,"end":1700000030,"elapsed":10},"script":"PRIVATE","steps":[{"step":{"id":"42.batch","name":"batch"},"state":["COMPLETED"]}]},{"job_id":42,"cluster":"fixture","name":"archive retry","state":{"current":["CANCELLED"]},"time":{"submission":1700000010,"start":4294967294,"end":1700000030,"elapsed":0}}],"errors":[],"meta":{"plugin":{"type":"openapi/slurmdbd","data_parser":"data_parser/v0.0.43"},"slurm":{"release":"25.11.8"}}}`)
	}
	registry := connectionRegistry(t, handler)
	manager := clusterconfig.Register(app, registry)
	cfg, err := manager.Save("", validConnection())
	if err != nil {
		t.Fatal(err)
	}
	actor := user(t, app, "users")
	grant(t, app, actor.Id, "viewer", cfg.ID)
	token := login(t, mux)
	bindings, _ := app.FindRecordsByFilter("role_permissions", "permission.code='jobs.read'", "", 0, 0)
	for _, binding := range bindings {
		if err := app.Delete(binding); err != nil {
			t.Fatal(err)
		}
	}
	base := "/api/v1/clusters/" + cfg.ID + "/job-history"
	query := "?start=1700000000&end=1700086400"
	res := request(mux, "GET", base+query+"&perPage=1", "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"totalItems":2`) || strings.Contains(res.Body.String(), "PRIVATE") || strings.Contains(res.Body.String(), `"id":"42.batch"`) {
		t.Fatal(res.Code, res.Body.String())
	}
	var body struct {
		Data struct{ Items []struct{ Key string } }
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	key := body.Data.Items[0].Key
	res = request(mux, "GET", base+"/"+key+query, "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"cached":true`) {
		t.Fatal(res.Code, res.Body.String())
	}
	res = request(mux, "GET", base+query+"&sort=startTime&order=desc", "", token)
	if res.Code != 200 || calls.Load() != 1 {
		t.Fatal("cache condition not canonical", res.Code, calls.Load())
	}
	res = request(mux, "GET", base+query+"&q=archive%20retry", "", token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"totalItems":1`) {
		t.Fatal(res.Code, res.Body.String())
	}
	before := calls.Load()
	for _, q := range []string{"?start=0&end=1", "?start=1700000000", "?start=1700000000&end=1800000000", "?cluster=other", "?user=alice,bob", "?state=ALL", "?sort=script", "?start=1&start=2&end=3", "?show_batch_script=true"} {
		if r := request(mux, "GET", base+q, "", token); r.Code != 400 {
			t.Fatal(q, r.Code, r.Body.String())
		}
	}
	if calls.Load() != before {
		t.Fatal("bad input reached source")
	}
	other := cluster(t, app, "other-history")
	if r := request(mux, "GET", "/api/v1/clusters/"+other.Id+"/job-history"+query, "", token); r.Code != 403 {
		t.Fatal("cross cluster access")
	}
	if r := request(mux, "GET", base+query, "", ""); r.Code != 401 {
		t.Fatal("anonymous access")
	}
	// Force a new query while permission is revoked upstream.
	services.SlurmFor(app).Invalidate(cfg.ID)
	revoke.Store(true)
	res = request(mux, "GET", base+query, "", token)
	if res.Code != 403 || strings.Contains(res.Body.String(), "archive") {
		t.Fatal("in-flight revocation", res.Code, res.Body.String())
	}
	res = request(mux, "GET", base+query, "", token)
	if res.Code != 403 {
		t.Fatal("cached data bypassed revocation")
	}
}
