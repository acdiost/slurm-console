package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"slurm-console/backend/internal/api"
	_ "slurm-console/backend/migrations"
)

const fixturePassword = "fixture-password-12345"

func setup(t *testing.T) (core.App, http.Handler) {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatalf("migration rerun: %v", err)
	}
	routes, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	publicDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(publicDir, "index.html"), []byte("<html>platform SPA</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	api.Register(routes, publicDir)
	mux, err := routes.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return app, mux
}

func user(t *testing.T, app core.App, collectionName string) *core.Record {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.SetEmail("fixture@example.com")
	record.SetPassword(fixturePassword)
	if collectionName == "users" {
		record.Set("name", "测试用户")
		record.Set("active", true)
	}
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

func request(mux http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}

func login(t *testing.T, mux http.Handler) string {
	t.Helper()
	res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"fixture@example.com","password":"`+fixturePassword+`"}`, "")
	if res.Code != 200 {
		t.Fatalf("login: %d %s", res.Code, res.Body.String())
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || result.Token == "" {
		t.Fatalf("missing token: %v", err)
	}
	return result.Token
}

func TestHealthAndAPIErrorContract(t *testing.T) {
	_, mux := setup(t)
	for _, sample := range []struct {
		path    string
		status  int
		content string
	}{
		{"/api/v1/health", 200, `"status":"ok"`},
		{"/api/v1/auth/me", 401, `"code":"UNAUTHENTICATED"`},
		{"/api/v1/missing", 404, `"code":"NOT_FOUND"`},
	} {
		res := request(mux, "GET", sample.path, "", "")
		if res.Code != sample.status || !strings.Contains(res.Body.String(), sample.content) {
			t.Fatalf("%s: %d %s", sample.path, res.Code, res.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if id := res.Header().Get("X-Request-ID"); id == "" || body["requestId"] != id {
			t.Fatal("missing or inconsistent request ID")
		}
		if res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("session response must not be cached")
		}
	}
	if res := request(mux, "GET", "/login", "", ""); res.Code != 200 || !strings.Contains(res.Body.String(), "platform SPA") {
		t.Fatalf("SPA fallback failed: %d", res.Code)
	}
	if res := request(mux, "GET", "/api/missing", "", ""); res.Code != 404 || strings.Contains(res.Body.String(), "platform SPA") {
		t.Fatal("API requests must not fall back to HTML")
	}
}

func TestNativeLoginRefreshAndCurrentUser(t *testing.T) {
	app, mux := setup(t)
	user(t, app, "users")
	token := login(t, mux)
	me := request(mux, "GET", "/api/v1/auth/me", "", token)
	if me.Code != 200 || !strings.Contains(me.Body.String(), `"email":"fixture@example.com"`) || strings.Contains(me.Body.String(), "tokenKey") {
		t.Fatalf("me: %d %s", me.Code, me.Body.String())
	}
	if res := request(mux, "POST", "/api/collections/users/auth-refresh", "{}", token); res.Code != 200 {
		t.Fatalf("refresh: %d %s", res.Code, res.Body.String())
	}
	if res := request(mux, "GET", "/api/v1/auth/me", "", "tampered-token"); res.Code != 401 {
		t.Fatalf("tampered token accepted: %d", res.Code)
	}
}

func TestDisabledAccountAndWrongCollectionAreRejected(t *testing.T) {
	app, mux := setup(t)
	record := user(t, app, "users")
	token := login(t, mux)
	record.Set("active", false)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	if res := request(mux, "GET", "/api/v1/auth/me", "", token); res.Code != 401 {
		t.Fatalf("disabled user token accepted: %d", res.Code)
	}
	if res := request(mux, "POST", "/api/collections/users/auth-refresh", "{}", token); res.Code == 200 {
		t.Fatal("disabled user refreshed token")
	}
	if res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"fixture@example.com","password":"`+fixturePassword+`"}`, ""); res.Code == 200 {
		t.Fatal("disabled user logged in")
	}
	superuser := user(t, app, core.CollectionNameSuperusers)
	adminToken, err := superuser.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	if res := request(mux, "GET", "/api/v1/auth/me", "", adminToken); res.Code != 403 {
		t.Fatalf("superuser accepted as platform user: %d", res.Code)
	}
}

func TestRegistrationAndSelfModificationAreLocked(t *testing.T) {
	app, mux := setup(t)
	res := request(mux, "POST", "/api/collections/users/records", `{"email":"intruder@example.com","password":"long-password-123","passwordConfirm":"long-password-123","active":true}`, "")
	if res.Code != 403 {
		t.Fatalf("public registration accepted: %d %s", res.Code, res.Body.String())
	}
	record := user(t, app, "users")
	token := login(t, mux)
	for _, path := range []string{"/api/collections/users/records/" + record.Id, "/api/collections/users/records"} {
		method := "GET"
		body := ""
		if strings.HasSuffix(path, record.Id) {
			method = "PATCH"
			body = `{"active":false}`
		}
		if res := request(mux, method, path, body, token); res.Code != 403 {
			t.Fatalf("unauthorized access %s: %d", path, res.Code)
		}
	}
}

func TestLoginRateLimit(t *testing.T) {
	_, mux := setup(t)
	for i := 0; i < 6; i++ {
		res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"missing@example.com","password":"wrong"}`, "")
		if i < 5 && res.Code != 400 {
			t.Fatalf("attempt %d: %d", i, res.Code)
		}
		if i == 5 && res.Code != 429 {
			t.Fatalf("rate limiter not enforced: %d", res.Code)
		}
	}
}
