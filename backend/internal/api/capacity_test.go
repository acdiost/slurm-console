package api_test

import (
	"context"
	"encoding/json"
	"github.com/pocketbase/pocketbase/core"
	"net/http"
	"slurm-console/backend/internal/capacity"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/overview"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/models"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func seedCapacity(t *testing.T, app core.App, cid, key string, bucket int64) {
	t.Helper()
	col, e := app.FindCollectionByNameOrId("capacity_samples")
	if e != nil {
		t.Fatal(e)
	}
	r := core.NewRecord(col)
	r.Set("cluster", cid)
	r.Set("source_key", key)
	r.Set("bucket", bucket)
	r.Set("observed_at", time.Unix(bucket+20, 0))
	v, e := overview.NodeSummary([]models.Node{{Name: "sample", States: []string{"MIXED"}, CPUs: 4, EffectiveCPUs: 4, AllocatedCPUs: 2, MemoryMiB: 1000}})
	if e != nil {
		t.Fatal(e)
	}
	r.Set("data", capacity.FromSummary(v))
	if e := app.Save(r); e != nil {
		t.Fatal(e)
	}
}
func TestCapacityReadCollectionBoundsAndSourceIsolation(t *testing.T) {
	app, mux := setup(t)
	var reads, parts atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if strings.Contains(r.URL.Path, "partitions") {
			parts.Add(1)
		}
		writeFixture(w, r)
	})
	m := clusterconfig.Register(app, registry)
	a, e := m.Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	second := validConnection()
	second.Slug = "capacity-b"
	second.Name = "Other"
	b, e := m.Save("", second)
	if e != nil {
		t.Fatal(e)
	}
	u := user(t, app, "users")
	binding := grant(t, app, u.Id, "viewer", a.ID)
	token := login(t, mux)
	base := "/api/v1/clusters/" + a.ID + "/capacity"
	out := request(mux, "GET", base, "", token)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"cpuIdleBuckets"`) || !strings.Contains(out.Body.String(), `"totalItems":2`) {
		t.Fatal(out.Code, out.Body.String())
	}
	count := reads.Load()
	if out := request(mux, "GET", "/api/v1/clusters/"+b.ID+"/capacity", "", token); out.Code != 403 {
		t.Fatal("cross cluster", out.Code)
	}
	if reads.Load() != count {
		t.Fatal("unauthorized source accessed")
	}
	for _, q := range []string{"?q=x&q=y", "?sort=nodes%7CcpuTotal", "?range=24h", "?q=bad%0Aquery"} {
		if out := request(mux, "GET", base+q, "", token); out.Code != 400 {
			t.Fatal("invalid query", q, out.Code)
		}
	}
	s := capacity.For(app)
	if e := s.Capture(context.Background(), a.ID); e != nil {
		t.Fatal(e)
	}
	if e := s.Capture(context.Background(), a.ID); e != nil {
		t.Fatal(e)
	}
	n, _ := app.CountRecords("capacity_samples")
	if n != 1 {
		t.Fatal("duplicate five-minute sample", n)
	}
	key, _, e := capacity.Key(app, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	seedCapacity(t, app, a.ID, key, time.Now().Unix()/capacity.Step*capacity.Step-2*capacity.Step)
	count = reads.Load()
	out = request(mux, "GET", base+"/history?range=1h", "", token)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"availableSamples":1`) || !strings.Contains(out.Body.String(), `"expectedSamples":12`) {
		t.Fatal(out.Code, out.Body.String())
	}
	if reads.Load() != count {
		t.Fatal("history read called Slurm")
	}
	for _, method := range []string{"GET", "POST"} {
		if out := request(mux, method, "/api/collections/capacity_samples/records", "{}", token); out.Code != 403 {
			t.Fatal("native history open", out.Code)
		}
	}
	for _, q := range []string{"?range=30d", "?range=1h&range=7d", "?endpoint=secret"} {
		if out := request(mux, "GET", base+"/history"+q, "", token); out.Code != 400 {
			t.Fatal(q, out.Code)
		}
	}
	// Changing an administrator-owned profile without editing the cluster record cannot mix history.
	p := m.Registry.Profiles["local-test"]
	p.Label = "changed profile"
	m.Registry.Profiles["local-test"] = p
	newKey, _, e := capacity.Key(app, a.ID)
	if e != nil || newKey == key {
		t.Fatal("profile not bound", e)
	}
	out = request(mux, "GET", base+"/history?range=1h", "", token)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"availableSamples":0`) {
		t.Fatal("old profile history mixed", out.Code, out.Body.String())
	}
	if e := app.Delete(binding); e != nil {
		t.Fatal(e)
	}
	count = reads.Load()
	for _, path := range []string{base, base + "/history"} {
		if out := request(mux, "GET", path, "", token); out.Code != 403 {
			t.Fatal("revoked", out.Code)
		}
	}
	if reads.Load() != count {
		t.Fatal("source accessed after revocation")
	}
	if out := request(mux, "GET", base, "", ""); out.Code != 401 {
		t.Fatal("anonymous", out.Code)
	}
}
func TestCapacityOptionalPartitionPermissionsAndSeparateCapacityRead(t *testing.T) {
	app, mux := setup(t)
	var calls, partitionCalls atomic.Int32
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.Contains(r.URL.Path, "partitions") {
			partitionCalls.Add(1)
		}
		writeFixture(w, r)
	})
	m := clusterconfig.Register(app, registry)
	c, e := m.Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	u := user(t, app, "users")
	roles, _ := app.FindCollectionByNameOrId("roles")
	role := core.NewRecord(roles)
	role.Set("code", "capacity_custom")
	role.Set("name", "Capacity custom")
	role.Set("scope", "cluster")
	if e := app.Save(role); e != nil {
		t.Fatal(e)
	}
	bindings, _ := app.FindCollectionByNameOrId("role_permissions")
	for _, code := range []string{"nodes.read", "capacity.read"} {
		p, _ := app.FindFirstRecordByData("permissions", "code", code)
		r := core.NewRecord(bindings)
		r.Set("role", role.Id)
		r.Set("permission", p.Id)
		if e := app.Save(r); e != nil {
			t.Fatal(e)
		}
	}
	grant(t, app, u.Id, "capacity_custom", c.ID)
	token := login(t, mux)
	base := "/api/v1/clusters/" + c.ID + "/capacity"
	out := request(mux, "GET", base, "", token)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"status":"forbidden"`) || strings.Contains(out.Body.String(), `"states":["UP"]`) {
		t.Fatal(out.Code, out.Body.String())
	}
	if partitionCalls.Load() != 0 {
		t.Fatal("unprivileged partition query")
	}
	p, _ := app.FindFirstRecordByData("permissions", "code", "capacity.read")
	b, _ := app.FindFirstRecordByData("role_permissions", "permission", p.Id)
	rows, _ := app.FindRecordsByFilter("role_permissions", "role='"+role.Id+"'", "", 0, 0)
	for _, v := range rows {
		if v.GetString("permission") == p.Id {
			b = v
			break
		}
	}
	if e := app.Delete(b); e != nil {
		t.Fatal(e)
	}
	count := calls.Load()
	if out := request(mux, "GET", base, "", token); out.Code != 403 {
		t.Fatal("node read implied historical capacity", out.Code)
	}
	if count != calls.Load() {
		t.Fatal("unauthorized query")
	}
}
func TestCapacityRevocationAndConfigurationChangesDiscardInflightData(t *testing.T) {
	app, mux := setup(t)
	u := user(t, app, "users")
	var callback func()
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if callback != nil {
			callback()
		}
		writeFixture(w, r)
	})
	m := clusterconfig.Register(app, registry)
	c, e := m.Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	binding := grant(t, app, u.Id, "viewer", c.ID)
	token := login(t, mux)
	base := "/api/v1/clusters/" + c.ID + "/capacity"
	callback = func() {
		if e := app.Delete(binding); e != nil {
			t.Error(e)
		}
	}
	out := request(mux, "GET", base, "", token)
	if out.Code != 403 || strings.Contains(out.Body.String(), `"totals"`) {
		t.Fatal("inflight data leaked", out.Code, out.Body.String())
	}
	grant(t, app, u.Id, "viewer", c.ID)
	services.SlurmFor(app).Invalidate(c.ID)
	callback = func() {
		r, _ := app.FindRecordById("clusters", c.ID)
		r.Set("revision", r.GetInt("revision")+1)
		if e := app.Save(r); e != nil {
			t.Error(e)
		}
	}
	if e := capacity.For(app).Capture(context.Background(), c.ID); e == nil {
		t.Fatal("changed source captured")
	}
	n, _ := app.CountRecords("capacity_samples")
	if n != 0 {
		t.Fatal("stale capture persisted")
	}
}
func TestCapacityHistoryCorruptionFailsClosed(t *testing.T) {
	app, mux := setup(t)
	m := clusterconfig.Register(app, connectionRegistry(t, writeFixture))
	c, e := m.Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	u := user(t, app, "users")
	grant(t, app, u.Id, "viewer", c.ID)
	token := login(t, mux)
	key, _, _ := capacity.Key(app, c.ID)
	seedCapacity(t, app, c.ID, key, time.Now().Unix()/capacity.Step*capacity.Step-capacity.Step)
	rows, _ := app.FindRecordsByFilter("capacity_samples", "", "", 1, 0)
	rows[0].Set("data", map[string]any{"version": 99, "secret": "not-public"})
	if e := app.Save(rows[0]); e != nil {
		t.Fatal(e)
	}
	out := request(mux, "GET", "/api/v1/clusters/"+c.ID+"/capacity/history?range=1h", "", token)
	if out.Code != 503 || strings.Contains(out.Body.String(), "not-public") {
		t.Fatal(out.Code, out.Body.String())
	}
	var raw map[string]any
	_ = json.Unmarshal(out.Body.Bytes(), &raw)
}
