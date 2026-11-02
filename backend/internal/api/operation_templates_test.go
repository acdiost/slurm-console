package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/jobcontrol"
	"slurm-console/backend/internal/services"
)

func templateSpec(name string, revision int) string {
	return fmt.Sprintf(`{"name":%q,"action":"jobs.cancel","description":"确认后取消明确作业","defaultReason":"测试取消原因","checklist":["已确认作业归属"],"revision":%d}`, name, revision)
}
func templateID(t *testing.T, mux http.Handler, base, token, name string) string {
	t.Helper()
	r := request(mux, "POST", base, templateSpec(name, 0), token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var b struct{ Data struct{ ID string } }
	_ = json.Unmarshal(r.Body.Bytes(), &b)
	return b.Data.ID
}
func TestOperationTemplatesOwnershipConcurrencyAndPreparation(t *testing.T) {
	app, mux := setup(t)
	var writes atomic.Int32
	var callback func()
	reg := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if callback != nil {
			x := callback
			callback = nil
			x()
		}
		if r.Method != "GET" {
			writes.Add(1)
			t.Error("template operation wrote Slurm")
		}
		fmt.Fprint(w, slurmEnvelope(cancelJobFields("PENDING", 1001)))
	})
	cfg, e := clusterconfig.Register(app, reg).Save("", validConnection())
	if e != nil {
		t.Fatal(e)
	}
	actor := user(t, app, "users")
	grant(t, app, actor.Id, "operator", cfg.ID)
	token := login(t, mux)
	if e := jobcontrol.LinkIdentity(app, actor.Id, cfg.ID, "alice", 1001, "own"); e != nil {
		t.Fatal(e)
	}
	base := "/api/v1/clusters/" + cfg.ID + "/operation-templates"
	id := templateID(t, mux, base, token, "取消验证")
	if r := request(mux, "POST", base, `{"name":"空清单","action":"jobs.cancel","description":"","defaultReason":"","checklist":[],"revision":0}`, token); r.Code != 200 {
		t.Fatal("empty checklist rejected", r.Code, r.Body.String())
	}
	if n, _ := app.CountRecords("operation_audits", nil); n == 0 {
		t.Fatal("template change not audited")
	}
	for _, in := range []string{strings.Replace(templateSpec("bad", 0), `"jobs.cancel"`, `"shell.run"`, 1), strings.Replace(templateSpec("bad", 0), `"name":`, `"owner":"other","name":`, 1), strings.Replace(templateSpec("bad", 0), `"name":`, `"targets":["42"],"name":`, 1)} {
		if r := request(mux, "POST", base, in, token); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	if r := request(mux, "POST", base, templateSpec("取消验证", 0), token); r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "PATCH", base+"/"+id, templateSpec("更名", 1), token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "PATCH", base+"/"+id, templateSpec("旧版本", 1), token); r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, in := range []string{`{"revision":1,"targets":["42"]}`, `{"revision":2,"targets":["42","42"]}`, `{"revision":2,"targets":["1-9"]}`} {
		r := request(mux, "POST", base+"/"+id+"/prepare", in, token)
		if r.Code != 400 && r.Code != 409 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	r := request(mux, "POST", base+"/"+id+"/prepare", `{"revision":2,"targets":["42"]}`, token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"allowed":true`) || strings.Contains(r.Body.String(), `"proof"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if writes.Load() != 0 {
		t.Fatal("prepare wrote")
	}
	// The same cluster and role never grant another user's template access.
	c, _ := app.FindCollectionByNameOrId("users")
	other := core.NewRecord(c)
	other.SetEmail("template-other@example.test")
	other.SetPassword(fixturePassword)
	other.Set("active", true)
	other.Set("auth_source", "local")
	other.Set("name", "Other")
	if e := app.Save(other); e != nil {
		t.Fatal(e)
	}
	grant(t, app, other.Id, "operator", cfg.ID)
	auth := request(mux, "POST", "/api/collections/users/auth-with-password", `{"identity":"template-other@example.test","password":"`+fixturePassword+`"}`, "")
	var authBody struct{ Token string }
	_ = json.Unmarshal(auth.Body.Bytes(), &authBody)
	if authBody.Token == "" {
		t.Fatal(auth.Code, auth.Body.String())
	}
	if r := request(mux, "GET", base, "", authBody.Token); r.Code != 200 || strings.Contains(r.Body.String(), "更名") {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "DELETE", base+"/"+id, `{"revision":2}`, authBody.Token); r.Code != 404 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "POST", base+"/"+id+"/prepare", `{"revision":2,"targets":["42"]}`, authBody.Token); r.Code != 404 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "GET", "/api/collections/operation_templates/records", "", token); r.Code != 403 && r.Code != 404 {
		t.Fatal("native collection open", r.Code)
	}
	// Changes while upstream state is read invalidate the template prepare response.
	services.SlurmFor(app).Invalidate(cfg.ID)
	callback = func() {
		v, _ := app.FindRecordById("operation_templates", id)
		v.Set("revision", 3)
		if e := app.Save(v); e != nil {
			t.Error(e)
		}
	}
	r = request(mux, "POST", base+"/"+id+"/prepare", `{"revision":2,"targets":["42"]}`, token)
	if r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	services.SlurmFor(app).Invalidate(cfg.ID)
	callback = func() {
		bindings, _ := app.FindRecordsByFilter("role_permissions", "permission.code='jobs.cancel'", "", 0, 0)
		for _, b := range bindings {
			if e := app.Delete(b); e != nil {
				t.Error(e)
			}
		}
	}
	r = request(mux, "POST", base+"/"+id+"/prepare", `{"revision":3,"targets":["42"]}`, token)
	if r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
	if writes.Load() != 0 {
		t.Fatal("template endpoint wrote Slurm")
	}
}
func TestOperationTemplatesQuotaAndAuditFailureRollback(t *testing.T) {
	app, mux, cid, token, _ := accountingSetup(t, "operator")
	base := "/api/v1/clusters/" + cid + "/operation-templates"
	for i := 0; i < 20; i++ {
		templateID(t, mux, base, token, fmt.Sprintf("模板 %02d", i))
	}
	if r := request(mux, "POST", base, templateSpec("超额", 0), token); r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	rows, _ := app.FindRecordsByFilter("operation_templates", "", "", 1, 0)
	id := rows[0].Id
	if r := request(mux, "DELETE", base+"/"+id, `{"revision":2}`, token); r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "DELETE", base+"/"+id, `{"revision":1}`, token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error { return fmt.Errorf("audit unavailable") })
	if r := request(mux, "POST", base, templateSpec("审计回滚", 0), token); r.Code != 500 {
		t.Fatal(r.Code, r.Body.String())
	}
	if n, _ := app.CountRecords("operation_templates", nil); n != 19 {
		t.Fatal("template persisted without audit", n)
	}
}
func TestOperationTemplatesViewerCannotManage(t *testing.T) {
	_, mux, cid, token, _ := accountingSetup(t, "viewer")
	base := "/api/v1/clusters/" + cid + "/operation-templates"
	for _, method := range []string{"GET", "POST"} {
		if r := request(mux, method, base, templateSpec("不可创建", 0), token); r.Code != 403 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
}
