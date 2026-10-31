package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slurm-console/backend/internal/gpumetrics"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGPUEfficiencyReadOnlyFilteringCacheAndFreshAccess(t *testing.T) {
	app, mux, cid, token, f := accountingSetup(t, "viewer")
	f.mu.Lock()
	f.onRead = func() { t.Error("efficiency must not query Slurm") }
	f.mu.Unlock()
	calls := atomic.Int32{}
	var callback func()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if callback != nil {
			x := callback
			callback = nil
			x()
		}
		q := r.URL.Query()
		if !strings.Contains(q.Get("query"), `cluster="isolated"`) {
			t.Error(q)
		}
		rows := []any{}
		if strings.Contains(q.Get("query"), "DCGM_FI_DEV_GPU_UTIL{") {
			start, _ := strconv.ParseInt(q.Get("start"), 10, 64)
			vals := []any{}
			for i := 0; i < 60; i++ {
				vals = append(vals, []any{start + int64(i)*60, "0"})
			}
			rows = append(rows, map[string]any{"metric": map[string]string{"__name__": "DCGM_FI_DEV_GPU_UTIL", "cluster": "isolated", "Hostname": "node1", "UUID": "GPU-one"}, "values": vals})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": rows}})
	}))
	defer srv.Close()
	reg, e := gpumetrics.NewRegistry(map[string]gpumetrics.Profile{cid: {Label: "isolated", Endpoint: srv.URL, ClusterLabel: "cluster", ClusterValue: "isolated", NodeLabel: "Hostname", UtilizationUnit: "percent"}})
	if e != nil {
		t.Fatal(e)
	}
	gpumetrics.Register(app, reg)
	base := "/api/v1/clusters/" + cid + "/metrics/gpu/efficiency"
	for _, bad := range []string{"?range=7d", "?query=secret", "?sort=secret", "?range=1h&range=6h", "?status=idle"} {
		if r := request(mux, "GET", base+bad, "", token); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("bad query reached source")
	}
	r := request(mux, "GET", base, "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"low"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	count := calls.Load()
	r = request(mux, "GET", base+"?q=absent&sort=mean&order=desc", "", token)
	if r.Code != 200 || calls.Load() != count || !strings.Contains(r.Body.String(), `"observedDevices":1`) || !strings.Contains(r.Body.String(), `"totalItems":0`) || !strings.Contains(r.Body.String(), `"cached":true`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(mux, "GET", strings.Replace(base, cid, "ungrantedcluster", 1), "", token); r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
	// A configuration change in flight cannot return the old device series.
	gpumetrics.Register(app, reg)
	callback = func() {
		c, _ := app.FindRecordById("clusters", cid)
		c.Set("revision", c.GetInt("revision")+1)
		if e := app.Save(c); e != nil {
			t.Error(e)
		}
	}
	r = request(mux, "GET", base, "", token)
	if r.Code != 503 || strings.Contains(r.Body.String(), "GPU-one") {
		t.Fatal(r.Code, r.Body.String())
	}
	gpumetrics.Register(app, reg)
	callback = func() {
		bindings, _ := app.FindRecordsByFilter("role_permissions", "permission.code='metrics.read'", "", 0, 0)
		for _, b := range bindings {
			if e := app.Delete(b); e != nil {
				t.Error(e)
			}
		}
	}
	r = request(mux, "GET", base, "", token)
	if r.Code != 403 || strings.Contains(r.Body.String(), "GPU-one") {
		t.Fatal(r.Code, r.Body.String())
	}
}
func TestGPUEfficiencyUnconfigured(t *testing.T) {
	_, mux, cid, token, _ := accountingSetup(t, "viewer")
	r := request(mux, "GET", "/api/v1/clusters/"+cid+"/metrics/gpu/efficiency?range=24h", "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"not_configured"`) || !strings.Contains(r.Body.String(), `"meanUtilization":null`) {
		t.Fatal(r.Code, r.Body.String())
	}
}
