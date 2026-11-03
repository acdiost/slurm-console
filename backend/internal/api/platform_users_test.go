package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
)

func newPlatformAccount(t *testing.T, mux interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, token, email string) map[string]any {
	t.Helper()
	r := request(mux, "POST", "/api/v1/platform-users", payloadNoFail(map[string]any{"email": email, "name": "普通平台用户", "authSource": "local", "username": "", "password": "temporary-fixture-password-123"}), token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var v struct{ Data map[string]any }
	json.Unmarshal(r.Body.Bytes(), &v)
	return v.Data
}
func roleBody(role, cid string, revision int) string {
	return payloadNoFail(map[string]any{"roleCode": role, "clusterId": cid, "revision": revision, "confirmation": "CHANGE ACCESS"})
}
func TestPlatformUsersCreationRolesPaginationAndSessionRevocation(t *testing.T) {
	app, mux, token := settingsActor(t)
	u := newPlatformAccount(t, mux, token, "new-user@example.test")
	id := u["id"].(string)
	if _, ok := u["password"]; ok {
		t.Fatal("password returned")
	}
	if _, ok := u["tokenKey"]; ok {
		t.Fatal("token secret returned")
	}
	c := cluster(t, app, "role-scope")
	before, _ := app.CountRecords("user_cluster_roles")
	if before != 0 {
		t.Fatal("user implicitly authorized")
	}
	for _, body := range []string{roleBody("super_admin", c.Id, 1), roleBody("viewer", "", 1)} {
		if r := request(mux, "POST", "/api/v1/platform-users/"+id+"/roles", body, token); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	if r := request(mux, "POST", "/api/v1/platform-users/"+id+"/roles", roleBody("viewer", c.Id, 1), token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	target, _ := app.FindRecordById("users", id)
	access, e := rbac.Resolve(app, id)
	if e != nil || !access.Allows("jobs.read", c.Id) || access.Allows("jobs.cancel", c.Id) || access.Allows("ai.explain", c.Id) || access.Allows("platform.settings.manage", "") {
		t.Fatal(access, e)
	}
	if r := request(mux, "POST", "/api/v1/platform-users/"+id+"/roles", roleBody("operator", c.Id, 1), token); r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "GET", "/api/v1/platform-users/"+id+"/roles", "", token); r.Code != 200 || !strings.Contains(r.Body.String(), c.Id) || !strings.Contains(r.Body.String(), "viewer") {
		t.Fatal(r.Code, r.Body.String())
	}
	r := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"new-user@example.test","password":"temporary-fixture-password-123"}`, "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var login struct{ Token string }
	json.Unmarshal(r.Body.Bytes(), &login)
	r = request(mux, "PATCH", "/api/v1/platform-users/"+id, payloadNoFail(map[string]any{"name": "停用用户", "active": false, "revision": 2, "confirmation": "UPDATE USER"}), token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(mux, "GET", "/api/v1/auth/me", "", login.Token); r.Code != 401 {
		t.Fatal("disabled token remained valid", r.Code)
	}
	if r = request(mux, "PATCH", "/api/v1/platform-users/"+id, payloadNoFail(map[string]any{"name": "重新启用", "active": true, "revision": 3, "confirmation": "UPDATE USER"}), token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(mux, "GET", "/api/v1/auth/me", "", login.Token); r.Code != 401 {
		t.Fatal("old token revived", r.Code)
	}
	if r = request(mux, "POST", "/api/v1/platform-users/"+id+"/roles/remove", roleBody("viewer", c.Id, 4), token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	access, _ = rbac.Resolve(app, id)
	if access.Allows("jobs.read", c.Id) {
		t.Fatal("revoked cluster read survived")
	}
	if r = request(mux, "GET", "/api/v1/platform-users?q=new-user&perPage=1&page=1", "", token); r.Code != 200 || !strings.Contains(r.Body.String(), `"totalItems":1`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(mux, "GET", "/api/v1/platform-users?q=%25", "", token); r.Code != 200 || !strings.Contains(r.Body.String(), `"totalItems":0`) {
		t.Fatal("search not literal", r.Code, r.Body.String())
	}
	catalog := request(mux, "GET", "/api/v1/platform-role-catalog", "", token)
	if catalog.Code != 200 || !strings.Contains(catalog.Body.String(), "platform.settings.manage") {
		t.Fatal(catalog.Code, catalog.Body.String())
	}
	audits, _ := app.FindRecordsByFilter("operation_audits", "target_id={:id}", "", 0, 0, map[string]any{"id": id})
	for _, a := range audits {
		data, _ := json.Marshal(a)
		if strings.Contains(string(data), "temporary-fixture-password") || strings.Contains(string(data), "passwordHash") {
			t.Fatal("credential audited")
		}
	}
	if target.GetString("password") == "temporary-fixture-password-123" {
		t.Fatal("plaintext secret persisted")
	}
}
func TestPlatformUserManagementProtectsLastAdminAndRollsBack(t *testing.T) {
	app, mux, token := settingsActor(t)
	actor, _ := app.FindAuthRecordByEmail("users", "fixture@example.com")
	if r := request(mux, "PATCH", "/api/v1/platform-users/"+actor.Id, payloadNoFail(map[string]any{"name": "管理员", "active": false, "revision": 0, "confirmation": "UPDATE USER"}), token); r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "POST", "/api/v1/platform-users/"+actor.Id+"/roles/remove", roleBody("super_admin", "", 0), token); r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	target := newPlatformAccount(t, mux, token, "rollback@example.test")
	id := target["id"].(string)
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString("actor_source") == "platform" {
			return errors.New("synthetic audit failure")
		}
		return e.Next()
	})
	if r := request(mux, "PATCH", "/api/v1/platform-users/"+id, payloadNoFail(map[string]any{"name": "不应保存", "active": false, "revision": 1, "confirmation": "UPDATE USER"}), token); r.Code != 503 {
		t.Fatal(r.Code, r.Body.String())
	}
	r, _ := app.FindRecordById("users", id)
	if !r.GetBool("active") || r.GetInt("management_revision") != 1 || r.GetString("name") == "不应保存" {
		t.Fatal("audit failure did not roll back")
	}
	if r := request(mux, "POST", "/api/v1/platform-users/"+id+"/roles", roleBody("super_admin", "", 1), token); r.Code != 503 {
		t.Fatal(r.Code, r.Body.String())
	}
	a, _ := rbac.Resolve(app, id)
	if a.Allows("platform.settings.manage", "") {
		t.Fatal("unaudited grant committed")
	}
}
func TestPlatformUsersExternalMappingsAndNoNativeOrClusterPrivilege(t *testing.T) {
	app, mux, token := settingsActor(t)
	auth.RegisterPAM(app, &pamFixture{})
	auth.RegisterLDAP(app, &ldapFixture{id: "directory-unique-id"})
	for _, source := range []string{"pam", "ldap"} {
		body := map[string]any{"email": source + "@example.test", "name": "外部账户", "authSource": source, "username": source + "_fixture", "password": ""}
		r := request(mux, "POST", "/api/v1/platform-users", payloadNoFail(body), token)
		if r.Code != 200 || strings.Contains(r.Body.String(), "directory-unique-id") {
			t.Fatal(r.Code, r.Body.String())
		}
		body["email"] = "bad-" + source + "@example.test"
		body["password"] = "external-password-must-not-be-read"
		if r = request(mux, "POST", "/api/v1/platform-users", payloadNoFail(body), token); r.Code != 400 {
			t.Fatal(r.Code)
		}
	}
	if r := request(mux, "POST", "/api/v1/platform-users", `{"email":"root@example.test","name":"bad","authSource":"pam","username":"root","password":""}`, token); r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, path := range []string{"/api/collections/users/records", "/api/collections/roles/records", "/api/collections/user_platform_roles/records"} {
		if r := request(mux, "GET", path, "", token); r.Code != 403 {
			t.Fatal(r.Code, path)
		}
	}
	bindings, _ := app.FindRecordsByFilter("role_permissions", "permission.code='platform.users.manage' || permission.code='platform.roles.manage'", "", 0, 0)
	for _, b := range bindings {
		if e := app.Delete(b); e != nil {
			t.Fatal(e)
		}
	}
	for _, path := range []string{"/api/v1/platform-users", "/api/v1/platform-role-catalog"} {
		if r := request(mux, "GET", path, "", token); r.Code != 403 {
			t.Fatal(r.Code, path)
		}
	}
}
func TestConcurrentAdministratorRevocationsRetainAnActiveAdministrator(t *testing.T) {
	app, mux, token := settingsActor(t)
	a, _ := app.FindAuthRecordByEmail("users", "fixture@example.com")
	b := newPlatformAccount(t, mux, token, "second-admin@example.test")
	id := b["id"].(string)
	grant(t, app, id, "super_admin", "")
	r := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"second-admin@example.test","password":"temporary-fixture-password-123"}`, "")
	var login struct{ Token string }
	json.Unmarshal(r.Body.Bytes(), &login)
	if r.Code != 200 {
		t.Fatal(r.Code)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	for _, item := range [][3]string{{token, id, "1"}, {login.Token, a.Id, "0"}} {
		go func(item [3]string) {
			defer wg.Done()
			rev := 0
			if item[2] == "1" {
				rev = 1
			}
			request(mux, "POST", "/api/v1/platform-users/"+item[1]+"/roles/remove", roleBody("super_admin", "", rev), item[0])
		}(item)
	}
	wg.Wait()
	rows, e := app.FindRecordsByFilter("user_platform_roles", "role.code='super_admin' && user.active=true", "", 0, 0)
	if e != nil || len(rows) < 1 {
		t.Fatal("no active administrator after concurrent revocations", e)
	}
}
