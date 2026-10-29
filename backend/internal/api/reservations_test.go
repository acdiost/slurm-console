package api_test

import (
	"encoding/json"
	"fmt"
	"github.com/pocketbase/pocketbase/core"
	"net/http"
	"slices"
	"slurm-console/backend/internal/accounting"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"strings"
	"testing"
	"time"
)

func (f *accountingFixture) reservationHTTP(w http.ResponseWriter, r *http.Request) bool {
	path := "/slurm/v0.0.43/reservation"
	if r.URL.Path != path && r.URL.Path != path+"s/" && !strings.HasPrefix(r.URL.Path, path+"/") {
		return false
	}
	emit := func() {
		v := f.reservations
		if v == nil {
			v = []map[string]any{}
		}
		b, _ := json.Marshal(v)
		fmt.Fprint(w, slurmEnvelope(`"reservations":`+string(b)))
	}
	if r.Method == "GET" {
		emit()
		return true
	}
	if f.rejected {
		w.WriteHeader(403)
		return true
	}
	f.writes.Add(1)
	if r.Method == "DELETE" {
		name := strings.TrimPrefix(r.URL.Path, path+"/")
		f.reservations = slices.DeleteFunc(f.reservations, func(v map[string]any) bool { return v["name"] == name })
	} else {
		d := json.NewDecoder(r.Body)
		d.UseNumber()
		var b map[string]any
		_ = d.Decode(&b)
		name := b["name"].(string)
		var target map[string]any
		for _, x := range f.reservations {
			if x["name"] == name {
				target = x
			}
		}
		if target == nil {
			target = map[string]any{"name": name, "node_count": 1, "core_count": 8, "core_specializations": []any{}, "groups": "", "partition": "", "features": "", "licenses": "", "burst_buffer": "", "tres": "cpu=8", "max_start_delay": 0, "purge_completed": map[string]any{"time": map[string]any{"set": true, "infinite": false, "number": 300}}, "watts": map[string]any{}}
			f.reservations = append(f.reservations, target)
		}
		for k, v := range b {
			switch k {
			case "node_list", "users", "accounts":
				if v == nil {
					target[k] = ""
					break
				}
				parts := []string{}
				for _, x := range v.([]any) {
					parts = append(parts, x.(string))
				}
				target[k] = strings.Join(parts, ",")
			case "flags":
				flags := append(v.([]any), "SPEC_NODES")
				target[k] = flags
			default:
				target[k] = v
			}
		}
	}
	if f.loseReply {
		c, _, e := w.(http.Hijacker).Hijack()
		if e == nil {
			c.Close()
		}
		return true
	}
	emit()
	return true
}
func reservationSetup(t *testing.T, role string) (core.App, http.Handler, string, string, *accountingFixture) {
	return accountingSetup(t, role)
}
func reservationSpec() *client.ReservationSpec {
	now := time.Now().Unix() + 86400
	return &client.ReservationSpec{Name: "qa-reservation", StartTime: now, EndTime: now + 3600, Nodes: []string{"node1"}, Users: []string{"alice"}, Accounts: []string{}, Maintenance: true}
}
func reservationPrepare(t *testing.T, m http.Handler, cid, token, action string, v *client.ReservationSpec) accounting.Prepared {
	t.Helper()
	r := request(m, "POST", "/api/v1/clusters/"+cid+"/reservation-controls/prepare", payload(t, map[string]any{"action": action, "spec": v}), token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var out struct {
		Data accounting.Prepared `json:"data"`
	}
	if json.Unmarshal(r.Body.Bytes(), &out) != nil {
		t.Fatal(r.Body.String())
	}
	return out.Data
}
func reservationExecute(t *testing.T, m http.Handler, cid, token string, p accounting.Prepared) accounting.Result {
	t.Helper()
	r := request(m, "POST", "/api/v1/clusters/"+cid+"/reservation-controls/execute", payload(t, map[string]any{"proof": p.Proof, "confirmation": p.Confirmation, "reason": "测试资源预留"}), token)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var out struct {
		Data accounting.Result `json:"data"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &out)
	return out.Data
}
func TestReservationCRUDPreservesPolicyAndReplays(t *testing.T) {
	_, mux, cid, token, f := reservationSetup(t, "cluster_admin")
	v := reservationSpec()
	base := "/api/v1/clusters/" + cid
	p := reservationPrepare(t, mux, cid, token, "reservation_create", v)
	if !p.Allowed {
		t.Fatal(p)
	}
	r := reservationExecute(t, mux, cid, token, p)
	if r.Status != "confirmed" {
		t.Fatal(r)
	}
	if r2 := reservationExecute(t, mux, cid, token, p); r2.OperationID != r.OperationID || f.writes.Load() != 1 {
		t.Fatal(r2)
	}
	for _, path := range []string{"/reservations?state=scheduled&q=alice&sort=endTime", "/reservations/qa-reservation", "/reservation-operations"} {
		r := request(mux, "GET", base+path, "", token)
		if r.Code != 200 || !strings.Contains(r.Body.String(), v.Name) {
			t.Fatal(path, r.Code, r.Body.String())
		}
	}
	p = reservationPrepare(t, mux, cid, token, "reservation_update", &client.ReservationSpec{Name: v.Name, EndTime: v.EndTime + 600})
	if !p.Allowed {
		t.Fatal(p)
	}
	r = reservationExecute(t, mux, cid, token, p)
	if r.Status != "confirmed" {
		t.Fatal(r)
	}
	if strings.Join([]string{fmt.Sprint(f.reservations[0]["flags"]), f.reservations[0]["users"].(string), f.reservations[0]["node_list"].(string)}, " ") != "[MAINT SPEC_NODES] alice node1" {
		t.Fatal(f.reservations)
	}
	p = reservationPrepare(t, mux, cid, token, "reservation_delete", &client.ReservationSpec{Name: v.Name})
	if !p.Allowed {
		t.Fatal(p)
	}
	r = reservationExecute(t, mux, cid, token, p)
	if r.Status != "confirmed" || len(f.reservations) != 0 {
		t.Fatal(r)
	}
	if r := request(mux, "GET", base+"/accounting-operations", "", token); strings.Contains(r.Body.String(), "qa-reservation") {
		t.Fatal("reservation leaked into accounting family", r.Body.String())
	}
}
func TestReservationReadPermissionsAndStrictInputs(t *testing.T) {
	_, mux, cid, token, _ := reservationSetup(t, "viewer")
	base := "/api/v1/clusters/" + cid
	if r := request(mux, "GET", base+"/reservations", "", token); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, path := range []string{"/reservations?state=bad", "/reservations?sort=bad", "/reservations?cluster=foreign"} {
		if r := request(mux, "GET", base+path, "", token); r.Code != 400 {
			t.Fatal(path, r.Code)
		}
	}
	if r := request(mux, "POST", base+"/reservation-controls/prepare", payload(t, map[string]any{"action": "reservation_create", "spec": reservationSpec()}), token); r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
}
func TestReservationRejectsJobsAdvancedAndStaleProof(t *testing.T) {
	_, mux, cid, token, f := reservationSetup(t, "super_admin")
	v := reservationSpec()
	p := reservationPrepare(t, mux, cid, token, "reservation_create", v)
	_ = reservationExecute(t, mux, cid, token, p)
	p = reservationPrepare(t, mux, cid, token, "reservation_update", &client.ReservationSpec{Name: v.Name, EndTime: v.EndTime + 600})
	f.reservations[0]["features"] = "external-policy"
	r := request(mux, "POST", "/api/v1/clusters/"+cid+"/reservation-controls/execute", payload(t, map[string]any{"proof": p.Proof, "confirmation": p.Confirmation, "reason": "旧确认不得写入"}), token)
	if r.Code != 409 || f.writes.Load() != 1 {
		t.Fatal(r.Code, r.Body.String())
	}
	p = reservationPrepare(t, mux, cid, token, "reservation_update", &client.ReservationSpec{Name: v.Name, EndTime: v.EndTime + 600})
	if p.Allowed {
		t.Fatal("advanced edited")
	}
	f.reservations[0]["features"] = ""
	for _, jobs := range []string{`"jobs":[{"job_id":42,"job_state":["PENDING"],"resv_name":"qa-reservation"}]`, `"jobs":[{"job_id":42,"job_state":["PENDING"]}]`} {
		f.jobs = jobs
		r = request(mux, "POST", "/api/v1/clusters/"+cid+"/reservation-controls/prepare", payload(t, map[string]any{"action": "reservation_delete", "spec": client.ReservationSpec{Name: v.Name}}), token)
		if r.Code != 403 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	f.jobs = `"jobs":[{"job_id":43,"job_state":["PENDING"],"resv_name":"other-reservation"}]`
	p = reservationPrepare(t, mux, cid, token, "reservation_delete", &client.ReservationSpec{Name: v.Name})
	if !p.Allowed {
		t.Fatal("unrelated known reservation blocked", p)
	}

	f.jobs = `"jobs":[]`
	f.reservations[0]["core_specializations"] = []any{map[string]any{"node": "node1", "core": "0"}}
	p = reservationPrepare(t, mux, cid, token, "reservation_delete", &client.ReservationSpec{Name: v.Name})
	if p.Allowed {
		t.Fatal("core-specific reservation mutated")
	}
}
func TestReservationUnknownRestartsReadOnly(t *testing.T) {
	app, mux, cid, token, f := reservationSetup(t, "super_admin")
	p := reservationPrepare(t, mux, cid, token, "reservation_create", reservationSpec())
	f.loseReply = true
	f.failReadback = true
	r := reservationExecute(t, mux, cid, token, p)
	if r.Status != "unknown" {
		t.Fatal(r)
	}
	app.Store().Remove("platform.accounting")
	app.Store().Remove("platform.slurm")
	services.SlurmFor(app).InvalidateAccounting()
	got := request(mux, "GET", "/api/v1/clusters/"+cid+"/reservation-operations/"+r.OperationID, "", token)
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"status":"confirmed"`) || f.writes.Load() != 1 {
		t.Fatal(got.Code, got.Body.String())
	}
}
