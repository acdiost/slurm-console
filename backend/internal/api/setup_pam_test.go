package api_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/auth"
)

func TestPAMSetupRequiresSettingsManagementAndStrictInput(t *testing.T) {
	app, mux := setup(t)
	person := user(t, app, "users")
	token := login(t, mux)
	for _, endpoint := range []struct{ method, path, body string }{
		{"POST", "/api/v1/setup/pam/check", `{}`},
		{"POST", "/api/v1/setup/pam/configure", `{"revision":1}`},
		{"PATCH", "/api/v1/setup/pam", `{"enabled":false,"revision":1}`},
	} {
		if result := request(mux, endpoint.method, endpoint.path, endpoint.body, ""); result.Code != 401 {
			t.Fatal("anonymous setup access", result.Code, result.Body.String())
		}
		if result := request(mux, endpoint.method, endpoint.path, endpoint.body, token); result.Code != 403 {
			t.Fatal("ordinary user accessed setup", result.Code, result.Body.String())
		}
	}
	grant(t, app, person.Id, "super_admin", "")
	for _, body := range []string{`{}`, `null`, `{"enabled":true}`, `{"enabled":false,"revision":1,"helper":"/PRIVATE/path"}`, `{"enabled":false,"enabled":true,"revision":1}`, `{"enabled":null,"revision":1}`, `{"enabled":false,"revision":0}`} {
		result := request(mux, "PATCH", "/api/v1/setup/pam", body, token)
		if result.Code != 400 || strings.Contains(result.Body.String(), "PRIVATE") {
			t.Fatal("invalid PAM body accepted or disclosed", result.Code, result.Body.String())
		}
	}
	if result := request(mux, "POST", "/api/v1/setup/pam/check", `{"helper":"/PRIVATE/path"}`, token); result.Code != 400 {
		t.Fatal("check accepted executable input", result.Code, result.Body.String())
	}
	for _, body := range []string{`{}`, `null`, `{"revision":0}`, `{"revision":null}`, `{"revision":1,"helper":"/PRIVATE/path"}`, `{"revision":1,"revision":2}`, `{"revision":1,"enabled":true}`} {
		result := request(mux, "POST", "/api/v1/setup/pam/configure", body, token)
		if result.Code != 400 || strings.Contains(result.Body.String(), "PRIVATE") {
			t.Fatal("invalid configure request accepted or disclosed", result.Code, result.Body.String())
		}
	}
	if result := request(mux, "GET", "/api/collections/pam_settings/records", "", token); result.Code != 403 {
		t.Fatal("native API exposed PAM settings", result.Code, result.Body.String())
	}
}

func TestPAMSetupMissingPrerequisitesAreActionableAndNeverSaved(t *testing.T) {
	t.Setenv("SLURM_CONSOLE_PAM_HELPER", filepath.Join(t.TempDir(), "PRIVATE-helper-missing"))
	app, mux, token := settingsActor(t)
	checked := request(mux, "POST", "/api/v1/setup/pam/check", `{}`, token)
	var readiness struct {
		Data auth.PAMStatus `json:"data"`
	}
	if checked.Code != 200 || json.Unmarshal(checked.Body.Bytes(), &readiness) != nil || readiness.Data.Available || readiness.Data.Configured || readiness.Data.HelperInstalled || readiness.Data.Revision != 1 || strings.Contains(checked.Body.String(), "PRIVATE") {
		t.Fatal("check did not return safe unavailable readiness", checked.Code, checked.Body.String())
	}
	result := request(mux, "PATCH", "/api/v1/setup/pam", `{"enabled":true,"revision":1}`, token)
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if result.Code != 503 || json.Unmarshal(result.Body.Bytes(), &failure) != nil || failure.Error.Code != "PAM_UNAVAILABLE" || strings.Contains(result.Body.String(), "PRIVATE") {
		t.Fatal("missing prerequisites were enabled or disclosed", result.Code, result.Body.String())
	}
	record, err := app.FindRecordById("pam_settings", auth.PAMSettingsID)
	if err != nil || record.GetBool("has_override") || record.GetInt("revision") != 1 {
		t.Fatal("missing helper persisted enabled choice", err)
	}
	if count, _ := app.CountRecords("operation_audits", nil); count != 1 {
		t.Fatal("failed check was not audited or failed enable was audited as success", count)
	}
	rows, _ := app.FindRecordsByFilter("operation_audits", "action='platform.pam.check'", "", 0, 0)
	for _, row := range rows {
		raw, _ := json.Marshal(row)
		if strings.Contains(string(raw), "PRIVATE") {
			t.Fatal("PAM helper reference disclosed in audit")
		}
	}
	login(t, mux)
}

