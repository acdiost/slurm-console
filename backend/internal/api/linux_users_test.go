package api_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"slurm-console/backend/internal/linuxusers"
)

func linuxCluster(t *testing.T, app core.App, slug string) *core.Record {
	t.Helper()
	c := cluster(t, app, slug)
	c.Set("enabled", true)
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}
	return c
}

type linuxReaderFunc func(context.Context, linuxusers.Profile, linuxusers.Request) (linuxusers.Snapshot, error)

func (f linuxReaderFunc) Read(ctx context.Context, p linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
	return f(ctx, p, r)
}
func linuxSnapshot(r linuxusers.Request) linuxusers.Snapshot {
	min := uint32(1000)
	items := []linuxusers.User{{Name: "root", UID: 0, GID: 0, Comment: "root", Home: "/root", Shell: "/bin/bash", Source: "local", Kind: "privileged"}, {Name: "service", UID: 201, GID: 201, Home: "/var/lib/service", Shell: "/sbin/nologin", Source: "local", Kind: "system"}, {Name: "worker", UID: 1001, GID: 1001, Comment: "Local worker", Home: "/home/worker", Shell: "/bin/bash", Source: "local", Kind: "regular"}}
	mode := "nss"
	if r.Name != "" {
		mode = "lookup"
		items = []linuxusers.User{{Name: r.Name, UID: 9001, GID: 9001, Comment: "Directory lookup", Home: "/home/directory", Shell: "/bin/bash", Source: "nss", Kind: "regular"}}
	}
	return linuxusers.Snapshot{Version: 1, Items: items, UIDMin: &min, Enumeration: mode, FetchedAt: time.Now().UTC().Format(time.RFC3339Nano)}
}
func TestLinuxUsersScopesPermissionsAndNeverExposesConnectionDetails(t *testing.T) {
	app, mux := setup(t)
	a := linuxCluster(t, app, "alpha")
	b := linuxCluster(t, app, "beta")
	person := user(t, app, "users")
	var calls atomic.Int32
	linuxusers.Register(app, &linuxusers.Registry{Clusters: map[string]linuxusers.Profile{a.Id: {Label: "Alpha NSS", Transport: "local"}, b.Id: {Label: "Secret endpoint", Transport: "ssh", Host: "192.0.2.99", IdentityFile: "/private/key"}}}, linuxReaderFunc(func(_ context.Context, p linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
		calls.Add(1)
		if p.Label != "Alpha NSS" {
			t.Fatal("unauthorized source accessed")
		}
		return linuxSnapshot(r), nil
	}))
	token := login(t, mux)
	path := "/api/v1/clusters/" + a.Id + "/linux-users"
	if r := request(mux, "GET", path, "", token); r.Code != 403 || calls.Load() != 0 {
		t.Fatal(r.Body.String())
	}
	binding := grant(t, app, person.Id, "viewer", a.Id)
	r := request(mux, "GET", path, "", token)
	if r.Code != 200 || calls.Load() != 1 || !strings.Contains(r.Body.String(), `"sourceLabel":"Alpha NSS"`) {
		t.Fatal(r.Body.String())
	}
	for _, v := range []string{"IdentityFile", "192.0.2.99", "/private/key", "password", "shadow"} {
		if strings.Contains(r.Body.String(), v) {
			t.Fatal("sensitive metadata leaked")
		}
	}
	if r = request(mux, "GET", "/api/v1/clusters/"+b.Id+"/linux-users", "", token); r.Code != 403 || calls.Load() != 1 {
		t.Fatal("cross-cluster read allowed")
	}
	if err := app.Delete(binding); err != nil {
		t.Fatal(err)
	}
	if r = request(mux, "GET", path, "", token); r.Code != 403 || calls.Load() != 1 {
		t.Fatal("revoked token reached source")
	}
	if r = request(mux, "GET", path, "", ""); r.Code != 401 {
		t.Fatal(r.Code)
	}
}
func TestLinuxUsersFilterSortPaginationAndExactDirectoryLookup(t *testing.T) {
	app, mux := setup(t)
	c := linuxCluster(t, app, "alpha")
	person := user(t, app, "users")
	grant(t, app, person.Id, "viewer", c.Id)
	linuxusers.Register(app, &linuxusers.Registry{Clusters: map[string]linuxusers.Profile{c.Id: {Label: "NSS", Transport: "local"}}}, linuxReaderFunc(func(_ context.Context, _ linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
		return linuxSnapshot(r), nil
	}))
	token := login(t, mux)
	path := "/api/v1/clusters/" + c.Id + "/linux-users"
	r := request(mux, "GET", path+"?perPage=1&page=2&sort=uid&order=desc", "", token)
	var body struct {
		Data struct {
			Items       []linuxusers.User
			TotalItems  int
			Enumeration string
		}
	}
	_ = json.Unmarshal(r.Body.Bytes(), &body)
	if r.Code != 200 || body.Data.TotalItems != 3 || len(body.Data.Items) != 1 || body.Data.Items[0].Name != "service" {
		t.Fatal(r.Body.String())
	}
	for _, query := range []string{"source=local&kind=regular", "q=Local%20worker", "q=%2Fhome%2Fworker"} {
		r = request(mux, "GET", path+"?"+query, "", token)
		if r.Code != 200 || !strings.Contains(r.Body.String(), `"totalItems":1`) || !strings.Contains(r.Body.String(), `"name":"worker"`) {
			t.Fatal(r.Body.String())
		}
	}
	r = request(mux, "GET", path+"/directory@realm", "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"enumeration":"lookup"`) || !strings.Contains(r.Body.String(), `"name":"directory@realm"`) {
		t.Fatal(r.Body.String())
	}
	r = request(mux, "GET", path+"?page=9", "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"items":[]`) {
		t.Fatal(r.Body.String())
	}
}
func TestLinuxUsersRejectInvalidQueriesAndUnconfiguredOrDisabledSources(t *testing.T) {
	app, mux := setup(t)
	c := linuxCluster(t, app, "alpha")
	person := user(t, app, "users")
	grant(t, app, person.Id, "viewer", c.Id)
	var calls atomic.Int32
	provider := linuxReaderFunc(func(_ context.Context, _ linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
		calls.Add(1)
		return linuxSnapshot(r), nil
	})
	linuxusers.Register(app, &linuxusers.Registry{Clusters: map[string]linuxusers.Profile{c.Id: {Label: "NSS", Transport: "local"}}}, provider)
	token := login(t, mux)
	path := "/api/v1/clusters/" + c.Id + "/linux-users"
	for _, query := range []string{"page=0", "perPage=101", "q=x&q=y", "source=ldap", "kind=admin", "sort=home", "order=invalid", "command=id", "q=%00", "q=" + strings.Repeat("x", 129)} {
		r := request(mux, "GET", path+"?"+query, "", token)
		if r.Code != 400 || calls.Load() != 0 {
			t.Fatal(query, r.Code, r.Body.String())
		}
	}
	r := request(mux, "GET", path+"/-option", "", token)
	if r.Code != 400 || calls.Load() != 0 {
		t.Fatal(r.Body.String())
	}
	c.Set("enabled", false)
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}
	r = request(mux, "GET", path, "", token)
	if r.Code != 409 || calls.Load() != 0 {
		t.Fatal(r.Body.String())
	}
	c.Set("enabled", true)
	_ = app.Save(c)
	linuxusers.Register(app, nil, provider)
	r = request(mux, "GET", path, "", token)
	if r.Code != 503 || !strings.Contains(r.Body.String(), "LINUX_USERS_NOT_CONFIGURED") || calls.Load() != 0 {
		t.Fatal(r.Body.String())
	}
}
func TestLinuxUsersDiscardChangedAuthorizationOrConfigurationDuringRead(t *testing.T) {
	for _, mode := range []string{"permission", "configuration", "invalid-source-response"} {
		t.Run(mode, func(t *testing.T) {
			app, mux := setup(t)
			c := linuxCluster(t, app, "alpha")
			person := user(t, app, "users")
			binding := grant(t, app, person.Id, "viewer", c.Id)
			linuxusers.Register(app, &linuxusers.Registry{Clusters: map[string]linuxusers.Profile{c.Id: {Label: "NSS", Transport: "local"}}}, linuxReaderFunc(func(_ context.Context, _ linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
				result := linuxSnapshot(r)
				switch mode {
				case "permission":
					if err := app.Delete(binding); err != nil {
						t.Fatal(err)
					}
				case "configuration":
					c.Set("enabled", false)
					if err := app.Save(c); err != nil {
						t.Fatal(err)
					}
				case "invalid-source-response":
					result.Items[1].UID = 0
				}
				return result, nil
			}))
			token := login(t, mux)
			r := request(mux, "GET", "/api/v1/clusters/"+c.Id+"/linux-users", "", token)
			expected := 403
			if mode == "configuration" {
				expected = 409
			}
			if mode == "invalid-source-response" {
				expected = 503
			}
			if r.Code != expected || strings.Contains(r.Body.String(), `"items"`) {
				t.Fatal(r.Code, r.Body.String())
			}
		})
	}
}
