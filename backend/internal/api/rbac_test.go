package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"slurm-console/backend/internal/api"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
)

func cluster(t *testing.T, app core.App, name string) *core.Record {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId("clusters")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("name", name)
	record.Set("slug", name)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

func grant(t *testing.T, app core.App, userID, role, clusterID string) *core.Record {
	t.Helper()
	record, err := rbac.Grant(app, userID, role, clusterID)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestClusterIsolationAndImmediateRevocation(t *testing.T) {
	app, mux := setup(t)
	record := user(t, app, "users")
	a, b := cluster(t, app, "alpha"), cluster(t, app, "beta")
	token := login(t, mux)
	res := request(mux, "GET", "/api/v1/clusters", "", token)
	if res.Code != 200 {
		t.Fatalf("empty scope: %d %s", res.Code, res.Body.String())
	}
	var body struct {
		Data struct {
			Items []rbac.Cluster `json:"items"`
			Total int            `json:"totalItems"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.Data.Total != 0 {
		t.Fatalf("ungranted user saw clusters: %s", res.Body.String())
	}
	binding := grant(t, app, record.Id, "viewer", a.Id)
	res = request(mux, "GET", "/api/v1/clusters?perPage=1", "", token)
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || len(body.Data.Items) != 1 || body.Data.Items[0].ID != a.Id || body.Data.Total != 1 {
		t.Fatalf("cluster list leaked: %s", res.Body.String())
	}
	if res := request(mux, "GET", "/api/v1/clusters/"+a.Id, "", token); res.Code != 200 {
		t.Fatalf("granted cluster denied: %d %s", res.Code, res.Body.String())
	}
	if res := request(mux, "GET", "/api/v1/clusters/"+b.Id, "", token); res.Code != 403 {
		t.Fatalf("cross-cluster access accepted: %d", res.Code)
	}
	if err := app.Delete(binding); err != nil {
		t.Fatal(err)
	}
	if res := request(mux, "GET", "/api/v1/clusters/"+a.Id, "", token); res.Code != 403 {
		t.Fatalf("revoked grant remained effective: %d", res.Code)
	}
	if res := request(mux, "GET", "/api/v1/clusters?perPage=1000", "", token); res.Code != 400 {
		t.Fatal("unbounded page size accepted")
	}
}

func TestActionPermissionsAndScopeChecks(t *testing.T) {
	app, _ := setup(t)
	record := user(t, app, "users")
	a, b := cluster(t, app, "alpha"), cluster(t, app, "beta")
	routes, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	api.Register(routes, t.TempDir())
	called := 0
	routes.POST("/api/v1/clusters/{clusterId}/test-cancel", func(e *core.RequestEvent) error {
		called++
		return e.JSON(http.StatusOK, map[string]bool{"allowed": true})
	}).Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("jobs.cancel", true))
	mux, err := routes.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	token := login(t, mux)
	viewer := grant(t, app, record.Id, "viewer", a.Id)
	if res := request(mux, "POST", "/api/v1/clusters/"+a.Id+"/test-cancel", "{}", token); res.Code != 403 || called != 0 {
		t.Fatal("viewer executed state change")
	}
	if err := app.Delete(viewer); err != nil {
		t.Fatal(err)
	}
	grant(t, app, record.Id, "operator", a.Id)
	if res := request(mux, "POST", "/api/v1/clusters/"+a.Id+"/test-cancel", "{}", token); res.Code != 200 || called != 1 {
		t.Fatalf("operator denied allowed action: %d", res.Code)
	}
	if res := request(mux, "POST", "/api/v1/clusters/"+b.Id+"/test-cancel", "{}", token); res.Code != 403 || called != 1 {
		t.Fatal("operator crossed cluster boundary")
	}
	access, err := rbac.Resolve(app, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	if access.Allows("accounts.update", a.Id) || access.Allows("platform.roles.manage", "") || access.Allows("unlisted.action", a.Id) {
		t.Fatal("operator received additional permissions")
	}
}

func TestPlatformGrantAndNoPublicAuthorizationWrites(t *testing.T) {
	app, mux := setup(t)
	record := user(t, app, "users")
	cluster(t, app, "alpha")
	cluster(t, app, "beta")
	token := login(t, mux)
	grant(t, app, record.Id, "super_admin", "")
	res := request(mux, "GET", "/api/v1/clusters?perPage=1&page=2", "", token)
	var body struct {
		Data struct {
			Items []rbac.Cluster `json:"items"`
			Total int            `json:"totalItems"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || res.Code != 200 || len(body.Data.Items) != 1 || body.Data.Total != 2 {
		t.Fatalf("platform scope pagination: %d %s", res.Code, res.Body.String())
	}
	for _, collection := range []string{"roles", "permissions", "role_permissions", "user_platform_roles", "user_cluster_roles", "clusters"} {
		if res := request(mux, "POST", "/api/collections/"+collection+"/records", `{"code":"escalate"}`, token); res.Code != 403 {
			t.Fatalf("platform user bypassed API rules for %s: %d", collection, res.Code)
		}
	}
}

func TestPermissionBindingsDriveAccessRatherThanRoleName(t *testing.T) {
	app, _ := setup(t)
	record := user(t, app, "users")
	a := cluster(t, app, "alpha")
	roles, _ := app.FindCollectionByNameOrId("roles")
	custom := core.NewRecord(roles)
	custom.Set("code", "custom_reader")
	custom.Set("name", "Custom reader")
	custom.Set("scope", "cluster")
	if err := app.Save(custom); err != nil {
		t.Fatal(err)
	}
	permission, err := app.FindFirstRecordByFilter("permissions", "code = {:code}", dbx.Params{"code": "nodes.read"})
	if err != nil {
		t.Fatal(err)
	}
	bindings, _ := app.FindCollectionByNameOrId("role_permissions")
	binding := core.NewRecord(bindings)
	binding.Set("role", custom.Id)
	binding.Set("permission", permission.Id)
	if err := app.Save(binding); err != nil {
		t.Fatal(err)
	}
	grant(t, app, record.Id, "custom_reader", a.Id)
	access, err := rbac.Resolve(app, record.Id)
	if err != nil || !access.Allows("nodes.read", a.Id) || access.Allows("jobs.read", a.Id) {
		t.Fatal("permissions did not follow explicit bindings")
	}
	if _, err := rbac.Grant(app, record.Id, "super_admin", a.Id); err == nil {
		t.Fatal("platform role accepted as cluster role")
	}
	if _, err := rbac.Grant(app, record.Id, "viewer", ""); err == nil {
		t.Fatal("cluster role accepted without cluster")
	}
	// Even malformed grants inserted by a privileged database operator fail closed.
	grants, _ := app.FindCollectionByNameOrId("user_platform_roles")
	malformed := core.NewRecord(grants)
	malformed.Set("user", record.Id)
	malformed.Set("role", custom.Id)
	if err := app.Save(malformed); err != nil {
		t.Fatal(err)
	}
	access, err = rbac.Resolve(app, record.Id)
	if err != nil || access.Allows("nodes.read", "") {
		t.Fatal("cluster role leaked into platform scope")
	}
}
