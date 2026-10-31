package api_test

import (
	"encoding/json"
	"fmt"
	"github.com/pocketbase/pocketbase/core"
	"net/http"
	"strings"
	"testing"
)

type filterDTO struct {
	ID, Name, Module, ClusterID   string
	Revision                      int
	Conditions                    map[string]string
	IsDefault, Usable, Compatible bool
}

func newFilter(t *testing.T, mux http.Handler, token, module, cluster, name string, conditions map[string]string, def bool) filterDTO {
	t.Helper()
	r := request(mux, "POST", "/api/v1/saved-filters", payload(t, map[string]any{"module": module, "clusterId": cluster, "name": name, "conditions": conditions, "isDefault": def}), token)
	if r.Code != 200 {
		t.Fatalf("create %d %s", r.Code, r.Body.String())
	}
	var v struct{ Data filterDTO }
	if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v.Data
}
func TestSavedFiltersOwnershipDefaultsRevisionAndNativeLock(t *testing.T) {
	app, mux := setup(t)
	actor := user(t, app, "users")
	a := cluster(t, app, "filter-a")
	b := cluster(t, app, "filter-b")
	grant(t, app, actor.Id, "viewer", a.Id)
	grant(t, app, actor.Id, "viewer", b.Id)
	token := login(t, mux)
	first := newFilter(t, mux, token, "jobs", a.Id, "Pending", map[string]string{"state": "PENDING", "user": "alice"}, true)
	if first.Revision != 1 || !first.Usable || !first.Compatible {
		t.Fatal(first)
	}
	second := newFilter(t, mux, token, "jobs", a.Id, "Running", map[string]string{"state": "RUNNING"}, true)
	other := newFilter(t, mux, token, "jobs", b.Id, "Pending", map[string]string{}, true)
	list := request(mux, "GET", "/api/v1/saved-filters?module=jobs&clusterId="+a.Id, "", token)
	var rows struct{ Data struct{ Items []filterDTO } }
	_ = json.Unmarshal(list.Body.Bytes(), &rows)
	if list.Code != 200 || len(rows.Data.Items) != 2 || strings.Contains(list.Body.String(), other.ID) {
		t.Fatal(list.Code, list.Body.String())
	}
	old, _ := app.FindRecordById("saved_filters", first.ID)
	if old.GetBool("is_default") || old.GetInt("revision") != 2 {
		t.Fatal("default transition not atomic/revisioned")
	}
	path := "/api/v1/saved-filters/" + first.ID
	update := map[string]any{"name": "Pending", "conditions": map[string]string{"q": "new"}, "isDefault": false, "revision": 1}
	if r := request(mux, "PATCH", path, payload(t, update), token); r.Code != 409 {
		t.Fatal("stale revision accepted", r.Code)
	}
	update["revision"] = 2
	r := request(mux, "PATCH", path, payload(t, update), token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "DELETE", path, `{"revision":2}`, token); r.Code != 409 {
		t.Fatal("stale delete accepted")
	}
	if r := request(mux, "DELETE", path, `{"revision":3}`, token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	// Platform Super Admin still cannot manage another user's preferences.
	otherActor := core.NewRecord(actor.Collection())
	otherActor.SetEmail("other-filter@example.test")
	otherActor.SetPassword("other-password-123")
	otherActor.Set("name", "Other")
	otherActor.Set("active", true)
	otherActor.Set("auth_source", "local")
	if e := app.Save(otherActor); e != nil {
		t.Fatal(e)
	}
	grant(t, app, otherActor.Id, "super_admin", "")
	otherToken, e := otherActor.NewAuthToken()
	if e != nil {
		t.Fatal(e)
	}
	if r := request(mux, "GET", "/api/v1/saved-filters?module=jobs&clusterId="+a.Id, "", otherToken); r.Code != 200 || strings.Contains(r.Body.String(), second.ID) {
		t.Fatal("cross-user list", r.Body.String())
	}
	if r := request(mux, "PATCH", "/api/v1/saved-filters/"+second.ID, payload(t, update), otherToken); r.Code != 404 {
		t.Fatal("cross-user update", r.Code)
	}
	if r := request(mux, "DELETE", "/api/v1/saved-filters/"+second.ID, `{"revision":1}`, otherToken); r.Code != 404 {
		t.Fatal("cross-user delete", r.Code)
	}
	for _, method := range []string{"GET", "POST"} {
		if r := request(mux, method, "/api/collections/saved_filters/records", `{}`, token); r.Code != 403 {
			t.Fatal("native preferences API exposed", method, r.Code)
		}
	}
	if r := request(mux, "GET", "/api/v1/saved-filters?module=jobs&clusterId="+a.Id, "", ""); r.Code != 401 {
		t.Fatal("anonymous preference read")
	}
}
func TestSavedFiltersRejectTransientFieldsBoundCountsAndRevocation(t *testing.T) {
	app, mux := setup(t)
	actor := user(t, app, "users")
	c := cluster(t, app, "filter-limit")
	binding := grant(t, app, actor.Id, "viewer", c.Id)
	token := login(t, mux)
	for _, field := range []string{"owner", "cluster", "page", "job", "node", "operation", "operations", "batch", "proof", "confirmation", "reason", "endpoint", "signal"} {
		body := map[string]any{"module": "jobs", "clusterId": c.Id, "name": "bad", "conditions": map[string]string{field: "secret-or-operation"}, "isDefault": false}
		if r := request(mux, "POST", "/api/v1/saved-filters", payload(t, body), token); r.Code != 400 {
			t.Fatal(field, r.Code, r.Body.String())
		}
	}
	for i := 0; i < 10; i++ {
		newFilter(t, mux, token, "nodes", c.Id, fmt.Sprintf("view-%02d", i), map[string]string{}, false)
	}
	body := map[string]any{"module": "nodes", "clusterId": c.Id, "name": "overflow", "conditions": map[string]string{}, "isDefault": false}
	if r := request(mux, "POST", "/api/v1/saved-filters", payload(t, body), token); r.Code != 409 {
		t.Fatal("unbounded preferences", r.Code, r.Body.String())
	}
	if r := request(mux, "GET", "/api/v1/saved-filters?module=nodes&clusterId="+c.Id+"&owner=other", "", token); r.Code != 400 {
		t.Fatal("owner selector allowed")
	}
	own := newFilter(t, mux, token, "jobs", c.Id, "Mixed Case", map[string]string{}, false)
	body["module"] = "jobs"
	body["name"] = "mixed case"
	if r := request(mux, "POST", "/api/v1/saved-filters", payload(t, body), token); r.Code != 409 {
		t.Fatal("case-insensitive duplicate accepted")
	}
	if err := app.Delete(binding); err != nil {
		t.Fatal(err)
	}
	if r := request(mux, "GET", "/api/v1/saved-filters?module=jobs&clusterId="+c.Id, "", token); r.Code != 403 {
		t.Fatal("revoked preference scope", r.Code)
	}
	// Deleting one's own condition data grants no resource access and remains possible.
	if r := request(mux, "DELETE", "/api/v1/saved-filters/"+own.ID, `{"revision":1}`, token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestSavedFiltersIncompatibleDataCannotBeAppliedAndIsDeletable(t *testing.T) {
	app, mux := setup(t)
	actor := user(t, app, "users")
	c := cluster(t, app, "filter-compatible")
	grant(t, app, actor.Id, "viewer", c.Id)
	token := login(t, mux)
	saved := newFilter(t, mux, token, "jobs", c.Id, "Legacy", map[string]string{}, true)
	row, _ := app.FindRecordById("saved_filters", saved.ID)
	row.Set("conditions", map[string]string{"operation": "never-restore"})
	if err := app.Save(row); err != nil {
		t.Fatal(err)
	}
	r := request(mux, "GET", "/api/v1/saved-filters?module=jobs&clusterId="+c.Id, "", token)
	if r.Code != 200 || strings.Contains(r.Body.String(), "never-restore") || !strings.Contains(r.Body.String(), `"compatible":false`) || !strings.Contains(r.Body.String(), `"usable":false`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "DELETE", "/api/v1/saved-filters/"+saved.ID, `{"revision":1}`, token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
}
