package api_test

import (
	"encoding/json"
	"github.com/pocketbase/pocketbase/core"
	"net/http"
	"slurm-console/backend/internal/accounting"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"strings"
	"testing"
)

func policySetup(t *testing.T, role string) (core.App, http.Handler, string, string, *accountingFixture) {
	app, mux, cid, token, f := accountingSetup(t, role)
	q := func(id int, name string) map[string]any {
		return map[string]any{"id": id, "name": name, "description": "policy " + name, "flags": []string{}, "priority": map[string]any{"set": true, "number": 0}, "usage_factor": map[string]any{"set": true, "number": 1}, "limits": map[string]any{"max": map[string]any{"jobs": map[string]any{"active_jobs": map[string]any{"per": map[string]any{"user": map[string]any{"set": false, "infinite": true}}}, "per": map[string]any{"user": map[string]any{"set": false, "infinite": true}}}, "wall_clock": map[string]any{"per": map[string]any{"job": map[string]any{"set": false, "infinite": true}}}}}, "preempt": map[string]any{"mode": []string{"DISABLED"}}}
	}
	f.qos = []map[string]any{q(1, "normal"), q(2, "foreign-secret")}
	for _, a := range f.assocs {
		a["qos"] = []string{"normal"}
		if a["cluster"] == "foreign" {
			a["qos"] = []string{"foreign-secret"}
		}
	}
	f.shares = `"shares":{"shares":[{"id":4,"cluster":"fixture","name":"alice","parent":"research","partition":"","type":["USER"],"shares":{"set":true,"number":0},"shares_normalized":{"set":true,"number":0.00000001},"usage":18446744073709551615,"usage_normalized":{"set":false,"number":0},"effective_usage":{"set":true,"number":0},"fairshare":{"factor":{"set":true,"number":0},"level":{"set":false,"infinite":true}}},{"id":7,"cluster":"foreign","name":"hidden-user","type":["USER"],"usage":10},{"id":999,"cluster":"fixture","name":"not-associated","type":["USER"],"usage":10}]}`
	return app, mux, cid, token, f
}
func qosInput() accounting.Spec {
	return accounting.Spec{QOS: &client.QOSSpec{Name: "test-policy", Description: "isolated policy", Priority: 7, UsageFactor: 1.5}}
}

