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

func tresSetup(t *testing.T, role string) (core.App, http.Handler, string, string, *accountingFixture) {
	app, mux, cid, token, f := policySetup(t, role)
	f.tres = []map[string]any{{"id": 1, "type": "cpu", "name": ""}, {"id": 2, "type": "mem", "name": ""}, {"id": 1001, "type": "gres", "name": "gpu:a100"}}
	entries := func() []any {
		return []any{map[string]any{"id": 1, "type": "cpu", "name": "", "count": 2}, map[string]any{"id": 2, "type": "mem", "name": "", "count": 512}}
	}
	f.qos[0]["limits"].(map[string]any)["max"].(map[string]any)["tres"] = map[string]any{"total": entries()}
	f.qos[0]["preempt"] = map[string]any{"mode": []string{"REQUEUE"}}
	for _, a := range f.assocs {
		a["partition"] = ""
		a["max"] = map[string]any{"tres": map[string]any{"total": entries()}}
	}
	return app, mux, cid, token, f
}
func quotaSpec(kind string) *client.TRESSpec {
	v := &client.TRESSpec{Kind: kind, TargetID: "4", Account: "research", User: "alice", ResourceID: "1", Bucket: "group_total", Mode: "set", Value: "3"}
	if kind == "qos" {
		v.TargetID = "1"
		v.Name = "normal"
		v.Account = ""
		v.User = ""
	}
	return v
}
func TestTRESReadScopeCatalogAndValidation(t *testing.T) {
	_, mux, cid, token, _ := tresSetup(t, "viewer")
	base := "/api/v1/clusters/" + cid
	for _, path := range []string{"/tres", "/tres-limits?target=association", "/tres-limits?target=qos", "/tres-limits/association/4"} {
		r := request(mux, "GET", base+path, "", token)
		if r.Code != 200 || strings.Contains(r.Body.String(), "hidden-user") || strings.Contains(r.Body.String(), "foreign-secret") {
			t.Fatal(path, r.Code, r.Body.String())
		}
	}
	for _, path := range []string{"/tres-limits/association/7", "/tres-limits/qos/2"} {
		if r := request(mux, "GET", base+path, "", token); r.Code != 404 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	for _, path := range []string{"/tres?arbitrary=1", "/tres-limits?target=other", "/tres-limits?target=qos&sort=other"} {
		if r := request(mux, "GET", base+path, "", token); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	r := request(mux, "POST", base+"/accounting-controls/prepare", payload(t, map[string]any{"action": "association_tres_update", "spec": accounting.Spec{TRES: quotaSpec("association")}}), token)
	if r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
}
func TestTRESUpdateClearPreservesOtherResourcesAndPolicies(t *testing.T) {
	for _, kind := range []string{"association", "qos"} {
		t.Run(kind, func(t *testing.T) {
			_, mux, cid, token, f := tresSetup(t, "super_admin")
			v := quotaSpec(kind)
			action := "association_tres_update"
			if kind == "qos" {
				action = "qos_tres_update"
			}
			for _, step := range []struct{ Mode, Value string }{{"set", "3"}, {"set", "0"}, {"set", "9007199254740993"}, {"clear", ""}} {
				v.Mode, v.Value = step.Mode, step.Value
				p := accountingPrepare(t, mux, cid, token, action, accounting.Spec{TRES: v})
				result := accountingExecute(t, mux, cid, token, p)
				if result.Status != "confirmed" {
					t.Fatal(result)
				}
				writes := f.writes.Load()
				accountingExecute(t, mux, cid, token, p)
				if f.writes.Load() != writes {
					t.Fatal("write replayed")
				}
				r := request(mux, "GET", "/api/v1/clusters/"+cid+"/tres-limits/"+kind+"/"+v.TargetID, "", token)
				if r.Code != 200 || !strings.Contains(r.Body.String(), `"value":"512"`) {
					t.Fatal("memory lost", r.Code, r.Body.String())
				}
				if step.Mode == "set" && !strings.Contains(r.Body.String(), `"value":"`+step.Value+`"`) {
					t.Fatal("value lost precision", r.Body.String())
				}
			}
			if kind == "qos" {
				encoded, _ := json.Marshal(f.qos[0])
				if !strings.Contains(string(encoded), "REQUEUE") {
					t.Fatal("preemption changed")
				}
			}
			if r := request(mux, "GET", "/api/v1/clusters/"+cid+"/tres-operations", "", token); r.Code != 200 || !strings.Contains(r.Body.String(), action) {
				t.Fatal(r.Code, r.Body.String())
			}
			for _, family := range []string{"accounting", "qos"} {
				if r := request(mux, "GET", "/api/v1/clusters/"+cid+"/"+family+"-operations", "", token); r.Code != 200 || strings.Contains(r.Body.String(), "tres_update") {
					t.Fatal("families mixed", r.Code, r.Body.String())
				}
			}
		})
	}
}
func TestTRESClusterAdminCannotChangeSharedQOSOrProtectedTargets(t *testing.T) {
	_, mux, cid, token, f := tresSetup(t, "cluster_admin")
	base := "/api/v1/clusters/" + cid
	r := request(mux, "POST", base+"/accounting-controls/prepare", payload(t, map[string]any{"action": "qos_tres_update", "spec": accounting.Spec{TRES: quotaSpec("qos")}}), token)
	if r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
	v := quotaSpec("association")
	v.TargetID = "1"
	v.Account = "root"
	v.User = ""
	r = request(mux, "POST", base+"/accounting-controls/prepare", payload(t, map[string]any{"action": "association_tres_update", "spec": accounting.Spec{TRES: v}}), token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"allowed":false`) {
		t.Fatal(r.Code, r.Body.String())
	}
	p := accountingPrepare(t, mux, cid, token, "association_tres_update", accounting.Spec{TRES: quotaSpec("association")})
	v2 := accountingExecute(t, mux, cid, token, p)
	if v2.Status != "confirmed" || f.writes.Load() != 1 {
		t.Fatal(v2)
	}
}
func TestTRESExternalChangeInvalidatesProofAndUnknownReconciles(t *testing.T) {
	app, mux, cid, token, f := tresSetup(t, "super_admin")
	spec := accounting.Spec{TRES: quotaSpec("qos")}
	p := accountingPrepare(t, mux, cid, token, "qos_tres_update", spec)
	f.mu.Lock()
	f.qos[0]["description"] = "changed externally"
	f.mu.Unlock()
	r := request(mux, "POST", "/api/v1/clusters/"+cid+"/accounting-controls/execute", payload(t, accounting.Input{Proof: p.Proof, Confirmation: p.Confirmation, Reason: "TRES verification"}), token)
	if r.Code != 409 || f.writes.Load() != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	f.loseReply = true
	f.failReadback = true
	p = accountingPrepare(t, mux, cid, token, "qos_tres_update", spec)
	v := accountingExecute(t, mux, cid, token, p)
	if v.Status != "unknown" {
		t.Fatal(v)
	}
	app.Store().Set("platform.accounting", nil)
	r = request(mux, "GET", "/api/v1/clusters/"+cid+"/accounting-operations/"+v.OperationID, "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"confirmed"`) || f.writes.Load() != 1 {
		t.Fatal(r.Code, r.Body.String())
	}
	services.SlurmFor(app).Invalidate(cid)
}
func TestTRESRelativeAndInvalidResource(t *testing.T) {
	_, mux, cid, token, f := tresSetup(t, "super_admin")
	f.qos[0]["flags"] = []string{"RELATIVE"}
	v := quotaSpec("qos")
	v.Value = "101"
	for _, change := range []func(){func() {}, func() { v.Value = "3"; v.ResourceID = "999" }, func() { v.ResourceID = "1"; v.Bucket = "group_minutes" }} {
		change()
		r := request(mux, "POST", "/api/v1/clusters/"+cid+"/accounting-controls/prepare", payload(t, map[string]any{"action": "qos_tres_update", "spec": accounting.Spec{TRES: v}}), token)
		if r.Code != 200 || !strings.Contains(r.Body.String(), `"allowed":false`) {
			t.Fatal(r.Code, r.Body.String())
		}
	}
}

func TestTRESAssociationPreservesEveryOtherBucket(t *testing.T) {
	_, mux, cid, token, f := tresSetup(t, "super_admin")
	for _, a := range f.assocs {
		tres := a["max"].(map[string]any)["tres"].(map[string]any)
		tres["per"] = map[string]any{"job": []any{map[string]any{"id": 1, "type": "cpu", "name": "", "count": 8}}, "node": []any{map[string]any{"id": 2, "type": "mem", "name": "", "count": 256}}}
		tres["group"] = map[string]any{"minutes": []any{map[string]any{"id": 1, "type": "cpu", "name": "", "count": 100}}, "active": []any{map[string]any{"id": 1, "type": "cpu", "name": "", "count": 200}}}
		tres["minutes"] = map[string]any{"per": map[string]any{"job": []any{map[string]any{"id": 1, "type": "cpu", "name": "", "count": 60}}}}
	}
	v := quotaSpec("association")
	v.Bucket = "per_job"
	v.Value = "5"
	p := accountingPrepare(t, mux, cid, token, "association_tres_update", accounting.Spec{TRES: v})
	if r := accountingExecute(t, mux, cid, token, p); r.Status != "confirmed" {
		t.Fatal(r)
	}
}

func TestTRESRegisteredGPULimit(t *testing.T) {
	_, mux, cid, token, _ := tresSetup(t, "super_admin")
	v := quotaSpec("association")
	v.ResourceID = "1001"
	v.Value = "1"
	p := accountingPrepare(t, mux, cid, token, "association_tres_update", accounting.Spec{TRES: v})
	if r := accountingExecute(t, mux, cid, token, p); r.Status != "confirmed" {
		t.Fatal(r)
	}
}
func TestTRESRefusesUnpreservableOtherLimit(t *testing.T) {
	for _, count := range []string{"-1", "18446744073709551613"} {
		t.Run(count, func(t *testing.T) {
			_, mux, cid, token, f := tresSetup(t, "super_admin")
			for _, a := range f.assocs {
				a["max"].(map[string]any)["tres"].(map[string]any)["per"] = map[string]any{"node": []any{map[string]any{"id": 2, "type": "mem", "name": "", "count": json.Number(count)}}}
			}
			r := request(mux, "POST", "/api/v1/clusters/"+cid+"/accounting-controls/prepare", payload(t, map[string]any{"action": "association_tres_update", "spec": accounting.Spec{TRES: quotaSpec("association")}}), token)
			if r.Code != 200 || strings.Contains(r.Body.String(), `"allowed":true`) || f.writes.Load() != 0 {
				t.Fatal(r.Code, r.Body.String())
			}
		})
	}
}
