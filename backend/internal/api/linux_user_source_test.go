package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"slurm-console/backend/internal/linuxusers"
)

func sourceAdmin(t *testing.T) (core.App, http.Handler, *core.Record, *core.Record, string) {
	t.Helper()
	app, mux := setup(t)
	c := linuxCluster(t, app, "source-cluster")
	person := user(t, app, "users")
	grant(t, app, person.Id, "super_admin", "")
	linuxusers.Register(app, linuxReaderFunc(func(_ context.Context, _ linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
		return linuxSnapshot(r), nil
	}))
	return app, mux, c, person, login(t, mux)
}
func sourcePath(cluster string) string { return "/api/v1/clusters/" + cluster + "/linux-user-source" }
func localSource(revision int) linuxusers.SourceInput {
	return linuxusers.SourceInput{Revision: revision, Label: "Local identity", Transport: "local"}
}
func sshSource(revision int) linuxusers.SourceInput {
	return linuxusers.SourceInput{Revision: revision, Label: "Remote identity", Transport: "ssh", Host: "192.0.2.8", Port: 22, Username: "inventory", PrivateKey: "PRIVATE-IDENTITY-CONTENT\n", KnownHosts: "192.0.2.8 ssh-ed25519 HOST-CONTENT\n"}
}
func putSource(t *testing.T, mux http.Handler, cluster, token string, input linuxusers.SourceInput) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return request(mux, "PUT", sourcePath(cluster), string(raw), token)
}
func sourceData(t *testing.T, r *httptest.ResponseRecorder) linuxusers.SourceView {
	t.Helper()
	var body struct {
		Data      linuxusers.SourceView
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil || r.Code != 200 || body.RequestID == "" {
		t.Fatal(r.Code, r.Body.String(), err)
	}
	return body.Data
}
func savedSource(t *testing.T, app core.App, cluster string) *core.Record {
	t.Helper()
	r, err := app.FindFirstRecordByData("linux_user_sources", "cluster", cluster)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLinuxSourceInitialContractValidationAndMissingCluster(t *testing.T) {
	_, mux, c, _, token := sourceAdmin(t)
	r := request(mux, "GET", sourcePath(c.Id), "", token)
	v := sourceData(t, r)
	if v.Configured || v.Revision != 0 || v.Transport != "local" || v.PrivateKeyConfigured {
		t.Fatal(v)
	}
	var body struct{ Data map[string]any }
	_ = json.Unmarshal(r.Body.Bytes(), &body)
	for _, field := range []string{"configured", "revision", "label", "transport", "host", "port", "username", "knownHosts", "privateKeyConfigured", "manageLocal"} {
		if _, ok := body.Data[field]; !ok {
			t.Fatal("missing source field", field)
		}
	}
	if len(body.Data) != 10 {
		t.Fatal("unexpected source fields", body.Data)
	}
	for _, raw := range []string{
		`{}`, `{"revision":null}`, `{"revision":0,"revision":0}`, `{"revision":0,"identityFile":"/private/key"}`, `{"revision":0,"knownHostsFile":"/etc/ssh/known_hosts"}`,
	} {
		if r := request(mux, "PUT", sourcePath(c.Id), raw, token); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	invalid := sshSource(0)
	invalid.Username = "root"
	if r := putSource(t, mux, c.Id, token, invalid); r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	invalid = sshSource(0)
	invalid.ManageLocal = true
	if r := putSource(t, mux, c.Id, token, invalid); r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	invalid = sshSource(0)
	invalid.PrivateKey = ""
	if r := putSource(t, mux, c.Id, token, invalid); r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		raw := `{"revision":0}`
		if method == "PUT" {
			b, _ := json.Marshal(localSource(0))
			raw = string(b)
		}
		if r := request(mux, method, sourcePath("missingcluster1"), raw, token); r.Code != 404 {
			t.Fatal(method, r.Code, r.Body.String())
		}
	}
}

func TestLinuxSourceRequiresBothPermissionsForEveryMethod(t *testing.T) {
	for _, permission := range []string{"platform.settings.manage", "linux_users.read"} {
		t.Run(permission, func(t *testing.T) {
			app, mux, c, _, token := sourceAdmin(t)
			rows, err := app.FindRecordsByFilter("role_permissions", "permission.code={:code}", "", 0, 0, map[string]any{"code": permission})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if err := app.Delete(r); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			linuxusers.Register(app, linuxReaderFunc(func(_ context.Context, _ linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
				calls++
				return linuxSnapshot(r), nil
			}))
			for _, method := range []string{"GET", "PUT", "DELETE"} {
				raw := `{"revision":0}`
				if method == "PUT" {
					b, _ := json.Marshal(localSource(0))
					raw = string(b)
				}
				if r := request(mux, method, sourcePath(c.Id), raw, token); r.Code != 403 {
					t.Fatal(method, r.Code, r.Body.String())
				}
			}
			if calls != 0 {
				t.Fatal("unauthorized probe")
			}
		})
	}
}

func TestLinuxSourceChangesApplyImmediatelyRetainKeyAndClearSSH(t *testing.T) {
	app, mux, c, _, token := sourceAdmin(t)
	input := sshSource(0)
	v := sourceData(t, putSource(t, mux, c.Id, token, input))
	if !v.Configured || v.Revision != 1 || !v.PrivateKeyConfigured || v.KnownHosts != input.KnownHosts || v.ManageLocal {
		t.Fatal(v)
	}
	if savedSource(t, app, c.Id).GetString("private_key") != input.PrivateKey {
		t.Fatal("private key content changed")
	}
	seen := 0
	linuxusers.Register(app, linuxReaderFunc(func(_ context.Context, p linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
		seen++
		if p.PrivateKey != input.PrivateKey || p.Host != input.Host {
			t.Fatal("runtime did not use saved source")
		}
		return linuxSnapshot(r), nil
	}))
	if r := request(mux, "GET", "/api/v1/clusters/"+c.Id+"/linux-users", "", token); r.Code != 200 || seen != 1 {
		t.Fatal(r.Code, r.Body.String())
	}
	retained := input
	retained.Revision, retained.PrivateKey, retained.Label = 1, "", "Renamed identity"
	v = sourceData(t, putSource(t, mux, c.Id, token, retained))
	if v.Revision != 2 || !v.PrivateKeyConfigured || savedSource(t, app, c.Id).GetString("private_key") != input.PrivateKey {
		t.Fatal("key retention failed")
	}
	linuxusers.Register(app, linuxReaderFunc(func(_ context.Context, p linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
		if p.Transport != "local" || p.PrivateKey != "" || p.KnownHosts != "" || p.Host != "" || p.Port != 0 || p.Username != "" {
			t.Fatal("local source retained SSH settings")
		}
		return linuxSnapshot(r), nil
	}))
	local := localSource(2)
	local.PrivateKey, local.KnownHosts, local.Host, local.Port, local.Username = input.PrivateKey, input.KnownHosts, input.Host, 22, input.Username
	local.ManageLocal = true
	v = sourceData(t, putSource(t, mux, c.Id, token, local))
	if v.Revision != 3 || v.PrivateKeyConfigured || v.KnownHosts != "" || !v.ManageLocal {
		t.Fatal(v)
	}
	stored := savedSource(t, app, c.Id)
	if stored.GetString("private_key") != "" || stored.GetString("known_hosts") != "" || stored.GetString("host") != "" {
		t.Fatal("local cutover retained credentials")
	}
	if linuxusers.For(app).ConfiguredCount() != 1 {
		t.Fatal("configured count ignored database")
	}
	for _, method := range []string{"PUT", "DELETE"} {
		if method == "PUT" {
			if r := putSource(t, mux, c.Id, token, localSource(2)); r.Code != 409 {
				t.Fatal(r.Code, r.Body.String())
			}
		} else {
			if r := request(mux, method, sourcePath(c.Id), `{"revision":2}`, token); r.Code != 409 {
				t.Fatal(r.Code, r.Body.String())
			}
		}
	}
	v = sourceData(t, request(mux, "DELETE", sourcePath(c.Id), `{"revision":3}`, token))
	if v.Configured || v.Revision != 4 || v.PrivateKeyConfigured || v.Transport != "local" || v.ManageLocal {
		t.Fatal(v)
	}
	if linuxusers.For(app).ConfiguredCount() != 0 {
		t.Fatal("deleted source counted")
	}
	if r := request(mux, "GET", "/api/v1/clusters/"+c.Id+"/linux-users", "", token); r.Code != 503 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestLinuxSourceProbeFailuresAndStaleCommitsPreservePriorSource(t *testing.T) {
	for _, mode := range []string{"probe-error", "invalid-response", "permission", "cluster-change", "source-change", "audit", "save-revocation", "audit-revocation"} {
		t.Run(mode, func(t *testing.T) {
			app, mux, c, person, token := sourceAdmin(t)
			sourceData(t, putSource(t, mux, c.Id, token, localSource(0)))
			before := savedSource(t, app, c.Id)
			clusterBefore, _ := app.FindRecordById("clusters", c.Id)
			linuxusers.Register(app, linuxReaderFunc(func(_ context.Context, _ linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
				result := linuxSnapshot(r)
				switch mode {
				case "probe-error":
					return result, errors.New(sshSource(1).PrivateKey)
				case "invalid-response":
					result.Version = 99
				case "permission":
					person.Set("active", false)
					if err := app.Save(person); err != nil {
						t.Fatal(err)
					}
				case "cluster-change":
					clusterBefore.Set("revision", clusterBefore.GetInt("revision")+1)
					if err := app.Save(clusterBefore); err != nil {
						t.Fatal(err)
					}
				case "source-change":
					if _, err := linuxusers.For(app).DeleteSource(context.Background(), person.Id, c.Id, "concurrent-delete", 1); err != nil {
						t.Fatal(err)
					}
				}
				return result, nil
			}))
			if mode == "audit" {
				app.OnRecordCreate("operation_audits").BindFunc(func(*core.RecordEvent) error { return errors.New(sshSource(1).PrivateKey) })
			}
			if mode == "save-revocation" {
				app.OnRecordUpdate("linux_user_sources").BindFunc(func(e *core.RecordEvent) error {
					p, err := e.App.FindRecordById("users", person.Id)
					if err != nil {
						return err
					}
					p.Set("active", false)
					if err := e.App.Save(p); err != nil {
						return err
					}
					return e.Next()
				})
			}
			if mode == "audit-revocation" {
				app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error {
					p, err := e.App.FindRecordById("users", person.Id)
					if err != nil {
						return err
					}
					p.Set("active", false)
					if err := e.App.Save(p); err != nil {
						return err
					}
					return e.Next()
				})
			}
			r := putSource(t, mux, c.Id, token, sshSource(1))
			expected := 503
			if mode == "permission" || mode == "save-revocation" || mode == "audit-revocation" {
				expected = 403
			}
			if mode == "cluster-change" || mode == "source-change" {
				expected = 409
			}
			if r.Code != expected || strings.Contains(r.Body.String(), "PRIVATE-IDENTITY-CONTENT") {
				t.Fatal(r.Code, r.Body.String())
			}
			after := savedSource(t, app, c.Id)
			if mode == "source-change" {
				if after.GetBool("configured") || after.GetInt("revision") != 2 {
					t.Fatal("stale probe overwrote concurrent deletion")
				}
			} else if after.GetString("transport") != before.GetString("transport") || after.GetInt("revision") != 1 || after.GetString("private_key") != "" {
				t.Fatal("failed save modified old source")
			}
			count, err := app.CountRecords("operation_audits")
			if err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if mode == "source-change" {
				want = 2
			}
			if int64(count) != want {
				t.Fatal("failed mutation committed an audit", count)
			}
		})
	}
}

func TestLinuxSourceSecretsStayOutOfResponsesAuditsAndRecordAPIs(t *testing.T) {
	app, mux, c, _, token := sourceAdmin(t)
	input := sshSource(0)
	r := putSource(t, mux, c.Id, token, input)
	sourceData(t, r)
	if strings.Contains(r.Body.String(), "PRIVATE-IDENTITY-CONTENT") || strings.Contains(r.Body.String(), `"privateKey":`) {
		t.Fatal("private key leaked")
	}
	get := request(mux, "GET", sourcePath(c.Id), "", token)
	if get.Code != 200 || strings.Contains(get.Body.String(), "PRIVATE-IDENTITY-CONTENT") {
		t.Fatal("private key leaked from GET")
	}
	rows, err := app.FindRecordsByFilter("operation_audits", "target_type='linux_user_source'", "", 0, 0)
	if err != nil || len(rows) != 1 {
		t.Fatal(err, len(rows))
	}
	raw, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE-IDENTITY-CONTENT", "HOST-CONTENT", input.Host, input.Username} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("source connection data leaked to audit")
		}
	}
	for _, collection := range []string{"linux_user_sources", "linux_user_source_import"} {
		for _, auth := range []string{"", token} {
			res := request(mux, "GET", "/api/collections/"+collection+"/records", "", auth)
			if res.Code == 200 || strings.Contains(res.Body.String(), "PRIVATE-IDENTITY-CONTENT") {
				t.Fatal("locked record collection exposed", collection, res.Code)
			}
		}
	}
}

