package gpumetrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func profile(t *testing.T, h http.HandlerFunc) (Profile, func()) {
	t.Helper()
	s := httptest.NewServer(h)
	r, e := NewRegistry(map[string]Profile{"alpha": {Label: "isolated fixture", Endpoint: s.URL, ClusterLabel: "cluster", ClusterValue: "alpha", NodeLabel: "Hostname", UtilizationUnit: "percent"}})
	if e != nil {
		t.Fatal(e)
	}
	return r.Clusters["alpha"], s.Close
}
func series(metric, uuid string, ts int64, v string) map[string]any {
	return map[string]any{"metric": map[string]string{"__name__": metric, "cluster": "alpha", "Hostname": "node1", "UUID": uuid, "gpu": "0", "modelName": "A100"}, "values": []any{[]any{ts, v}}}
}
func output(w http.ResponseWriter, rows []map[string]any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": rows}})
}
func TestSnapshotZeroMissingStaleDuplicateAndMIG(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	rows := []map[string]any{series(metricNames[0], "GPU-one", now.Unix(), "0"), series(metricNames[1], "GPU-one", now.Unix(), "0"), series(metricNames[2], "GPU-one", now.Unix(), "40960"), series(metricNames[4], "GPU-one", now.Unix(), "45"), series(metricNames[5], "GPU-one", now.Unix(), "100"), series(metricNames[0], "GPU-old", now.Unix()-180, "60"), series(metricNames[0], "GPU-dup", now.Unix(), "10"), series(metricNames[0], "GPU-dup", now.Unix(), "20"), series(metricNames[4], "GPU-invalid", now.Unix(), "9223372036854775794"), series(metricNames[0], "MIG-part", now.Unix(), "20")}
	p, close := profile(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if !strings.Contains(q, `cluster="alpha"`) || !strings.HasSuffix(q, "[10m]") {
			t.Error(q)
		}
		output(w, rows)
	})
	defer close()
	s, e := LoadSnapshot(context.Background(), p, now)
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Items) != 4 || s.IgnoredSeries != 1 {
		t.Fatal(s)
	}
	by := map[string]Device{}
	for _, d := range s.Items {
		by[d.UUID] = d
	}
	d := by["GPU-one"]
	if d.Status != "fresh" || d.Utilization.Value == nil || *d.Utilization.Value != 0 || d.MemoryTotal.Value == nil || *d.MemoryTotal.Value != 40960 || d.TotalSource != "used_plus_free" {
		t.Fatal(d)
	}
	if by["GPU-old"].Status != "stale" || by["GPU-old"].Utilization.Value != nil || by["GPU-dup"].Status != "ambiguous" || by["GPU-invalid"].Temperature.Status != "invalid" {
		t.Fatal(by)
	}
}
func TestScopeMismatchAndRedirectRejected(t *testing.T) {
	p, close := profile(t, func(w http.ResponseWriter, r *http.Request) {
		x := series(metricNames[0], "GPU-one", time.Now().Unix(), "10")
		x["metric"].(map[string]string)["cluster"] = "foreign"
		output(w, []map[string]any{x})
	})
	defer close()
	if _, e := LoadSnapshot(context.Background(), p, time.Now()); e == nil {
		t.Fatal("foreign telemetry accepted")
	}
	p2, c2 := profile(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://127.0.0.1:1/private", 302) })
	defer c2()
	if _, e := LoadSnapshot(context.Background(), p2, time.Now()); e == nil {
		t.Fatal("redirect followed")
	}
}
func TestHistoryGapsZeroAndNodeSelector(t *testing.T) {
	p, close := profile(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/query_range" || !strings.Contains(q.Get("query"), `Hostname="exporter-node"`) || !strings.Contains(q.Get("query"), `UUID="GPU-one"`) {
			t.Error(r.URL)
		}
		start, _ := strconv.ParseInt(q.Get("start"), 10, 64)
		a := series(metricNames[0], "GPU-one", start, "0")
		a["metric"].(map[string]string)["Hostname"] = "exporter-node"
		a["values"] = []any{[]any{start, "0"}, []any{start + 120, "50"}, []any{start + 180, "NaN"}}
		output(w, []map[string]any{a})
	})
	defer close()
	p.NodeMap = map[string]string{"exporter-node": "node1"}
	h, e := LoadHistory(context.Background(), p, "node1", "GPU-one", "1h", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	points := h.Series["utilization"]
	if points[0].Value == nil || *points[0].Value != 0 || points[1].Value != nil || points[2].Value == nil || *points[2].Value != 50 || points[3].Value != nil {
		t.Fatal(points[:4])
	}
}
func TestProfileTransportUnitsAndRestrictedFiles(t *testing.T) {
	base := Profile{Label: "metrics", Endpoint: "https://metrics.example.test", ClusterLabel: "cluster", ClusterValue: "alpha", NodeLabel: "Hostname", UtilizationUnit: "percent"}
	for _, change := range []func(*Profile){func(p *Profile) { p.Endpoint = "http://192.168.1.1:9090" }, func(p *Profile) { p.Endpoint = "https://user:password@metrics.example.test" }, func(p *Profile) { p.ClusterLabel = "__name__" }, func(p *Profile) { p.UtilizationUnit = "" }, func(p *Profile) { p.NodeMap = map[string]string{"one": "node1", "two": "node1"} }} {
		p := base
		change(&p)
		if _, e := NewRegistry(map[string]Profile{"alpha": p}); e == nil {
			t.Fatal(p)
		}
	}
	token := filepath.Join(t.TempDir(), "token")
	if e := os.WriteFile(token, []byte("private-token"), 0644); e != nil {
		t.Fatal(e)
	}
	base.TokenFile = token
	if _, e := NewRegistry(map[string]Profile{"alpha": base}); e == nil {
		t.Fatal("public token accepted")
	}
	if e := os.Chmod(token, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := NewRegistry(map[string]Profile{"alpha": base}); e != nil {
		t.Fatal(e)
	}
	p := base
	p.UtilizationUnit = "ratio"
	ts := time.Now().Unix()
	b, _ := json.Marshal([]any{ts, "0.25"})
	var raw []json.RawMessage
	_ = json.Unmarshal(b, &raw)
	_, n := parsePoint(raw, metricNames[0], p)
	if n == nil || *n != 25 {
		t.Fatal(n)
	}
}
