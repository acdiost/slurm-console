package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/confighistory"
)

func TestConfigurationPreviewAndVersionComparisonStayReadOnlyAndScoped(t *testing.T) {
	app, mux := setup(t)
	var calls atomic.Int32
	manager := clusterconfig.Register(app, connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); writeFixture(w, r) }))
	person := user(t, app, "users")
	grant(t, app, person.Id, "super_admin", "")
	token := login(t, mux)
	input := validConnection()
	input.Description = "Before description"
	base := "/api/v1/cluster-configurations"
	r := request(mux, "POST", base+"/preview", payload(t, input), token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"before":null`) || strings.Contains(r.Body.String(), "endpoint") {
		t.Fatal(r.Code, r.Body.String())
	}
	if n, _ := app.CountRecords("clusters", nil); n != 0 {
		t.Fatal("preview saved cluster")
	}
	if n, _ := app.CountRecords("cluster_config_versions", nil); n != 0 {
		t.Fatal("preview saved history")
	}
	r = request(mux, "POST", base, payload(t, input), token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var b struct{ Data clusterconfig.Configuration }
	_ = json.Unmarshal(r.Body.Bytes(), &b)
	id := b.Data.ID
	input = b.Data.Input
	first, _ := app.FindFirstRecordByFilter("cluster_config_versions", "cluster_id={:id}", map[string]any{"id": id})
	if first.GetString("kind") != "change" || first.GetInt("revision") != 1 || first.GetString("actor_id") != person.Id || first.GetString("request_id") != r.Header().Get("X-Request-ID") {
		t.Fatal("missing version provenance", first)
	}
	input.Name = "Updated research"
	input.Description = "After description"
	input.TimeoutSeconds = 8
	r = request(mux, "POST", base+"/"+id+"/preview", payload(t, input), token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "Before description") || !strings.Contains(r.Body.String(), "After description") {
		t.Fatal(r.Code, r.Body.String())
	}
	current, _ := app.FindRecordById("clusters", id)
	if current.GetInt("revision") != 1 || current.GetString("description") != "Before description" {
		t.Fatal("preview mutated")
	}
	if n, _ := app.CountRecords("operation_audits", nil); n != 1 {
		t.Fatal("preview wrote audit")
	}
	r = request(mux, "PATCH", base+"/"+id, payload(t, input), token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	second, _ := app.FindFirstRecordByFilter("cluster_config_versions", "cluster_id={:id} && revision=2", map[string]any{"id": id})
	// Registry changes do not resolve or rewrite historical profile fields.
	manager.Registry.Profiles = map[string]clusterconfig.Profile{}
	q := "?from=" + first.Id + "&to=" + second.Id
	r = request(mux, "GET", base+"/"+id+"/compare"+q, "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"field":"description"`) || !strings.Contains(r.Body.String(), `"changed":true`) || strings.Contains(r.Body.String(), "endpoint") || strings.Contains(r.Body.String(), "lastProbe") {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request(mux, "GET", base+"/"+id+"/versions?perPage=1", "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"totalItems":2`) || !strings.Contains(r.Body.String(), `"currentStatus":"matched"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	// Disable remains possible even when a profile has been removed; preview sends no request.
	r = request(mux, "POST", base+"/"+id+"/disable-preview", `{"revision":2}`, token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"after":"false"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request(mux, "POST", base+"/"+id+"/disable", `{"revision":2}`, token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, suffix := range []string{"/preview", "/disable-preview"} {
		r = request(mux, "POST", base+"/"+id+suffix, func() string {
			if suffix == "/preview" {
				return payload(t, input)
			}
			return `{"revision":2}`
		}(), token)
		if r.Code != 409 && r.Code != 400 {
			t.Fatal("stale preview", r.Code, r.Body.String())
		}
	}
	other := cluster(t, app, "history-other")
	if r := request(mux, "GET", base+"/"+other.Id+"/compare"+q, "", token); r.Code != 404 {
		t.Fatal("cross cluster versions", r.Code, r.Body.String())
	}
	for _, bad := range []string{q + "&endpoint=x", q + "&from=" + first.Id, "?from=bad&to=bad"} {
		if r := request(mux, "GET", base+"/"+id+"/compare"+bad, "", token); r.Code != 400 {
			t.Fatal(bad, r.Code, r.Body.String())
		}
	}
	if r := request(mux, "GET", "/api/collections/cluster_config_versions/records", "", token); r.Code != 403 {
		t.Fatal("native versions open", r.Code)
	}
	// An out-of-band change is reported, never substituted for a saved snapshot.
	current, _ = app.FindRecordById("clusters", id)
	current.Set("description", "outside change")
	if e := app.Save(current); e != nil {
		t.Fatal(e)
	}
	if r := request(mux, "GET", base+"/"+id+"/versions", "", token); !strings.Contains(r.Body.String(), `"currentStatus":"different"`) {
		t.Fatal(r.Body.String())
	}
	if e := app.Delete(current); e != nil {
		t.Fatal(e)
	}
	if r := request(mux, "GET", base+"/"+id+"/versions", "", token); r.Code != 200 || !strings.Contains(r.Body.String(), `"currentStatus":"missing"`) {
		t.Fatal("deleted history", r.Code, r.Body.String())
	}
	if r := request(mux, "GET", base+"/"+id+"/compare"+q, "", token); r.Code != 200 {
		t.Fatal("deleted comparison", r.Code, r.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatal("preview/history contacted Slurm", calls.Load())
	}
}
func TestConfigurationVersionFailureRollsBackConfigurationAndAudit(t *testing.T) {
	app, mux := setup(t)
	clusterconfig.Register(app, connectionRegistry(t, writeFixture))
	p := user(t, app, "users")
	grant(t, app, p.Id, "super_admin", "")
	token := login(t, mux)
	input := validConnection()
	created := request(mux, "POST", "/api/v1/cluster-configurations", payload(t, input), token)
	if created.Code != 200 {
		t.Fatal(created.Code, created.Body.String())
	}
	var saved struct{ Data clusterconfig.Configuration }
	_ = json.Unmarshal(created.Body.Bytes(), &saved)
	app.OnRecordCreate("cluster_config_versions").BindFunc(func(e *core.RecordEvent) error { return errors.New("history unavailable") })
	input = saved.Data.Input
	input.Name = "Should roll back"
	failed := request(mux, "PATCH", "/api/v1/cluster-configurations/"+saved.Data.ID, payload(t, input), token)
	if failed.Code != 500 {
		t.Fatal(failed.Code, failed.Body.String())
	}
	current, _ := app.FindRecordById("clusters", saved.Data.ID)
	if current.GetInt("revision") != 1 || current.GetString("name") != saved.Data.Name {
		t.Fatal("update partially committed")
	}
	input = validConnection()
	input.Slug = "new-rollback"
	failed = request(mux, "POST", "/api/v1/cluster-configurations", payload(t, input), token)
	if failed.Code != 500 {
		t.Fatal(failed.Code, failed.Body.String())
	}
	for _, table := range []string{"clusters", "operation_audits", "cluster_config_versions"} {
		if n, _ := app.CountRecords(table, nil); n != 1 {
			t.Fatal("partial transaction", table, n)
		}
	}
}
func TestConfigurationHistoryBaselineNoopAndAuthorization(t *testing.T) {
	app, mux := setup(t)
	manager := clusterconfig.Register(app, connectionRegistry(t, writeFixture))
	p := user(t, app, "users")
	grant(t, app, p.Id, "super_admin", "")
	token := login(t, mux)
	v, e := manager.Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	e = app.RunInTransaction(func(tx core.App) error {
		r, _ := tx.FindRecordById("clusters", v.ID)
		return confighistory.Capture(tx, r, nil)
	})
	if e != nil {
		t.Fatal(e)
	}
	baseline, _ := app.FindFirstRecordByFilter("cluster_config_versions", "cluster_id={:id}", map[string]any{"id": v.ID})
	if baseline.GetString("kind") != "baseline" || baseline.GetString("actor_id") != "" || baseline.GetString("operation_id") != "" {
		t.Fatal("invented baseline provenance")
	}
	base := "/api/v1/cluster-configurations/" + v.ID
	if r := request(mux, "PATCH", base, payload(t, v.Input), token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	next, _ := app.FindFirstRecordByFilter("cluster_config_versions", "cluster_id={:id} && revision=2", map[string]any{"id": v.ID})
	r := request(mux, "GET", base+"/compare?from="+baseline.Id+"&to="+next.Id, "", token)
	if r.Code != 200 || strings.Contains(r.Body.String(), `"changed":true`) {
		t.Fatal("no-op fields changed", r.Code, r.Body.String())
	}
	// Independent audit permission, even for a configuration manager.
	grants, _ := app.FindRecordsByFilter("role_permissions", "permission.code='platform.audit.read'", "", 0, 0)
	for _, g := range grants {
		if e := app.Delete(g); e != nil {
			t.Fatal(e)
		}
	}
	for _, suffix := range []string{"/versions", "/compare?from=" + baseline.Id + "&to=" + next.Id} {
		if r := request(mux, "GET", base+suffix, "", token); r.Code != 403 || strings.Contains(r.Body.String(), "research") {
			t.Fatal("history without audit", r.Code, r.Body.String())
		}
	}
	input := v.Input
	input.Revision = 2
	if r := request(mux, "POST", base+"/preview", payload(t, input), token); r.Code != 200 {
		t.Fatal("preview incorrectly requires audit", r.Code, r.Body.String())
	}
	// Removing management also denies preview. A fresh request cannot reuse old grants.
	grants, _ = app.FindRecordsByFilter("role_permissions", "permission.code='platform.clusters.manage'", "", 0, 0)
	for _, g := range grants {
		if e := app.Delete(g); e != nil {
			t.Fatal(e)
		}
	}
	if r := request(mux, "POST", base+"/preview", payload(t, input), token); r.Code != 403 {
		t.Fatal(r.Code)
	}
	if r := request(mux, "GET", base+"/versions", "", ""); r.Code != 401 {
		t.Fatal(r.Code)
	}
}