func TestLinuxSourceSavedAndDeletedStateSurvivesRestart(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprint(deleted), func(t *testing.T) {
			app, mux, c, person, token := sourceAdmin(t)
			sourceData(t, putSource(t, mux, c.Id, token, sshSource(0)))
			if deleted {
				sourceData(t, request(mux, "DELETE", sourcePath(c.Id), `{"revision":1}`, token))
			}
			dataDir := app.DataDir()
			if err := app.ResetBootstrapState(); err != nil {
				t.Fatal(err)
			}
			restarted := core.NewBaseApp(core.BaseAppConfig{DataDir: dataDir})
			if err := restarted.Bootstrap(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = restarted.ResetBootstrapState() })
			s := linuxusers.Register(restarted, nil)
			v, err := s.GetSource(person.Id, c.Id)
			if err != nil || v.Configured == deleted || v.PrivateKeyConfigured == deleted {
				t.Fatal(v, err)
			}
			wantRevision := 1
			if deleted {
				wantRevision = 2
			}
			if v.Revision != wantRevision {
				t.Fatal("revision lost across restart", v.Revision)
			}
			stored := savedSource(t, restarted, c.Id)
			if deleted && stored.GetString("private_key") != "" || !deleted && stored.GetString("private_key") != sshSource(0).PrivateKey {
				t.Fatal("credential persistence incorrect")
			}
		})
	}
}

