package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/rbac"
)

func clusterWithAuthentication(t *testing.T, app core.App, name, method string) *core.Record {
	t.Helper()
	record := cluster(t, app, name)
	record.Set("authentication_method", method)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestOrdinaryClusterMetadataExposesOnlySavedAuthenticationBinding(t *testing.T) {
	app, mux := setup(t)
	actor := user(t, app, "users")
	want := map[string]string{}
	for _, method := range []string{"pam", "ldap", "freeipa", ""} {
		name := "metadata-" + method
		if method == "" {
			name = "metadata-unresolved"
		}
		record := clusterWithAuthentication(t, app, name, method)
		record.Set("connection_ref", "private-connection-marker")
		if err := app.Save(record); err != nil {
			t.Fatal(err)
		}
		grant(t, app, actor.Id, "viewer", record.Id)
		want[record.Id] = method
	}
	forbidden := clusterWithAuthentication(t, app, "metadata-forbidden", "ldap")
	token := login(t, mux)
	assertMetadata := func(endpoint string, expected map[string]string) {
		t.Helper()
		out := request(mux, "GET", endpoint, "", token)
		if out.Code != 200 {
			t.Fatalf("%s: %d %s", endpoint, out.Code, out.Body.String())
		}
		for _, secret := range []string{"private-connection-marker", "connectionRef", "connection_ref", "endpoint", "authType", "password", "tokenKey", forbidden.Id} {
			if strings.Contains(out.Body.String(), secret) {
				t.Fatalf("%s leaked private metadata %q: %s", endpoint, secret, out.Body.String())
			}
		}
		var body any
		if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		seen := map[string]string{}
		var visit func(any)
		visit = func(value any) {
			switch value := value.(type) {
			case map[string]any:
				if id, ok := value["id"].(string); ok {
					if method, exists := expected[id]; exists {
						if actual, present := value["authenticationMethod"].(string); !present || actual != method {
							t.Fatalf("%s binding for %s: expected %q, got %#v", endpoint, id, method, value["authenticationMethod"])
						} else {
							seen[id] = actual
						}
					}
				}
				for _, child := range value {
					visit(child)
				}
			case []any:
				for _, child := range value {
					visit(child)
				}
			}
		}
		visit(body)
		if len(seen) != len(expected) {
			t.Fatalf("%s omitted authorized cluster bindings: got %v, expected %v", endpoint, seen, expected)
		}
	}
	for _, endpoint := range []string{"/api/v1/clusters", "/api/v1/cluster-fleet", "/api/v1/auth/me"} {
		assertMetadata(endpoint, want)
	}
	for id, method := range want {
		assertMetadata("/api/v1/clusters/"+id, map[string]string{id: method})
	}
	if out := request(mux, "GET", "/api/v1/clusters/"+forbidden.Id, "", token); out.Code != 403 {
		t.Fatalf("unauthorized cluster metadata exposed: %d %s", out.Code, out.Body.String())
	}
}

func TestClusterAuthenticationRestrictsExternalIdentitiesAndPlatformWildcards(t *testing.T) {
	for _, source := range []string{"pam", "ldap", "freeipa"} {
		for _, platform := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/platform=%t", source, platform), func(t *testing.T) {
				app, mux := setup(t)
				record := user(t, app, "users")
				record.Set("auth_source", source)
				password := "directory-password"
				switch source {
				case "pam":
					auth.RegisterPAM(app, &pamFixture{})
					record.Set("username", "fixture_linux")
					password = "system-password"
				case "ldap":
					provider := &ldapFixture{id: "cluster-authentication-entry"}
					auth.RegisterLDAP(app, provider)
					record.Set("username", "ldap_fixture")
					record.Set("ldap_entry_id", provider.id)
				case "freeipa":
					provider := newIPA()
					auth.RegisterFreeIPA(app, provider)
					record.Set("username", provider.u.Username)
					record.Set("freeipa_entry_id", provider.u.ID)
				}
				if err := app.Save(record); err != nil {
					t.Fatal(err)
				}
				clusters := map[string]*core.Record{}
				for _, method := range []string{"pam", "ldap", "freeipa"} {
					candidate := clusterWithAuthentication(t, app, "authentication-"+method, method)
					clusters[method] = candidate
					if !platform {
						grant(t, app, record.Id, "viewer", candidate.Id)
					}
				}
				unresolved := cluster(t, app, "unresolved")
				if platform {
					grant(t, app, record.Id, "super_admin", "")
				} else {
					grant(t, app, record.Id, "viewer", unresolved.Id)
				}
				loginResponse := request(mux, "POST", "/api/collections/users/auth-with-password", payload(t, map[string]string{"identity": record.Email(), "password": password}), "")
				var session struct {
					Token string `json:"token"`
				}
				if loginResponse.Code != 200 || json.Unmarshal(loginResponse.Body.Bytes(), &session) != nil || session.Token == "" {
					t.Fatalf("external login: %d %s", loginResponse.Code, loginResponse.Body.String())
				}
				for method, candidate := range clusters {
					expected := 403
					if method == source {
						expected = 200
					}
					out := request(mux, "GET", "/api/v1/clusters/"+candidate.Id, "", session.Token)
					if out.Code != expected {
						t.Fatalf("%s identity accessing %s cluster: expected %d, got %d %s", source, method, expected, out.Code, out.Body.String())
					}
				}
				if out := request(mux, "GET", "/api/v1/clusters/"+unresolved.Id, "", session.Token); out.Code != 403 {
					t.Fatalf("unconfigured authentication accepted: %d", out.Code)
				}
				for _, endpoint := range []string{"/clusters", "/cluster-fleet"} {
					out := request(mux, "GET", "/api/v1"+endpoint+"?perPage=1", "", session.Token)
					var page struct {
						Data struct {
							Items []struct {
								ID      string       `json:"id"`
								Cluster rbac.Cluster `json:"cluster"`
							} `json:"items"`
							Total int `json:"totalItems"`
						} `json:"data"`
					}
					if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || page.Data.Total != 1 || len(page.Data.Items) != 1 {
						t.Fatalf("%s authentication filter/count: %d %s", endpoint, out.Code, out.Body.String())
					}
					item := page.Data.Items[0]
					if item.ID != clusters[source].Id && item.Cluster.ID != clusters[source].Id {
						t.Fatalf("%s leaked another provider: %s", endpoint, out.Body.String())
					}
					out = request(mux, "GET", "/api/v1"+endpoint+"?perPage=1&page=2", "", session.Token)
					if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Data.Items) != 0 || page.Data.Total != 1 {
						t.Fatalf("%s pagination leaked excluded cluster: %d %s", endpoint, out.Code, out.Body.String())
					}
				}
				before, err := app.CountRecords("user_cluster_roles")
				if err != nil {
					t.Fatal(err)
				}
				replacement := "pam"
				if source == replacement {
					replacement = "ldap"
				}
				selected := clusters[source]
				selected.Set("authentication_method", replacement)
				if err := app.Save(selected); err != nil {
					t.Fatal(err)
				}
				if out := request(mux, "GET", "/api/v1/clusters/"+selected.Id, "", session.Token); out.Code != 403 {
					t.Fatalf("existing token bypassed authentication switch: %d %s", out.Code, out.Body.String())
				}
				out := request(mux, "GET", "/api/v1/clusters", "", session.Token)
				var empty struct {
					Data struct {
						Total int `json:"totalItems"`
					} `json:"data"`
				}
				if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &empty) != nil || empty.Data.Total != 0 {
					t.Fatalf("old authentication remained selectable: %d %s", out.Code, out.Body.String())
				}
				after, err := app.CountRecords("user_cluster_roles")
				if err != nil || before != after {
					t.Fatalf("switch deleted grants: before=%d after=%d err=%v", before, after, err)
				}
				for method, candidate := range clusters {
					if method == source {
						continue
					}
					saved, err := app.FindRecordById("clusters", candidate.Id)
					if err != nil || saved.GetString("authentication_method") != method {
						t.Fatalf("another cluster changed: method=%s err=%v", method, err)
					}
				}
				selected.Set("authentication_method", source)
				if err := app.Save(selected); err != nil {
					t.Fatal(err)
				}
				if out := request(mux, "GET", "/api/v1/clusters/"+selected.Id, "", session.Token); out.Code != 200 {
					t.Fatalf("preserved grant did not recover after switching back: %d %s", out.Code, out.Body.String())
				}
			})
		}
	}
}

