package api_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/pamauth"
)

type pamFixture struct {
	calls        []pamauth.Request
	accountError error
}

func (p *pamFixture) Run(_ context.Context, req pamauth.Request) (pamauth.Result, error) {
	p.calls = append(p.calls, req)
	if req.Mode == "authenticate" && req.Password != "system-password" {
		return pamauth.Result{}, pamauth.ErrDenied
	}
	if p.accountError != nil {
		return pamauth.Result{}, p.accountError
	}
	return pamauth.Result{OK: true, Username: req.Username}, nil
}

func TestPAMLoginUsesSystemPasswordAndPreservesRBAC(t *testing.T) {
	app, mux := setup(t)
	provider := &pamFixture{}
	auth.RegisterPAM(app, provider)
	record := user(t, app, "users")
	record.Set("auth_source", "pam")
	record.Set("username", "fixture_linux")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	a := cluster(t, app, "pam-cluster")
	grant(t, app, record.Id, "viewer", a.Id)
	for _, identity := range []string{"fixture_linux", record.Email()} {
		res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"`+identity+`","password":"system-password"}`, "")
		if res.Code != 200 {
			t.Fatalf("PAM login: %d %s", res.Code, res.Body.String())
		}
		var result struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		me := request(mux, "GET", "/api/v1/auth/me", "", result.Token)
		if me.Code != 200 || !strings.Contains(me.Body.String(), `"authSource":"pam"`) || !strings.Contains(me.Body.String(), `"Viewer"`) {
			t.Fatalf("PAM me: %d %s", me.Code, me.Body.String())
		}
		if res := request(mux, "POST", "/api/collections/users/auth-refresh", "{}", result.Token); res.Code != 200 {
			t.Fatalf("PAM refresh: %d %s", res.Code, res.Body.String())
		}
		if res := request(mux, "GET", "/api/v1/clusters/"+a.Id, "", result.Token); res.Code != 200 {
			t.Fatalf("PAM RBAC: %d", res.Code)
		}
	}
	res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"fixture_linux","password":"`+fixturePassword+`"}`, "")
	if res.Code != 400 {
		t.Fatalf("PAM fell back to stored PB hash: %d", res.Code)
	}
	for _, call := range provider.calls {
		if call.Username != "fixture_linux" || call.RemoteHost != "192.0.2.1" {
			t.Fatalf("incorrect PAM identity or peer: %+v", call)
		}
		if call.Mode == "account" && call.Password != "" {
			t.Fatal("password passed to account check")
		}
	}
}

func TestPAMAccountRecheckedForExistingTokensAndRefresh(t *testing.T) {
	app, mux := setup(t)
	provider := &pamFixture{}
	auth.RegisterPAM(app, provider)
	record := user(t, app, "users")
	record.Set("auth_source", "pam")
	record.Set("username", "fixture_linux")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	token, err := record.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		err    error
		status int
	}{{pamauth.ErrDenied, 401}, {pamauth.ErrUnavailable, 503}} {
		provider.accountError = sample.err
		for _, path := range []string{"/api/v1/auth/me", "/api/v1/clusters", "/api/collections/users/auth-refresh"} {
			method := "GET"
			if strings.HasSuffix(path, "auth-refresh") {
				method = "POST"
			}
			if res := request(mux, method, path, "{}", token); res.Code != sample.status {
				t.Fatalf("%s: expected %d, got %d %s", path, sample.status, res.Code, res.Body.String())
			}
		}
	}
}

func TestPAMDisabledFailsClosedAndDoesNotAffectLocalAccounts(t *testing.T) {
	app, mux := setup(t)
	auth.RegisterPAM(app, nil)
	record := user(t, app, "users")
	login(t, mux) // Ordinary local authentication still works with PAM disabled.
	record.Set("auth_source", "pam")
	record.Set("username", "fixture_linux")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"fixture_linux","password":"`+fixturePassword+`"}`, "")
	if res.Code != 503 {
		t.Fatalf("disabled PAM accepted local password: %d", res.Code)
	}
}

func TestPAMRequiresExplicitActivePlatformMapping(t *testing.T) {
	app, mux := setup(t)
	provider := &pamFixture{}
	auth.RegisterPAM(app, provider)
	record := user(t, app, "users")
	record.Set("auth_source", "pam")
	record.Set("username", "fixture_linux")
	record.Set("active", false)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{"unregistered_linux", "fixture_linux"} {
		res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"`+identity+`","password":"system-password"}`, "")
		if res.Code != 400 {
			t.Fatalf("unmapped/disabled identity accepted: %d", res.Code)
		}
	}
	if len(provider.calls) != 0 {
		t.Fatal("PAM invoked without active platform mapping")
	}
}