func TestLinuxSourceMutationInvalidatesInflightReadAndControlConfirmation(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprint(deleted), func(t *testing.T) {
			app, mux, cid, token, admin := controlFixture(t, "super_admin")
			mutated := false
			linuxusers.Register(app, linuxReaderFunc(func(_ context.Context, _ linuxusers.Profile, r linuxusers.Request) (linuxusers.Snapshot, error) {
				if !mutated {
					mutated = true
					if deleted {
						sourceData(t, request(mux, "DELETE", sourcePath(cid), `{"revision":1}`, token))
					} else {
						replacement := localSource(1)
						replacement.ManageLocal = true
						sourceData(t, putSource(t, mux, cid, token, replacement))
					}
				}
				return linuxSnapshot(r), nil
			}))
			s := linuxusers.For(app)
			s.SetAdminProvider(admin)
			prepared := prepareCreate(t, mux, cid, token)
			r := request(mux, "GET", "/api/v1/clusters/"+cid+"/linux-users", "", token)
			if r.Code != 409 || strings.Contains(r.Body.String(), `"items"`) {
				t.Fatal(r.Code, r.Body.String())
			}
			input := fmt.Sprintf(`{"proof":%q,"confirmation":"worker","reason":"test invalidation"}`, prepared.Proof)
			r = request(mux, "POST", "/api/v1/clusters/"+cid+"/linux-user-controls/execute", input, token)
			if r.Code != 409 || admin.execute != 0 {
				t.Fatal("stale confirmation reached OS", r.Code, r.Body.String())
			}
		})
	}
}

