package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/aiassist"
	"slurm-console/backend/internal/platformsettings"
)

func settingsActor(t *testing.T) (core.App, http.Handler, string) {
	t.Helper()
	app, mux := setup(t)
	u := user(t, app, "users")
	grant(t, app, u.Id, "super_admin", "")
	return app, mux, login(t, mux)
}
func identityBody(revision int, name string) string {
	return payloadNoFail(map[string]any{"name": name, "description": "平台说明", "loginNotice": "公告 <script>alert(1)</script>", "supportContact": "管理员联系信息", "revision": revision})
}
func payloadNoFail(v any) string { raw, _ := json.Marshal(v); return string(raw) }
func TestPlatformPublicIdentityAndTransactionalAudit(t *testing.T) {
	app, mux, token := settingsActor(t)
	r := request(mux, "GET", "/api/v1/platform-info", "", "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "Slurm Console") || strings.Contains(r.Body.String(), "revision") {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(mux, "PATCH", "/api/v1/platform-settings", identityBody(1, "科研平台"), token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request(mux, "GET", "/api/v1/platform-info", "", "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "科研平台") || !strings.Contains(r.Body.String(), "公告") {
		t.Fatal(r.Code, r.Body.String())
	}
	row, e := app.FindFirstRecordByData("operation_audits", "action", "platform.settings.update")
	if e != nil || row.GetString("actor_id") == "" || row.GetString("request_id") == "" || row.GetString("target_id") != platformsettings.ID {
		t.Fatal(row, e)
	}
	if r = request(mux, "PATCH", "/api/v1/platform-settings", identityBody(1, "旧修订"), token); r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString("action") == "platform.settings.update" {
			return errors.New("synthetic audit failure")
		}
		return e.Next()
	})
	if r = request(mux, "PATCH", "/api/v1/platform-settings", identityBody(2, "不应保存"), token); r.Code != 503 {
		t.Fatal(r.Code, r.Body.String())
	}
	v, e := platformsettings.Read(app)
	if e != nil || v.Name != "科研平台" || v.Revision != 2 {
		t.Fatal(v, e)
	}
}
func TestPlatformSettingsPermissionsStrictBodyAndNativeLock(t *testing.T) {
	app, mux, token := settingsActor(t)
	for _, body := range []string{`null`, `{}`, `{"name":"科研平台"}`, strings.TrimSuffix(identityBody(1, "科研平台"), "}") + `,"name":"重复"}`, strings.TrimSuffix(identityBody(1, "科研平台"), "}") + `,"smtpPassword":"PRIVATE"}`, strings.Replace(identityBody(1, "科研平台"), `"supportContact":"管理员联系信息"`, `"supportContact":null`, 1), identityBody(1, "x"), identityBody(1, strings.Repeat("长", 65))} {
		r := request(mux, "PATCH", "/api/v1/platform-settings", body, token)
		if r.Code != 400 {
			t.Fatal(r.Code, r.Body.String(), body)
		}
	}
	for _, path := range []string{"/api/v1/platform-info?q=x", "/api/v1/platform-settings?q=x", "/api/v1/platform-settings/runtime?q=x"} {
		if r := request(mux, "GET", path, "", token); r.Code != 400 {
			t.Fatal(r.Code, path)
		}
	}
	for _, path := range []string{"/api/v1/platform-settings", "/api/v1/platform-settings/ai", "/api/v1/platform-settings/runtime", "/api/v1/platform-users", "/api/v1/platform-role-catalog"} {
		if r := request(mux, "GET", path, "", ""); r.Code != 401 {
			t.Fatal(r.Code, path)
		}
	}
	if r := request(mux, "GET", "/api/collections/platform_settings/records", "", token); r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
	rows, _ := app.FindRecordsByFilter("role_permissions", "permission.code~'platform.settings.'", "", 0, 0)
	for _, row := range rows {
		role, _ := app.FindRecordById("roles", row.GetString("role"))
		if role.GetString("code") != "super_admin" {
			t.Fatal("cluster role inherited settings")
		}
		if e := app.Delete(row); e != nil {
			t.Fatal(e)
		}
	}
	if r := request(mux, "GET", "/api/v1/platform-settings", "", token); r.Code != 403 {
		t.Fatal(r.Code)
	}
	if r := request(mux, "PATCH", "/api/v1/platform-settings", identityBody(1, "科研平台"), token); r.Code != 403 {
		t.Fatal(r.Code)
	}
}
func TestPlatformAISettingsNeverExposeCredentialsAndSaveDoesNotCallModel(t *testing.T) {
	app, mux, cid, token, calls, _ := aiFixture(t)
	// The test actor needs explicit platform settings permissions.
	actor, _ := app.FindAuthRecordByEmail("users", "fixture@example.com")
	grant(t, app, actor.Id, "super_admin", "")
	old := aiassist.For(app).Destination()
	app.Store().Set("platform.aiBase", aiassist.Config{Enabled: true, Label: old.Label, Endpoint: old.Endpoint, Model: old.Model, ClusterIDs: []string{cid}})
	preview := aiPrepare(t, mux, cid, token)
	cfg := platformsettings.AI{Enabled: false, Label: old.Label, Endpoint: old.Endpoint, Model: old.Model, ClusterIDs: []string{cid}, TimeoutSeconds: 30, MaxOutputTokens: 2048, TokenParameter: "max_completion_tokens"}
	r := request(mux, "PATCH", "/api/v1/platform-settings/ai", payloadNoFail(map[string]any{"config": cfg, "revision": 1, "confirmation": "APPLY AI"}), token)
	if r.Code != 200 || calls.Load() != 0 || aiassist.For(app).Enabled(cid) {
		t.Fatal(r.Code, r.Body.String(), calls.Load())
	}
	if r = aiSend(mux, cid, token, preview); r.Code != 409 || calls.Load() != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	cfg.Enabled = true
	if r = request(mux, "PATCH", "/api/v1/platform-settings/ai", payloadNoFail(map[string]any{"config": cfg, "revision": 2, "confirmation": "APPLY AI"}), token); r.Code != 200 || !aiassist.For(app).Enabled(cid) || calls.Load() != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	aiassist.Register(app, nil)
	if e := platformsettings.ApplyAI(app); e != nil || !aiassist.For(app).Enabled(cid) {
		t.Fatal("persisted AI configuration not restored", e)
	}
	var config map[string]any
	data, _ := json.Marshal(cfg)
	json.Unmarshal(data, &config)
	config["apiKeyFile"] = "/PRIVATE/PATH"
	if r = request(mux, "PATCH", "/api/v1/platform-settings/ai", payloadNoFail(map[string]any{"config": config, "revision": 3, "confirmation": "APPLY AI"}), token); r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	cfg.Enabled = false
	cfg.Endpoint = "https://model.example.test/v1/chat/completions"
	if r = request(mux, "PATCH", "/api/v1/platform-settings/ai", payloadNoFail(map[string]any{"config": cfg, "revision": 3, "confirmation": "APPLY AI"}), token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	app.Store().Set("platform.aiBase", aiassist.Config{APIKeyFile: "/PRIVATE/SECRET/key", CAFile: "/PRIVATE/SECRET/ca"})
	for _, path := range []string{"/api/v1/platform-info", "/api/v1/platform-settings/ai", "/api/v1/platform-settings/runtime"} {
		r = request(mux, "GET", path, "", token)
		if r.Code != 200 || strings.Contains(r.Body.String(), "PRIVATE") || strings.Contains(r.Body.String(), "apiKeyFile") || strings.Contains(r.Body.String(), "caFile") {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	rows, _ := app.FindRecordsByFilter("operation_audits", "action='platform.ai.update'", "", 0, 0)
	if len(rows) != 3 {
		t.Fatal(len(rows))
	}
	for _, row := range rows {
		data, _ := json.Marshal(row)
		if strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), "apiKeyFile") {
			t.Fatal("credential reference persisted")
		}
	}
}
func TestPlatformAIChangesDuringRequestDiscardOldAnswer(t *testing.T) {
	app, mux, cid, token, calls, callback := aiFixture(t)
	actor, _ := app.FindAuthRecordByEmail("users", "fixture@example.com")
	grant(t, app, actor.Id, "super_admin", "")
	d := aiassist.For(app).Destination()
	app.Store().Set("platform.aiBase", aiassist.Config{Enabled: true, Label: d.Label, Endpoint: d.Endpoint, Model: d.Model, ClusterIDs: []string{cid}})
	p := aiPrepare(t, mux, cid, token)
	*callback = func() {
		cfg := platformsettings.AI{Enabled: false, Label: d.Label, Endpoint: d.Endpoint, Model: d.Model, ClusterIDs: []string{cid}, TimeoutSeconds: 30, MaxOutputTokens: 2048, TokenParameter: "max_completion_tokens"}
		r := request(mux, "PATCH", "/api/v1/platform-settings/ai", payloadNoFail(map[string]any{"config": cfg, "revision": 1, "confirmation": "APPLY AI"}), token)
		if r.Code != 200 {
			t.Error(r.Code, r.Body.String())
		}
	}
	r := aiSend(mux, cid, token, p)
	if r.Code != 503 || calls.Load() != 1 || strings.Contains(r.Body.String(), aiResponse) {
		t.Fatal(r.Code, r.Body.String())
	}
	row, e := app.FindFirstRecordByData("operation_audits", "action", "ai.explain")
	if e != nil || row.GetString("status") != "unknown" {
		t.Fatal(row, e)
	}
}

func TestDisabledAIDraftRejectsCredentialURLsBeforePersistence(t *testing.T) {
	app, mux, token := settingsActor(t)
	for _, endpoint := range []string{"https://user:PRIVATE@example.test/v1/chat/completions", "https://example.test/v1/chat/completions?api_key=PRIVATE", "https://example.test/v1/chat/completions#PRIVATE", "http://external.example.test/v1/chat/completions"} {
		cfg := platformsettings.AI{Label: "停用草稿", Endpoint: endpoint, Model: "fixture", ClusterIDs: []string{}, TimeoutSeconds: 30, MaxOutputTokens: 2048, TokenParameter: "max_completion_tokens"}
		r := request(mux, "PATCH", "/api/v1/platform-settings/ai", payloadNoFail(map[string]any{"config": cfg, "revision": 1, "confirmation": "APPLY AI"}), token)
		if r.Code != 400 || strings.Contains(r.Body.String(), "PRIVATE") {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	r, _ := app.FindRecordById("platform_settings", platformsettings.ID)
	if r.GetInt("revision") != 1 || r.GetString("ai_override") != "" && r.GetString("ai_override") != "null" {
		t.Fatal("invalid draft persisted")
	}
	n, _ := app.CountRecords("operation_audits")
	if n != 0 {
		t.Fatal("invalid draft was audited")
	}
}
