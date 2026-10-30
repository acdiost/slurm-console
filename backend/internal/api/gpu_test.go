package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slurm-console/backend/internal/gpumetrics"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGPUUnconfiguredAndPermissions(t *testing.T) {
	app, mux, cid, token, _ := accountingSetup(t, "viewer")
	base := "/api/v1/clusters/" + cid + "/metrics/gpu"
	r := request(mux, "GET", base, "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"not_configured"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, query := range []string{"?query=secret", "?range=1y", "?sort=secret"} {
		if r := request(mux, "GET", base+query, "", token); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	bindings, _ := app.FindRecordsByFilter("role_permissions", "permission.code='metrics.read'", "", 0, 0)
	for _, b := range bindings {
		if e := app.Delete(b); e != nil {
			t.Fatal(e)
		}
	}
	if r := request(mux, "GET", base, "", token); r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
}
func TestGPUProfileScopeHistoryCacheAndRevocation(t *testing.T) {
	app, mux, cid, token, f := accountingSetup(t, "viewer")
	calls := atomic.Int32{}
	var onQuery func()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if onQuery != nil {
			x := onQuery
			onQuery = nil
			x()
		}
		q := r.URL.Query().Get("query")
		if !strings.Contains(q, `cluster="isolated"`) {
			t.Error(q)
		}
		ts := time.Now().Unix()
		if r.URL.Path == "/api/v1/query_range" {
			ts = time.Now().Unix()/60*60 - 3600
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": map[string]string{"__name__": "DCGM_FI_DEV_GPU_UTIL", "cluster": "isolated", "Hostname": "node1", "UUID": "GPU-one", "gpu": "0", "modelName": "Test"}, "values": []any{[]any{ts, "0"}}}}}})
	}))
	defer srv.Close()
	registry, e := gpumetrics.NewRegistry(map[string]gpumetrics.Profile{cid: {Label: "isolated", Endpoint: srv.URL, ClusterLabel: "cluster", ClusterValue: "isolated", NodeLabel: "Hostname", UtilizationUnit: "percent"}})
	if e != nil {
		t.Fatal(e)
	}
	gpumetrics.Register(app, registry)
	base := "/api/v1/clusters/" + cid + "/metrics/gpu"
	r := request(mux, "GET", base, "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"value":0`) || strings.Contains(r.Body.String(), "foreign") {
		t.Fatal(r.Code, r.Body.String())
	}
	count := calls.Load()
	r = request(mux, "GET", base, "", token)
	if r.Code != 200 || calls.Load() != count || !strings.Contains(r.Body.String(), `"cached":true`) {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request(mux, "GET", base+"/GPU-one/history?node=node1&range=1h", "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"value":null`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(mux, "GET", base+"/GPU-secret/history?node=node1", "", token); r.Code != 404 {
		t.Fatal(r.Code, r.Body.String())
	}
	// Revoked node access must neither call Slurm nor return allocation data; metrics remains independently readable.
	bindings, _ := app.FindRecordsByFilter("role_permissions", "permission.code='nodes.read'", "", 0, 0)
	for _, b := range bindings {
		if e := app.Delete(b); e != nil {
			t.Fatal(e)
		}
	}
	f.mu.Lock()
	f.onRead = func() { t.Error("Slurm called without nodes.read") }
	f.mu.Unlock()
	r = request(mux, "GET", base, "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"forbidden"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	// A fresh query after role revocation cannot return telemetry already in flight.
	gpumetrics.Register(app, registry)
	onQuery = func() {
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