func TestLinuxSourceLegacyImportOnceAndDatabaseWins(t *testing.T) {
	app, mux, a, person, token := sourceAdmin(t)
	b := linuxCluster(t, app, "legacy-only")
	c := linuxCluster(t, app, "deleted-source")
	sourceData(t, putSource(t, mux, a.Id, token, localSource(0)))
	sourceData(t, request(mux, "DELETE", sourcePath(c.Id), `{"revision":0}`, token))
	dir := t.TempDir()
	key, hosts, config := filepath.Join(dir, "key"), filepath.Join(dir, "hosts"), filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(key, []byte("IMPORTED-PRIVATE-CONTENT\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hosts, []byte("imported-host-content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{"clusters": map[string]any{
		a.Id: map[string]any{"label": "Should never replace DB", "transport": "ssh", "identityFile": "/missing/key", "knownHostsFile": "/missing/hosts"},
		b.Id: map[string]any{"label": "Imported identity", "transport": "ssh", "host": "192.0.2.9", "port": 22, "username": "inventory", "identityFile": key, "knownHostsFile": hosts},
		c.Id: map[string]any{"label": "Should never resurrect", "transport": "local", "manageLocal": true},
	}}
	raw, _ := json.Marshal(legacy)
	if err := os.WriteFile(config, raw, 0640); err != nil {
		t.Fatal(err)
	}
	if err := linuxusers.ImportLegacy(app, config); err != nil {
		t.Fatal(err)
	}
	if savedSource(t, app, a.Id).GetString("label") != localSource(0).Label || savedSource(t, app, c.Id).GetBool("configured") {
		t.Fatal("legacy import overrode database")
	}
	if savedSource(t, app, b.Id).GetString("private_key") != "IMPORTED-PRIVATE-CONTENT\n" {
		t.Fatal("legacy key not imported")
	}
	if linuxusers.For(app).ConfiguredCount() != 2 {
		t.Fatal("imported count incorrect")
	}
	for _, path := range []string{key, hosts, config} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	// A database deletion after import must remain deleted, even with the old environment.
	if _, err := linuxusers.For(app).DeleteSource(context.Background(), person.Id, b.Id, "delete-imported", 1); err != nil {
		t.Fatal(err)
	}
	dataDir := app.DataDir()
	if err := app.ResetBootstrapState(); err != nil {
		t.Fatal(err)
	}
	restarted := core.NewBaseApp(core.BaseAppConfig{DataDir: dataDir})
	if err := restarted.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.ResetBootstrapState() })
	if err := linuxusers.ImportLegacy(restarted, config); err != nil {
		t.Fatal("completed import still required removed legacy files", err)
	}
	v, err := linuxusers.For(restarted).GetSource(person.Id, b.Id)
	if err != nil || v.Configured || v.Revision != 2 {
		t.Fatal("deleted imported source resurrected", v, err)
	}
}

