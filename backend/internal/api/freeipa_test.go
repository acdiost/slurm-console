package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/freeipa"
	"slurm-console/backend/internal/rbac"
)

type ipaFixture struct {
	u          freeipa.User
	g          freeipa.Group
	failure    error
	writes     int
	writeError error
	revoke     func()
}

func newIPA() *ipaFixture {
	return &ipaFixture{u: freeipa.User{ID: "immutable-user", Username: "alice", FirstName: "Alice", LastName: "Example", Enabled: true, Groups: []string{"research"}, Version: strings.Repeat("a", 64)}, g: freeipa.Group{ID: "immutable-group", Name: "research", Members: []string{"alice"}, Version: strings.Repeat("b", 64)}}
}
func (p *ipaFixture) Info() freeipa.Info {
	return freeipa.Info{Configured: true, Domain: "example.test", Realm: "EXAMPLE.TEST"}
}
func (p *ipaFixture) User(context.Context, string) (freeipa.User, error)   { return p.u, p.failure }
func (p *ipaFixture) Group(context.Context, string) (freeipa.Group, error) { return p.g, p.failure }
func (p *ipaFixture) Users(context.Context, string) ([]freeipa.User, error) {
	if p.revoke != nil {
		p.revoke()
	}
	return []freeipa.User{p.u}, p.failure
}
func (p *ipaFixture) Groups(context.Context, string) ([]freeipa.Group, error) {
	return []freeipa.Group{p.g}, p.failure
}
func (p *ipaFixture) Authenticate(_ context.Context, name, id, password string) error {
	if p.failure != nil {
		return p.failure
	}
	if id != p.u.ID || name != p.u.Username || password != "directory-password" || !p.u.Enabled || p.u.PasswordExpired {
		return freeipa.ErrDenied
	}
	return nil
}
func (p *ipaFixture) Change(context.Context, freeipa.Change) error { p.writes++; return p.writeError }
func (p *ipaFixture) ProtectedUser(name string) bool               { return name == "admin" || name == "service" }
func ipaAccount(t *testing.T, app core.App) *core.Record {
	r := user(t, app, "users")
	r.Set("auth_source", "freeipa")
	r.Set("username", "alice")
	r.Set("freeipa_entry_id", "immutable-user")
	if e := app.Save(r); e != nil {
		t.Fatal(e)
	}
	return r
}
func ipaLogin(t *testing.T, mux http.Handler) string {
	t.Helper()
	r := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"alice","password":"directory-password"}`, "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var b struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &b)
	return b.Token
}
func TestFreeIPANativeAuthenticationRevokesDisabledRecreatedAndExpiredIdentities(t *testing.T) {
	app, mux := setup(t)
	p := newIPA()
	auth.RegisterFreeIPA(app, p)
	r := ipaAccount(t, app)
	token := ipaLogin(t, mux)
	res := request(mux, "GET", "/api/v1/auth/me", "", token)
	if res.Code != 200 || strings.Contains(res.Body.String(), p.u.ID) {
		t.Fatal(res.Code, res.Body.String())
	}
	for _, change := range []func(){func() { p.u.Enabled = false }, func() { p.u.ID = "replacement" }, func() { p.u.PasswordExpired = true }} {
		original := p.u
		change()
		for _, path := range []string{"/api/v1/auth/me", "/api/collections/users/auth-refresh"} {
			method := "GET"
			if strings.HasSuffix(path, "auth-refresh") {
				method = "POST"
			}
			if res := request(mux, method, path, "{}", token); res.Code != 401 {
				t.Fatal("identity not revoked", path, res.Code)
			}
		}
		p.u = original
	}
	p.failure = freeipa.ErrUnavailable
	if res := request(mux, "GET", "/api/v1/auth/me", "", token); res.Code != 503 {
		t.Fatal(res.Code)
	}
	r.Set("auth_source", "local")
	if e := app.Save(r); e != nil {
		t.Fatal(e)
	}
	login(t, mux)
	p.failure = nil
	r.Set("auth_source", "freeipa")
	if e := app.Save(r); e != nil {
		t.Fatal(e)
	}
	if res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"alice","password":"`+fixturePassword+`"}`, ""); res.Code != 400 {
		t.Fatal("FreeIPA fell back to platform password")
	}
}
func TestFreeIPAGroupMappingsAreLiveScopedAndBindImmutableGroups(t *testing.T) {
	app, mux := setup(t)
	p := newIPA()
	auth.RegisterFreeIPA(app, p)
	r := ipaAccount(t, app)
	c := cluster(t, app, "ipa-cluster")
	collection, e := app.FindCollectionByNameOrId("directory_role_mappings")
	if e != nil {
		t.Fatal(e)
	}
	role, e := app.FindFirstRecordByData("roles", "code", "viewer")
	if e != nil {
		t.Fatal(e)
	}
	m := core.NewRecord(collection)
	m.Set("group_name", "research")
	m.Set("group_id", p.g.ID)
	m.Set("role", role.Id)
	m.Set("cluster", c.Id)
	if e = app.Save(m); e != nil {
		t.Fatal(e)
	}
	token := ipaLogin(t, mux)
	res := request(mux, "GET", "/api/v1/clusters/"+c.Id, "", token)
	if res.Code != 200 {
		t.Fatal("mapping not applied", res.Code, res.Body.String())
	}
	for _, change := range []func(){func() { p.u.Groups = []string{} }, func() { p.g.ID = "recreated-group" }} {
		originalU, originalG := p.u, p.g
		change()
		a, e := rbac.Resolve(app, r.Id)
		if e != nil || a.Allows("clusters.read", c.Id) {
			t.Fatal("stale group granted access", e)
		}
		p.u, p.g = originalU, originalG
	}
	p.failure = freeipa.ErrUnavailable
	if _, e = rbac.Resolve(app, r.Id); e == nil {
		t.Fatal("directory outage retained group authorization")
	}
}
func directoryAdmin(t *testing.T) (core.App, http.Handler, string, *ipaFixture) {
	app, mux := setup(t)
	r := user(t, app, "users")
	grant(t, app, r.Id, "super_admin", "")
	p := newIPA()
	auth.RegisterFreeIPA(app, p)
	return app, mux, login(t, mux), p
}
func TestFreeIPADirectoryChangesPersistIntentAndNeverReplayUnknownWrites(t *testing.T) {
	app, mux, token, p := directoryAdmin(t)
	body := `{"action":"user.disable","target":"alice","member":"","firstName":"","lastName":"","email":"","description":"","operationId":"directory-operation-0001","version":"` + p.u.Version + `","confirmation":"alice","reason":"测试停用"}`
	p.writeError = freeipa.ErrUnavailable
	res := request(mux, "POST", "/api/v1/directory/changes", body, token)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"unknown"`) {
		t.Fatal(res.Code, res.Body.String())
	}
	if p.writes != 1 {
		t.Fatal("missing write")
	}
	res = request(mux, "POST", "/api/v1/directory/changes", body, token)
	if res.Code != 200 || p.writes != 1 {
		t.Fatal("unknown write replayed")
	}
	res = request(mux, "POST", "/api/v1/directory/changes", strings.ReplaceAll(body, "directory-operation-0001", "directory-operation-0002"), token)
	if res.Code != 409 || p.writes != 1 {
		t.Fatal("unknown target was retried", res.Code, res.Body.String())
	}
	if res = request(mux, "GET", "/api/v1/directory/operations", "", token); res.Code != 200 || !strings.Contains(res.Body.String(), "directory-operation-0001") {
		t.Fatal("lost persistent operation")
	}
	rows, e := app.FindRecordsByFilter("operation_audits", "target_type='directory'", "", 0, 0)
	if e != nil || len(rows) != 1 || rows[0].GetString("status") != "unknown" {
		t.Fatal("intent not durable", e)
	}
	if res = request(mux, "GET", "/api/collections/directory_role_mappings/records", "", token); res.Code != 403 {
		t.Fatal("native mappings CRUD unlocked")
	}
}
func TestFreeIPADirectoryRejectsStaleVersionsProtectedUsersAndRevokedRead(t *testing.T) {
	app, mux, token, p := directoryAdmin(t)
	body := `{"action":"user.disable","target":"admin","member":"","firstName":"","lastName":"","email":"","description":"","operationId":"directory-operation-0003","version":"` + p.u.Version + `","confirmation":"admin","reason":"测试停用"}`
	if res := request(mux, "POST", "/api/v1/directory/changes", body, token); res.Code != 403 || p.writes != 0 {
		t.Fatal("protected account modified", res.Code)
	}
	body = strings.ReplaceAll(body, "admin", "alice")
	body = strings.ReplaceAll(body, p.u.Version, strings.Repeat("f", 64))
	if res := request(mux, "POST", "/api/v1/directory/changes", body, token); res.Code != 409 || p.writes != 0 {
		t.Fatal("stale identity modified", res.Code)
	}
	p.revoke = func() {
		rows, _ := app.FindRecordsByFilter("user_platform_roles", "", "", 0, 0)
		for _, r := range rows {
			_ = app.Delete(r)
		}
	}
	if res := request(mux, "GET", "/api/v1/directory/users", "", token); res.Code != 403 {
		t.Fatal("revoked read leaked directory", res.Code)
	}
}
func TestFreeIPAMappingCreationAuditsAndPreservesManualRoles(t *testing.T) {
	app, mux, token, p := directoryAdmin(t)
	c := cluster(t, app, "mapping-cluster")
	body := `{"group":"research","roleCode":"viewer","clusterId":"` + c.Id + `","confirmation":"CHANGE ACCESS"}`
	res := request(mux, "POST", "/api/v1/directory/role-mappings", body, token)
	if res.Code != 200 || strings.Contains(res.Body.String(), p.g.ID) {
		t.Fatal(res.Code, res.Body.String())
	}
	var v struct {
		Data struct{ ID, UpdatedAt string }
	}
	_ = json.Unmarshal(res.Body.Bytes(), &v)
	res = request(mux, "DELETE", "/api/v1/directory/role-mappings/"+v.Data.ID, `{"updatedAt":"`+v.Data.UpdatedAt+`","confirmation":"CHANGE ACCESS"}`, token)
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	rows, e := app.FindRecordsByFilter("user_platform_roles", "", "", 0, 0)
	if e != nil || len(rows) != 1 {
		t.Fatal("manual grants changed", e)
	}
	p.failure = errors.New("directory unreachable")
	if res = request(mux, "GET", "/api/v1/directory/users", "", token); res.Code != 503 {
		t.Fatal(res.Code)
	}
}

func TestFreeIPADurableAuditFailurePreventsDirectoryWrite(t *testing.T) {
	app, mux, token, p := directoryAdmin(t)
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error { return errors.New("audit storage unavailable") })
	body := `{"action":"user.disable","target":"alice","member":"","firstName":"","lastName":"","email":"","description":"","operationId":"directory-operation-0004","version":"` + p.u.Version + `","confirmation":"alice","reason":"测试停用"}`
	res := request(mux, "POST", "/api/v1/directory/changes", body, token)
	if res.Code != 503 || p.writes != 0 {
		t.Fatal("write escaped missing durable intent", res.Code, p.writes, res.Body.String())
	}
}

func TestFreeIPAGroupMemberAuditRetainsExactMemberWithoutImmutableIDs(t *testing.T) {
	app, mux, token, p := directoryAdmin(t)
	body := `{"action":"group.remove-member","target":"research","member":"alice","firstName":"","lastName":"","email":"","description":"","operationId":"directory-operation-0005","version":"` + p.g.Version + `","confirmation":"research","reason":"移除测试成员"}`
	res := request(mux, "POST", "/api/v1/directory/changes", body, token)
	if res.Code != 200 || p.writes != 1 {
		t.Fatal(res.Code, res.Body.String())
	}
	rows, e := app.FindRecordsByFilter("operation_audits", "target_type='directory'", "", 0, 0)
	if e != nil || len(rows) != 1 {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(rows[0].GetRaw("changes"))
	if !strings.Contains(string(raw), "alice") || strings.Contains(string(raw), p.u.ID) || strings.Contains(string(raw), p.g.ID) {
		t.Fatal("missing or unsafe membership audit", string(raw))
	}
}