func TestClusterAuthenticationSwitchRejectsInflightFleetForExternalPlatformAdmin(t *testing.T) {
	app, mux := setup(t)
	started, release := make(chan struct{}), make(chan struct{})
	var startedOnce, releasedOnce sync.Once
	unblock := func() { releasedOnce.Do(func() { close(release) }) }
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "nodes") {
			startedOnce.Do(func() { close(started) })
			<-release
		}
		writeFixture(w, r)
	})
	t.Cleanup(unblock)
	manager := clusterconfig.Register(app, registry)
	input := validConnection()
	input.AuthenticationMethod = "pam"
	configured, err := manager.Save("", input)
	if err != nil {
		t.Fatal(err)
	}
	person := user(t, app, "users")
	person.Set("auth_source", "pam")
	person.Set("username", "fixture_linux")
	if err := app.Save(person); err != nil {
		t.Fatal(err)
	}
	auth.RegisterPAM(app, &pamFixture{})
	grant(t, app, person.Id, "super_admin", "")
	token, err := person.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct {
		code int
		body string
	}, 1)
	go func() {
		out := request(mux, "GET", "/api/v1/cluster-fleet", "", token)
		done <- struct {
			code int
			body string
		}{out.Code, out.Body.String()}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("fleet source did not start")
	}
	input = configured.Input
	input.AuthenticationMethod = "ldap"
	_, err = manager.Save(configured.ID, input)
	unblock()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.code != 409 || strings.Contains(out.body, `"items"`) {
			t.Fatalf("authentication switch exposed in-flight data: %d %s", out.code, out.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fleet request did not complete")
	}
}