func TestLinuxSourceFailedLegacyImportRollsBackAndCanRetry(t *testing.T) {
	app, _, c, _, _ := sourceAdmin(t)
	dir := t.TempDir()
	key, hosts, config := filepath.Join(dir, "key"), filepath.Join(dir, "hosts"), filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(key, []byte("PRIVATE-IMPORT-SECRET"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hosts, []byte("host-key"), 0644); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{"clusters":{%q:{"label":"Import retry","transport":"ssh","host":"192.0.2.9","port":22,"username":"inventory","identityFile":%q,"knownHostsFile":%q}}}`, c.Id, key, hosts)
	if err := os.WriteFile(config, []byte(raw), 0640); err != nil {
		t.Fatal(err)
	}
	err := linuxusers.ImportLegacy(app, config)
	if err == nil || strings.Contains(err.Error(), "PRIVATE-IMPORT-SECRET") || strings.Contains(err.Error(), key) {
		t.Fatal("unsafe import accepted or leaked credential")
	}
	state, err := app.FindRecordById("linux_user_source_import", "linuxsources001")
	if err != nil || state.GetBool("completed") {
		t.Fatal("failed import marked complete", err)
	}
	if count, _ := app.CountRecords("linux_user_sources"); count != 0 {
		t.Fatal("failed import committed partial state")
	}
	if err := os.Chmod(key, 0600); err != nil {
		t.Fatal(err)
	}
	if err := linuxusers.ImportLegacy(app, config); err != nil {
		t.Fatal("corrected import could not retry", err)
	}
	if savedSource(t, app, c.Id).GetString("private_key") != "PRIVATE-IMPORT-SECRET" {
		t.Fatal("retry did not persist key")
	}
}

func TestLinuxSourceLegacyImportIsAtomicAndEmptyImportIsFinal(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			app, _, a, _, _ := sourceAdmin(t)
			if empty {
				if err := linuxusers.ImportLegacy(app, ""); err != nil {
					t.Fatal(err)
				}
				if err := linuxusers.ImportLegacy(app, "/removed/legacy.json"); err != nil {
					t.Fatal("empty import was not final", err)
				}
				return
			}
			b := linuxCluster(t, app, "second-import")
			config := filepath.Join(t.TempDir(), "legacy.json")
			raw := fmt.Sprintf(`{"clusters":{%q:{"label":"First","transport":"local"},%q:{"label":"Second","transport":"local"}}}`, a.Id, b.Id)
			if err := os.WriteFile(config, []byte(raw), 0640); err != nil {
				t.Fatal(err)
			}
			failSave, calls := true, 0
			app.OnRecordCreate("linux_user_sources").BindFunc(func(e *core.RecordEvent) error {
				calls++
				if failSave && calls == 2 {
					return errors.New("import storage unavailable")
				}
				return e.Next()
			})
			if err := linuxusers.ImportLegacy(app, config); err == nil {
				t.Fatal("partial import succeeded")
			}
			if count, _ := app.CountRecords("linux_user_sources"); count != 0 {
				t.Fatal("partial import was not rolled back")
			}
			state, err := app.FindRecordById("linux_user_source_import", "linuxsources001")
			if err != nil || state.GetBool("completed") {
				t.Fatal("partial import marked complete", err)
			}
			failSave = false
			if err := linuxusers.ImportLegacy(app, config); err != nil {
				t.Fatal(err)
			}
			if count, _ := app.CountRecords("linux_user_sources"); count != 2 {
				t.Fatal("retry lost sources", count)
			}
		})
	}
}

func TestLinuxSourceClusterScopeAndLocalPolicyBoundary(t *testing.T) {
	app, mux, a, person, token := sourceAdmin(t)
	b := linuxCluster(t, app, "forbidden-source")
	rows, err := app.FindRecordsByFilter("role_permissions", "role.code='super_admin' && permission.code='linux_users.read'", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if err := app.Delete(row); err != nil {
			t.Fatal(err)
		}
	}
	grant(t, app, person.Id, "viewer", a.Id)
	if r := request(mux, "GET", sourcePath(a.Id), "", token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		raw := `{"revision":0}`
		if method == "PUT" {
			encoded, _ := json.Marshal(localSource(0))
			raw = string(encoded)
		}
		if r := request(mux, method, sourcePath(b.Id), raw, token); r.Code != 403 {
			t.Fatal("cross-cluster configuration allowed", method, r.Code)
		}
		if r := request(mux, method, sourcePath(a.Id), raw, ""); r.Code != 401 {
			t.Fatal("anonymous source access", method, r.Code)
		}
	}
	// Enabling the database flag does not grant OS capabilities or bypass host policy.
	grant(t, app, person.Id, "cluster_admin", a.Id)
	input := localSource(0)
	input.ManageLocal = true
	sourceData(t, putSource(t, mux, a.Id, token, input))
	linuxusers.For(app).SetAdminProvider(sourceAdminFunc(func(context.Context, linuxusers.AdminRequest) (linuxusers.AdminResult, error) {
		return linuxusers.AdminResult{Allowed: false, Message: "Root-owned host policy is disabled."}, nil
	}))
	r := request(mux, "GET", "/api/v1/clusters/"+a.Id+"/linux-user-controls", "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"enabled":false`) || !strings.Contains(r.Body.String(), "Root-owned host policy") {
		t.Fatal("database flag bypassed root policy", r.Code, r.Body.String())
	}
}

