package api_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/ldapauth"
)

type ldapFixture struct {
	id      string
	failure error
	calls   int
}

func (p *ldapFixture) Lookup(_ context.Context, username string) (ldapauth.Identity, error) {
	return ldapauth.Identity{Username: username, ID: p.id}, p.failure
}
func (p *ldapFixture) Probe(context.Context) error { return p.failure }
func (p *ldapFixture) Check(_ context.Context, username, id string) error {
	p.calls++
	if p.failure != nil {
		return p.failure
	}
	if username != "ldap_fixture" || id != p.id {
		return ldapauth.ErrDenied
	}
	return nil
}
func (p *ldapFixture) Authenticate(ctx context.Context, username, id, password string) error {
	if password != "directory-password" {
		p.calls++
		return ldapauth.ErrDenied
	}
	return p.Check(ctx, username, id)
}

func TestLDAPNativeLoginUsesDirectoryIdentityAndRetainsClusterRBAC(t *testing.T) {
	app, mux := setup(t)
	provider := &ldapFixture{id: "immutable-directory-id"}
	auth.RegisterPAM(app, nil)
	auth.RegisterLDAP(app, provider)
	record := user(t, app, "users")
	record.Set("auth_source", "ldap")
	record.Set("username", "ldap_fixture")
	record.Set("ldap_entry_id", provider.id)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	a, b := cluster(t, app, "ldap-alpha"), cluster(t, app, "ldap-beta")
	grant(t, app, record.Id, "viewer", a.Id)
	for _, identity := range []string{record.Email(), "ldap_fixture"} {
		res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"`+identity+`","password":"directory-password"}`, "")
		if res.Code != 200 || strings.Contains(res.Body.String(), "ldap_entry_id") || strings.Contains(res.Body.String(), provider.id) {
			t.Fatalf("LDAP login: %d %s", res.Code, res.Body.String())
		}
		var body struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if res := request(mux, "GET", "/api/v1/auth/me", "", body.Token); res.Code != 200 || !strings.Contains(res.Body.String(), `"authSource":"ldap"`) {
			t.Fatalf("LDAP me: %d %s", res.Code, res.Body.String())
		}
		if res := request(mux, "POST", "/api/collections/users/auth-refresh", "{}", body.Token); res.Code != 200 {
			t.Fatalf("refresh: %d", res.Code)
		}
		if res := request(mux, "GET", "/api/v1/clusters/"+a.Id, "", body.Token); res.Code != 200 {
			t.Fatal("lost LDAP viewer grant")
		}
		if res := request(mux, "GET", "/api/v1/clusters/"+b.Id, "", body.Token); res.Code != 403 {
			t.Fatal("LDAP user crossed cluster scope")
		}
	}
	if res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"ldap_fixture","password":"`+fixturePassword+`"}`, ""); res.Code != 400 {
		t.Fatal("LDAP fell back to PB password")
	}
}

func TestLDAPRechecksDirectoryIdentityAndAvailabilityForExistingTokens(t *testing.T) {
	app, mux := setup(t)
	provider := &ldapFixture{id: "original-id"}
	auth.RegisterLDAP(app, provider)
	record := user(t, app, "users")
	record.Set("auth_source", "ldap")
	record.Set("username", "ldap_fixture")
	record.Set("ldap_entry_id", provider.id)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	token, err := record.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		id      string
		failure error
		status  int
	}{{"replacement-id", nil, 401}, {"original-id", ldapauth.ErrDenied, 401}, {"original-id", ldapauth.ErrUnavailable, 503}} {
		provider.id, provider.failure = sample.id, sample.failure
		for _, path := range []string{"/api/v1/auth/me", "/api/v1/clusters", "/api/collections/users/auth-refresh"} {
			method := "GET"
			if strings.HasSuffix(path, "auth-refresh") {
				method = "POST"
			}
			if res := request(mux, method, path, "{}", token); res.Code != sample.status {
				t.Fatalf("%s: expected %d got %d", path, sample.status, res.Code)
			}
		}
	}
	// A directory outage must not block local recovery accounts.
	record.Set("auth_source", "local")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	login(t, mux)
}

func TestLDAPRequiresExplicitActiveMappingAndNeverFallsBackWhenDisabled(t *testing.T) {
	app, mux := setup(t)
	provider := &ldapFixture{id: "directory-id"}
	auth.RegisterLDAP(app, provider)
	record := user(t, app, "users")
	record.Set("auth_source", "ldap")
	record.Set("username", "ldap_fixture")
	record.Set("ldap_entry_id", provider.id)
	record.Set("active", false)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{"ldap_fixture", "unmapped_directory_user"} {
		if res := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"`+identity+`","password":"directory-password"}`, ""); res.Code != 400 {
			t.Fatal("inactive or unmapped directory user accepted")
		}
	}
	if provider.calls != 0 {
		t.Fatal("LDAP contacted without active mapping")
	}
	app2, mux2 := setup(t)
	auth.RegisterLDAP(app2, nil)
	record2 := user(t, app2, "users")
	record2.Set("auth_source", "ldap")
	record2.Set("username", "ldap_fixture")
	record2.Set("ldap_entry_id", provider.id)
	if err := app2.Save(record2); err != nil {
		t.Fatal(err)
	}
	if res := request(mux2, "POST", "/api/collections/users/auth-with-password", `{"identity":"ldap_fixture","password":"`+fixturePassword+`"}`, ""); res.Code != 503 {
		t.Fatal("disabled LDAP accepted PB password")
	}
}