func TestQOSCatalogScopeAndFairsharePrecision(t *testing.T) {
	_, mux, cid, token, _ := policySetup(t, "viewer")
	base := "/api/v1/clusters/" + cid
	r := request(mux, "GET", base+"/qos", "", token)
	if r.Code != 200 || strings.Contains(r.Body.String(), "foreign-secret") || !strings.Contains(r.Body.String(), `"catalogScope":"referenced"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(mux, "GET", base+"/qos/foreign-secret", "", token); r.Code != 404 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request(mux, "GET", base+"/fairshare?kind=user&account=research&sort=factor&order=desc", "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"rawUsage":"18446744073709551615"`) || !strings.Contains(r.Body.String(), `"value":0`) || strings.Contains(r.Body.String(), "hidden-user") || strings.Contains(r.Body.String(), "not-associated") {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, path := range []string{"/qos?sort=unknown", "/qos?with_deleted=true", "/fairshare?kind=wrong", "/fairshare?sort=usage", "/fairshare?cluster=foreign"} {
		if r := request(mux, "GET", base+path, "", token); r.Code != 400 {
			t.Fatal(path, r.Code, r.Body.String())
		}
	}
	r = request(mux, "POST", base+"/accounting-controls/prepare", payload(t, map[string]any{"action": "qos_create", "spec": qosInput()}), token)
	if r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestQOSCreateUpdateAuditAndReplay(t *testing.T) {
	app, mux, cid, token, f := policySetup(t, "super_admin")
	base := "/api/v1/clusters/" + cid
	r := request(mux, "GET", base+"/qos?sort=priority&order=desc", "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "foreign-secret") || !strings.Contains(r.Body.String(), `"catalogScope":"global"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	p := accountingPrepare(t, mux, cid, token, "qos_create", qosInput())
	v := accountingExecute(t, mux, cid, token, p)
	if v.Status != "confirmed" || v.Spec.QOS == nil || f.writes.Load() != 1 {
		t.Fatal(v, f.writes.Load())
	}
	if repeat := accountingExecute(t, mux, cid, token, p); repeat.OperationID != v.OperationID || f.writes.Load() != 1 {
		t.Fatal(repeat)
	}
	var added struct {
		Data struct{ Item struct{ ID string } }
	}
	r = request(mux, "GET", base+"/qos/test-policy", "", token)
	_ = json.Unmarshal(r.Body.Bytes(), &added)
	if r.Code != 200 || added.Data.Item.ID == "" {
		t.Fatal(r.Code, r.Body.String())
	}
	spec := qosInput()
	spec.QOS.ID = added.Data.Item.ID
	spec.QOS.Priority = 0
	zero := uint32(0)
	spec.QOS.MaxJobsPerUser = &zero
	p = accountingPrepare(t, mux, cid, token, "qos_update", spec)
	v = accountingExecute(t, mux, cid, token, p)
	if v.Status != "confirmed" || f.writes.Load() != 2 {
		t.Fatal(v)
	}
	r = request(mux, "GET", base+"/qos-operations", "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "qos_update") {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request(mux, "GET", base+"/accounting-operations", "", token)
	if r.Code != 200 || strings.Contains(r.Body.String(), "qos_update") {
		t.Fatal(r.Code, r.Body.String())
	}
	count, err := app.CountRecords("operation_audits")
	if err != nil || count < 2 {
		t.Fatal(count, err)
	}
}

func TestQOSRejectsChangedAdvancedPolicyAndRevokedGlobalPermission(t *testing.T) {
	for _, mode := range []string{"advanced", "permission"} {
		t.Run(mode, func(t *testing.T) {
			app, mux, cid, token, f := policySetup(t, "super_admin")
			spec := qosInput()
			spec.QOS.Name = "normal"
			spec.QOS.ID = "1"
			p := accountingPrepare(t, mux, cid, token, "qos_update", spec)
			if mode == "advanced" {
				f.mu.Lock()
				f.qos[0]["unexposed_setting"] = 17
				f.mu.Unlock()
			} else {
				permission, _ := app.FindFirstRecordByData("permissions", "code", "platform.qos.manage")
				bindings, _ := app.FindRecordsByFilter("role_permissions", "permission={:p}", "", 0, 0, map[string]any{"p": permission.Id})
				for _, binding := range bindings {
					if err := app.Delete(binding); err != nil {
						t.Fatal(err)
					}
				}
			}
			r := request(mux, "POST", "/api/v1/clusters/"+cid+"/accounting-controls/execute", payload(t, accounting.Input{Proof: p.Proof, Confirmation: p.Confirmation, Reason: "policy check"}), token)
			if r.Code != 409 && r.Code != 403 {
				t.Fatal(r.Code, r.Body.String())
			}
			if f.writes.Load() != 0 {
				t.Fatal("unexpected write")
			}
		})
	}
}

func TestQOSUnknownWriteReconcilesWithoutRetry(t *testing.T) {
	app, mux, cid, token, f := policySetup(t, "super_admin")
	f.loseReply = true
	f.failReadback = true
	p := accountingPrepare(t, mux, cid, token, "qos_create", qosInput())
	v := accountingExecute(t, mux, cid, token, p)
	if v.Status != "unknown" || f.writes.Load() != 1 {
		t.Fatal(v)
	}
	app.Store().Set("platform.accounting", nil)
	r := request(mux, "GET", "/api/v1/clusters/"+cid+"/accounting-operations/"+v.OperationID, "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"confirmed"`) || f.writes.Load() != 1 {
		t.Fatal(r.Code, r.Body.String())
	}
	services.SlurmFor(app).Invalidate(cid)
}

func TestQOSUpdatePreservesPreemptionAndUneditedLimits(t *testing.T) {
	_, mux, cid, token, f := policySetup(t, "super_admin")
	f.qos[0]["preempt"] = map[string]any{"mode": []string{"REQUEUE"}, "exempt_time": map[string]any{"set": true, "number": 30}}
	f.qos[0]["flags"] = []string{"DENY_LIMIT"}
	f.qos[0]["limits"].(map[string]any)["max"].(map[string]any)["tres"] = map[string]any{"total": []any{map[string]any{"type": "cpu", "count": 2}}}
	spec := qosInput()
	spec.QOS.Name = "normal"
	spec.QOS.ID = "1"
	p := accountingPrepare(t, mux, cid, token, "qos_update", spec)
	v := accountingExecute(t, mux, cid, token, p)
	if v.Status != "confirmed" || f.writes.Load() != 1 {
		t.Fatal(v)
	}
	encoded, _ := json.Marshal(f.qos[0])
	if !strings.Contains(string(encoded), "REQUEUE") || !strings.Contains(string(encoded), "DENY_LIMIT") || !strings.Contains(string(encoded), `"count":2`) {
		t.Fatal(string(encoded))
	}
}

func TestQOSGlobalReadRevocationDuringQueryFiltersCatalog(t *testing.T) {
	app, mux, cid, token, f := policySetup(t, "super_admin")
	f.onRead = func() {
		permission, _ := app.FindFirstRecordByData("permissions", "code", "platform.qos.read")
		bindings, _ := app.FindRecordsByFilter("role_permissions", "permission={:p}", "", 0, 0, map[string]any{"p": permission.Id})
		for _, binding := range bindings {
			if err := app.Delete(binding); err != nil {
				t.Fatal(err)
			}
		}
	}
	r := request(mux, "GET", "/api/v1/clusters/"+cid+"/qos", "", token)
	if r.Code != 200 || strings.Contains(r.Body.String(), "foreign-secret") || !strings.Contains(r.Body.String(), `"catalogScope":"referenced"`) {
		t.Fatal(r.Code, r.Body.String())
	}
}