func TestPAMSetupDisablePersistsAndAuditFailureRollsBack(t *testing.T) {
	t.Setenv("SLURM_CONSOLE_PAM_ENABLED", "true")
	t.Setenv("SLURM_CONSOLE_PAM_HELPER", filepath.Join(t.TempDir(), "missing"))
	app, mux, token := settingsActor(t)
	result := request(mux, "PATCH", "/api/v1/setup/pam", `{"enabled":false,"revision":1}`, token)
	if result.Code != 200 || !strings.Contains(result.Body.String(), `"configured":false`) || !strings.Contains(result.Body.String(), `"revision":2`) {
		t.Fatal("disable did not persist without installed helper", result.Code, result.Body.String())
	}
	if err := auth.ApplyPAMSetup(app); err != nil {
		t.Fatal("saved override did not supersede enabled environment", err)
	}
	row, err := app.FindFirstRecordByData("operation_audits", "action", "platform.pam.update")
	if err != nil || row.GetString("actor_id") == "" || row.GetString("request_id") == "" || row.GetString("target_id") != auth.PAMSettingsID {
		t.Fatal("missing PAM audit provenance", err)
	}
	if result = request(mux, "PATCH", "/api/v1/setup/pam", `{"enabled":false,"revision":1}`, token); result.Code != 409 {
		t.Fatal("stale revision accepted", result.Code, result.Body.String())
	}
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString("action") == "platform.pam.update" {
			return errors.New("PRIVATE-audit-failure")
		}
		return e.Next()
	})
	if result = request(mux, "PATCH", "/api/v1/setup/pam", `{"enabled":false,"revision":2}`, token); result.Code != 503 || strings.Contains(result.Body.String(), "PRIVATE") {
		t.Fatal("audit failure ignored or disclosed", result.Code, result.Body.String())
	}
	record, err := app.FindRecordById("pam_settings", auth.PAMSettingsID)
	if err != nil || record.GetInt("revision") != 2 || !record.GetBool("has_override") || record.GetBool("enabled") {
		t.Fatal("audit failure persisted PAM state", err)
	}
	login(t, mux)
}

func TestPAMConfigureRejectsStaleRevisionBeforeHostPreparation(t *testing.T) {
	t.Setenv("SLURM_CONSOLE_PAM_HELPER", filepath.Join(t.TempDir(), "PRIVATE-missing-helper"))
	app, mux, token := settingsActor(t)
	result := request(mux, "POST", "/api/v1/setup/pam/configure", `{"revision":2}`, token)
	if result.Code != 409 || strings.Contains(result.Body.String(), "PRIVATE") {
		t.Fatal("configure did not reject stale revision before checking host", result.Code, result.Body.String())
	}
	result = request(mux, "POST", "/api/v1/setup/pam/configure", `{"revision":1}`, token)
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if result.Code != 503 || json.Unmarshal(result.Body.Bytes(), &failure) != nil || failure.Error.Code != "PAM_UNAVAILABLE" || strings.Contains(result.Body.String(), "PRIVATE") || strings.Contains(result.Body.String(), "make ") {
		t.Fatal("configure unavailable response is not actionable/safe", result.Code, result.Body.String())
	}
	record, err := app.FindRecordById("pam_settings", auth.PAMSettingsID)
	if err != nil || record.GetInt("revision") != 1 || record.GetBool("has_override") {
		t.Fatal("failed configure persisted settings", err)
	}
	if count, _ := app.CountRecords("operation_audits"); count != 0 {
		t.Fatal("failed configure retained success audit")
	}
}

func TestPAMSetupRechecksManagementPrivilegeAtCommit(t *testing.T) {
	app, mux, token := settingsActor(t)
	app.OnRecordUpdate("pam_settings").BindFunc(func(e *core.RecordEvent) error {
		rows, err := e.App.FindRecordsByFilter("role_permissions", "permission.code='platform.settings.manage'", "", 0, 0)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := e.App.Delete(row); err != nil {
				return err
			}
		}
		return e.Next()
	})
	result := request(mux, "PATCH", "/api/v1/setup/pam", `{"enabled":false,"revision":1}`, token)
	if result.Code != 403 {
		t.Fatal("revoked management privilege committed", result.Code, result.Body.String())
	}
	record, err := app.FindRecordById("pam_settings", auth.PAMSettingsID)
	if err != nil || record.GetInt("revision") != 1 || record.GetBool("has_override") {
		t.Fatal("revoked privilege changed PAM state", err)
	}
}

func TestPAMProviderReplacementDoesNotBindDuplicateAuthHooks(t *testing.T) {
	app, mux := setup(t)
	person := user(t, app, "users")
	person.Set("auth_source", "pam")
	person.Set("username", "pam_fixture")
	if err := app.Save(person); err != nil {
		t.Fatal(err)
	}
	previous, provider := &pamFixture{}, &pamFixture{}
	auth.RegisterPAM(app, previous)
	auth.RegisterPAM(app, nil)
	auth.RegisterPAM(app, provider)
	auth.RegisterPAM(app, provider)
	token, err := person.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	result := request(mux, "POST", "/api/collections/users/auth-refresh", `{}`, token)
	if result.Code != 200 || len(previous.calls) != 0 || len(provider.calls) != 1 || provider.calls[0].Mode != "account" {
		t.Fatalf("provider replacement duplicated auth check: status=%d, calls=%d, previous=%d, body=%s", result.Code, len(provider.calls), len(previous.calls), result.Body.String())
	}
}