type sourceAdminFunc func(context.Context, linuxusers.AdminRequest) (linuxusers.AdminResult, error)

func (f sourceAdminFunc) Run(ctx context.Context, r linuxusers.AdminRequest) (linuxusers.AdminResult, error) {
	return f(ctx, r)
}

func TestLinuxSourceDeleteAuditFailurePreservesSavedCredentials(t *testing.T) {
	app, mux, c, person, token := sourceAdmin(t)
	sourceData(t, putSource(t, mux, c.Id, token, sshSource(0)))
	app.OnRecordCreate("operation_audits").BindFunc(func(*core.RecordEvent) error { return errors.New(sshSource(0).PrivateKey) })
	_, err := linuxusers.For(app).DeleteSource(context.Background(), person.Id, c.Id, "failed-delete", 1)
	if err == nil || strings.Contains(err.Error(), "PRIVATE-IDENTITY-CONTENT") {
		t.Fatal("delete error leaked credentials")
	}
	stored := savedSource(t, app, c.Id)
	if !stored.GetBool("configured") || stored.GetInt("revision") != 1 || stored.GetString("private_key") != sshSource(0).PrivateKey {
		t.Fatal("failed delete removed credentials")
	}
	_, err = linuxusers.For(app).SaveSource(context.Background(), person.Id, c.Id, "failed-save", sshSource(1))
	if err == nil || strings.Contains(err.Error(), "PRIVATE-IDENTITY-CONTENT") {
		t.Fatal("save error leaked credentials")
	}
}
