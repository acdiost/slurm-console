package api_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/accounting"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/services"
)

type accountingFixture struct {
	mu                                sync.Mutex
	app                               core.App
	accounts, users, assocs           []map[string]any
	writes                            atomic.Int32
	loseReply, failReadback, rejected bool
	onRead                            func()
	jobs                              string
	qos                               []map[string]any
	shares                            string
	tres                              []map[string]any
	reservations                      []map[string]any
}

func newAccountingFixture(app core.App) *accountingFixture {
	return &accountingFixture{app: app,
		accounts: []map[string]any{{"name": "root", "description": "root", "organization": "root"}, {"name": "research", "description": "Research", "organization": "Lab"}, {"name": "secondary", "description": "Secondary", "organization": "Lab"}, {"name": "hidden", "description": "do-not-expose", "organization": "secret"}},
		users:    []map[string]any{{"name": "root", "default": map[string]any{"account": "root"}, "administrator_level": []string{"Administrator"}}, {"name": "alice", "default": map[string]any{"account": "research"}, "administrator_level": []string{"None"}}, {"name": "hidden-user", "default": map[string]any{"account": "hidden"}, "administrator_level": []string{"None"}}},
		assocs:   []map[string]any{{"id": 1, "cluster": "fixture", "account": "root", "user": "", "is_default": false}, {"id": 2, "cluster": "fixture", "account": "root", "user": "root", "is_default": true}, {"id": 3, "cluster": "fixture", "account": "research", "user": "", "parent_account": "root"}, {"id": 4, "cluster": "fixture", "account": "research", "user": "alice", "is_default": true}, {"id": 5, "cluster": "fixture", "account": "secondary", "user": "", "parent_account": "root"}, {"id": 6, "cluster": "fixture", "account": "secondary", "user": "alice", "is_default": false}, {"id": 7, "cluster": "foreign", "account": "hidden", "user": "hidden-user", "is_default": true}}, jobs: `"jobs":[]`}
}
func (f *accountingFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == "GET" && f.onRead != nil {
		callback := f.onRead
		f.onRead = nil
		callback()
	}
	if r.Method == "GET" && f.failReadback && f.writes.Load() > 0 {
		f.failReadback = false
		w.WriteHeader(503)
		return
	}
	if f.reservationHTTP(w, r) {
		return
	}
	if strings.HasPrefix(r.URL.Path, "/slurm/") {
		switch r.URL.Path {
		case "/slurm/v0.0.43/ping/":
			fmt.Fprint(w, slurmEnvelope(`"pings":[{"hostname":"controller","pinged":"UP","mode":"primary"}]`))
		case "/slurm/v0.0.43/nodes/":
			fmt.Fprint(w, slurmEnvelope(fixtureNodes))
		case "/slurm/v0.0.43/jobs/":
			fmt.Fprint(w, slurmEnvelope(f.jobs))
		case "/slurm/v0.0.43/partitions/":
			fmt.Fprint(w, slurmEnvelope(fixturePartitions))
		case "/slurm/v0.0.43/shares":
			fmt.Fprint(w, strings.Replace(slurmEnvelope(f.shares), `"data_parser":"data_parser/v0.0.43"`, `"data_parser":"data_parser/v0.0.43","type":"openapi/slurmctld"`, 1))
		default:
			w.WriteHeader(404)
		}
		return
	}
	emit := func(key string, value any) {
		raw, _ := json.Marshal(value)
		body := slurmEnvelope(fmt.Sprintf(`%q:%s`, key, raw))
		body = strings.Replace(body, `"data_parser":"data_parser/v0.0.43"`, `"data_parser":"data_parser/v0.0.43","type":"openapi/slurmdbd"`, 1)
		fmt.Fprint(w, body)
	}
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/slurmdb/v0.0.43/accounts/":
			emit("accounts", f.accounts)
		case "/slurmdb/v0.0.43/users/":
			if r.URL.Query().Get("with_assocs") != "true" {
				w.WriteHeader(400)
				return
			}
			emit("users", f.users)
		case "/slurmdb/v0.0.43/associations/":
			if r.URL.Query().Get("cluster") != "fixture" {
				w.WriteHeader(400)
				return
			}
			emit("associations", f.assocs)
		case "/slurmdb/v0.0.43/qos/":
			emit("qos", f.qos)
		case "/slurmdb/v0.0.43/tres/":
			emit("TRES", f.tres)
		default:
			w.WriteHeader(404)
		}
		return
	}
	if f.rejected {
		w.WriteHeader(403)
		return
	}
	intents, err := f.app.FindRecordsByFilter("operation_audits", "status='processing' && accounting_spec != null", "", 1, 0)
	if err != nil || len(intents) != 1 || intents[0].GetString("actor_id") == "" || intents[0].GetString("request_id") == "" {
		w.WriteHeader(500)
		return
	}
	f.writes.Add(1)
	var body struct {
		Accounts, Associations, QOS []map[string]any
		Account                     map[string]any
		User                        struct {
			DefaultAccount string `json:"defaultaccount"`
			Adminlevel     []string
		}
		Condition struct{ Accounts, Clusters, Users []string } `json:"association_condition"`
	}
	d := json.NewDecoder(r.Body)
	d.UseNumber()
	if r.Method == "POST" && d.Decode(&body) != nil {
		w.WriteHeader(400)
		return
	}
	switch r.URL.Path {
	case "/slurmdb/v0.0.43/qos/":
		if len(body.QOS) != 1 {
			w.WriteHeader(400)
			return
		}
		v := body.QOS[0]
		found := false
		for i, q := range f.qos {
			if q["name"] == v["name"] {
				// Match v0.0.43 partial updates: omitted mode resets to DISABLED,
				// NOT_SET leaves flags intact, and omitted other fields survive.
				delete(v, "flags")
				if v["preempt"] == nil {
					v["preempt"] = map[string]any{"mode": []string{"DISABLED"}}
				}
				mergeAccountingFixture(q, v, true)
				f.qos[i] = q
				found = true
			}
		}
		if !found {
			v["id"] = 100 + len(f.qos)
			v["flags"] = []string{}
			v["preempt"] = map[string]any{"mode": []string{"DISABLED"}}
			f.qos = append(f.qos, v)
		}
	case "/slurmdb/v0.0.43/accounts_association/":
		f.accounts = append(f.accounts, map[string]any{"name": body.Condition.Accounts[0], "description": body.Account["description"], "organization": body.Account["organization"]})
		f.assocs = append(f.assocs, map[string]any{"id": 100 + len(f.assocs), "cluster": body.Condition.Clusters[0], "account": body.Condition.Accounts[0], "user": "", "parent_account": "root"})
	case "/slurmdb/v0.0.43/accounts/":
		for i, a := range f.accounts {
			if a["name"] == body.Accounts[0]["name"] {
				f.accounts[i] = body.Accounts[0]
			}
		}
	case "/slurmdb/v0.0.43/users_association/":
		f.users = append(f.users, map[string]any{"name": body.Condition.Users[0], "administrator_level": body.User.Adminlevel, "default": map[string]any{"account": body.User.DefaultAccount}})
		f.assocs = append(f.assocs, map[string]any{"id": 100 + len(f.assocs), "cluster": body.Condition.Clusters[0], "account": body.Condition.Accounts[0], "user": body.Condition.Users[0], "is_default": true})
	case "/slurmdb/v0.0.43/associations/":
		a := body.Associations[0]
		if a["id"] != nil {
			found := false
			for _, existing := range f.assocs {
				if fmt.Sprint(existing["id"]) == fmt.Sprint(a["id"]) {
					if max, ok := a["max"].(map[string]any); ok && max["tres"] != nil {
						// Slurm 25.11 resets GrpTRES when another nested TRES bucket is supplied alone.
						existing["max"].(map[string]any)["tres"].(map[string]any)["total"] = []any{}
					}
					mergeAccountingFixture(existing, a, false)
					found = true
					break
				}
			}
			if !found {
				w.WriteHeader(404)
				return
			}
			break
		}
		a["id"] = 100 + len(f.assocs)
		f.assocs = append(f.assocs, a)
	case "/slurmdb/v0.0.43/association/":
		id, _ := strconv.Atoi(r.URL.Query().Get("id"))
		if r.Method != "DELETE" || r.URL.Query().Get("cluster") != "fixture" {
			w.WriteHeader(400)
			return
		}
		out := f.assocs[:0]
		for _, a := range f.assocs {
			if a["id"] != id {
				out = append(out, a)
			}
		}
		f.assocs = out
	default:
		w.WriteHeader(404)
		return
	}
	if f.loseReply {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
		return
	}
	emit("warnings", []any{})
}
func mergeAccountingFixture(dst, src map[string]any, qos bool) {
	for key, value := range src {
		child, ok := value.(map[string]any)
		previous, exists := dst[key].(map[string]any)
		if ok && exists {
			mergeAccountingFixture(previous, child, qos)
			continue
		}
		rows, isArray := value.([]any)
		if isArray && len(rows) > 0 {
			if entry, ok := rows[0].(map[string]any); ok && entry["count"] != nil {
				result := []any{}
				changed := map[string]bool{}
				for _, row := range rows {
					changed[fmt.Sprint(row.(map[string]any)["id"])] = true
				}
				if qos {
					old, _ := dst[key].([]any)
					for _, row := range old {
						if !changed[fmt.Sprint(row.(map[string]any)["id"])] {
							result = append(result, row)
						}
					}
				}
				for _, row := range rows {
					if fmt.Sprint(row.(map[string]any)["count"]) != "-1" {
						result = append(result, row)
					}
				}
				dst[key] = result
				continue
			}
		}
		dst[key] = value
	}
}
func accountingSetup(t *testing.T, role string) (core.App, http.Handler, string, string, *accountingFixture) {
	t.Helper()
	app, mux := setup(t)
	f := newAccountingFixture(app)
	manager := clusterconfig.Register(app, connectionRegistry(t, f.ServeHTTP))
	config, err := manager.Save("", validConnection())
	if err != nil {
		t.Fatal(err)
	}
	person := user(t, app, "users")
	scope := config.ID
	if role == "super_admin" {
		scope = ""
	}
	grant(t, app, person.Id, role, scope)
	return app, mux, config.ID, login(t, mux), f
}
func accountingPrepare(t *testing.T, mux http.Handler, cid, token, action string, v accounting.Spec) accounting.Prepared {
	t.Helper()
	r := request(mux, "POST", "/api/v1/clusters/"+cid+"/accounting-controls/prepare", payload(t, map[string]any{"action": action, "spec": v}), token)
	var body struct{ Data accounting.Prepared }
	_ = json.Unmarshal(r.Body.Bytes(), &body)
	if r.Code != 200 || !body.Data.Allowed {
		t.Fatal(r.Code, r.Body.String())
	}
	return body.Data
}
func accountingExecute(t *testing.T, mux http.Handler, cid, token string, p accounting.Prepared) accounting.Result {
	t.Helper()
	r := request(mux, "POST", "/api/v1/clusters/"+cid+"/accounting-controls/execute", payload(t, accounting.Input{Proof: p.Proof, Confirmation: p.Confirmation, Reason: "isolated accounting verification"}), token)
	var body struct{ Data accounting.Result }
	_ = json.Unmarshal(r.Body.Bytes(), &body)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	return body.Data
}
func TestAccountingReadRoutesScopeFilterAndRevoke(t *testing.T) {
	app, mux, cid, token, f := accountingSetup(t, "viewer")
	base := "/api/v1/clusters/" + cid
	for _, resource := range []string{"accounts", "slurm-users", "associations"} {
		r := request(mux, "GET", base+"/"+resource, "", token)
		if r.Code != 200 || strings.Contains(r.Body.String(), "do-not-expose") || strings.Contains(r.Body.String(), "hidden-user") || strings.Contains(r.Body.String(), "secret") {
			t.Fatal(r.Code, r.Body.String())
		}
		r = request(mux, "GET", base+"/"+resource+"?q=alice&perPage=1&page=1", "", token)
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	for _, path := range []string{"/accounts/root", "/slurm-users/alice", "/associations/6"} {
		if r := request(mux, "GET", base+path, "", token); r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	for _, path := range []string{"/accounts/hidden", "/slurm-users/hidden-user", "/associations/7"} {
		if r := request(mux, "GET", base+path, "", token); r.Code != 404 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	if r := request(mux, "GET", base+"/associations?account=secondary&user=alice&kind=user", "", token); r.Code != 200 || !strings.Contains(r.Body.String(), `"totalItems":1`) {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, path := range []string{"/accounts?sort=endpoint", "/associations?cluster=foreign", "/accounts?q=a&q=b", "/accounts?page=0", "/accounts?account=x"} {
		if r := request(mux, "GET", base+path, "", token); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	r := request(mux, "POST", base+"/accounting-controls/prepare", `{"action":"association_create","spec":{"account":"secondary","user":"alice","partition":"gpu"}}`, token)
	if r.Code != 403 || f.writes.Load() != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	rows, _ := app.FindRecordsByFilter("user_cluster_roles", "", "", 0, 0)
	if err := app.Delete(rows[0]); err != nil {
		t.Fatal(err)
	}
	if r := request(mux, "GET", base+"/accounts", "", token); r.Code != 403 {
		t.Fatal("revocation bypassed cache", r.Code)
	}
}
func TestAccountingMutationsAuditAndReplay(t *testing.T) {
	app, mux, cid, token, f := accountingSetup(t, "super_admin")
	for _, tc := range []struct {
		action string
		spec   accounting.Spec
	}{
		{"account_create", accounting.Spec{Account: "new-lab", Description: "New lab", Organization: "Lab"}},
		{"account_update", accounting.Spec{Account: "new-lab", Description: "Updated lab", Organization: "Science"}},
		{"user_create", accounting.Spec{Account: "new-lab", User: "new-user"}},
		{"association_create", accounting.Spec{Account: "secondary", User: "new-user"}},
	} {
		p := accountingPrepare(t, mux, cid, token, tc.action, tc.spec)
		v := accountingExecute(t, mux, cid, token, p)
		if v.Status != "confirmed" {
			t.Fatal(v)
		}
		before := f.writes.Load()
		again := accountingExecute(t, mux, cid, token, p)
		if again.OperationID != v.OperationID || f.writes.Load() != before {
			t.Fatal("mutation replayed")
		}
		altered := request(mux, "POST", "/api/v1/clusters/"+cid+"/accounting-controls/execute", payload(t, accounting.Input{Proof: p.Proof, Confirmation: p.Confirmation, Reason: "different reason"}), token)
		if altered.Code != 409 {
			t.Fatal("altered replay accepted")
		}
	}
	p := accountingPrepare(t, mux, cid, token, "association_remove", accounting.Spec{Account: "secondary", User: "alice", AssociationID: "6"})
	v := accountingExecute(t, mux, cid, token, p)
	if v.Status != "confirmed" {
		t.Fatal(v)
	}
	records, err := app.FindRecordsByFilter("operation_audits", "accounting_spec != null", "", 0, 0)
	if err != nil || len(records) != 5 {
		t.Fatal(len(records), err)
	}
	for _, r := range records {
		if r.GetString("status") != "confirmed" || r.GetString("actor_source") != "platform" || r.GetString("reason") == "" {
			t.Fatal("audit incomplete")
		}
	}
	if f.writes.Load() != 5 {
		t.Fatal("unexpected write count", f.writes.Load())
	}
	recent := request(mux, "GET", "/api/v1/clusters/"+cid+"/accounting-operations", "", token)
	if recent.Code != 200 || !strings.Contains(recent.Body.String(), p.OperationID) {
		t.Fatal(recent.Body.String())
	}
}
func TestAccountingRejectsUnsafeGlobalWritesDefaultAndActiveAssociations(t *testing.T) {
	_, mux, cid, token, f := accountingSetup(t, "cluster_admin")
	base := "/api/v1/clusters/" + cid
	if r := request(mux, "POST", base+"/accounting-controls/prepare", `{"action":"account_create","spec":{"account":"newlab","description":"New","organization":"Lab"}}`, token); r.Code != 403 {
		t.Fatal("cluster role edited global identities", r.Code)
	}
	for _, v := range []accounting.Spec{{Account: "root", User: "root", AssociationID: "2"}, {Account: "research", User: "alice", AssociationID: "4"}, {Account: "hidden", User: "hidden-user", AssociationID: "7"}} {
		r := request(mux, "POST", base+"/accounting-controls/prepare", payload(t, map[string]any{"action": "association_remove", "spec": v}), token)
		if r.Code != 400 && (r.Code != 200 || !strings.Contains(r.Body.String(), `"allowed":false`)) {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	f.mu.Lock()
	f.jobs = `"jobs":[{"job_id":42,"user_name":"alice","account":"secondary","job_state":["FAILED","COMPLETING"]}]`
	f.mu.Unlock()
	r := request(mux, "POST", base+"/accounting-controls/prepare", `{"action":"association_remove","spec":{"account":"secondary","user":"alice","associationId":"6"}}`, token)
	if r.Code != 403 || f.writes.Load() != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, body := range []string{`{"action":"association_create","spec":{"account":"secondary","user":"alice","adminlevel":"Administrator"}}`, `{"action":"association_remove","spec":{"account":"secondary","user":"alice","associationId":"6","cluster":"foreign"}}`} {
		if r := request(mux, "POST", base+"/accounting-controls/prepare", body, token); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
}
func TestAccountingUnknownResultsReconcileAfterRestartWithoutResending(t *testing.T) {
	app, mux, cid, token, f := accountingSetup(t, "super_admin")
	p := accountingPrepare(t, mux, cid, token, "account_create", accounting.Spec{Account: "qa-unknown", Description: "QA", Organization: "Lab"})
	f.mu.Lock()
	f.loseReply = true
	f.failReadback = true
	f.mu.Unlock()
	v := accountingExecute(t, mux, cid, token, p)
	if v.Status != "unknown" {
		t.Fatal(v)
	}
	app.Store().Set("platform.accounting", nil)
	r := request(mux, "GET", "/api/v1/clusters/"+cid+"/accounting-operations/"+p.OperationID, "", token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"confirmed"`) || f.writes.Load() != 1 {
		t.Fatal(r.Code, r.Body.String(), f.writes.Load())
	}
}
func TestAccountingRefusesChangedStateRevocationAndAuditFailure(t *testing.T) {
	for _, mode := range []string{"revision", "state", "revoke", "audit", "reject"} {
		t.Run(mode, func(t *testing.T) {
			app, mux, cid, token, f := accountingSetup(t, "super_admin")
			p := accountingPrepare(t, mux, cid, token, "account_update", accounting.Spec{Account: "research", Description: "new description", Organization: "Lab"})
			switch mode {
			case "revision":
				c, _ := app.FindRecordById("clusters", cid)
				c.Set("revision", c.GetInt("revision")+1)
				if err := app.Save(c); err != nil {
					t.Fatal(err)
				}
			case "state":
				f.mu.Lock()
				f.accounts[1]["description"] = "changed by external admin"
				f.mu.Unlock()
			case "revoke":
				rows, _ := app.FindRecordsByFilter("user_platform_roles", "", "", 0, 0)
				if err := app.Delete(rows[0]); err != nil {
					t.Fatal(err)
				}
			case "audit":
				app.OnRecordCreate("operation_audits").BindFunc(func(*core.RecordEvent) error { return errors.New("audit unavailable") })
			case "reject":
				f.mu.Lock()
				f.rejected = true
				f.mu.Unlock()
			}
			r := request(mux, "POST", "/api/v1/clusters/"+cid+"/accounting-controls/execute", payload(t, accounting.Input{Proof: p.Proof, Confirmation: p.Confirmation, Reason: "test changes"}), token)
			if mode == "reject" {
				if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"failed"`) {
					t.Fatal(r.Code, r.Body.String())
				}
			} else if r.Code != 403 && r.Code != 409 && r.Code != 503 {
				t.Fatal(r.Code, r.Body.String())
			}
			if f.writes.Load() != 0 {
				t.Fatal("rejected write reached upstream")
			}
		})
	}
}
func TestAccountingRevocationDuringReadDoesNotReturnData(t *testing.T) {
	app, mux, cid, token, f := accountingSetup(t, "viewer")
	rows, _ := app.FindRecordsByFilter("user_cluster_roles", "", "", 0, 0)
	f.mu.Lock()
	f.onRead = func() {
		if err := app.Delete(rows[0]); err != nil {
			t.Error(err)
		}
	}
	f.mu.Unlock()
	services.SlurmFor(app).Invalidate(cid)
	r := request(mux, "GET", "/api/v1/clusters/"+cid+"/accounts", "", token)
	if r.Code != 403 || strings.Contains(r.Body.String(), "Research") {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestAccountingGeneralAssociationCannotCascadeToPartitionChildren(t *testing.T) {
	_, mux, cid, token, f := accountingSetup(t, "cluster_admin")
	f.mu.Lock()
	f.assocs = append(f.assocs, map[string]any{"id": 8, "cluster": "fixture", "account": "secondary", "user": "alice", "partition": "gpu", "is_default": false})
	f.mu.Unlock()
	r := request(mux, "POST", "/api/v1/clusters/"+cid+"/accounting-controls/prepare", payload(t, map[string]any{"action": "association_remove", "spec": accounting.Spec{Account: "secondary", User: "alice", AssociationID: "6"}}), token)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"allowed":false`) {
		t.Fatal(r.Code, r.Body.String())
	}
	p := accountingPrepare(t, mux, cid, token, "association_remove", accounting.Spec{Account: "secondary", User: "alice", Partition: "gpu", AssociationID: "8"})
	v := accountingExecute(t, mux, cid, token, p)
	if v.Status != "confirmed" {
		t.Fatal(v)
	}
	p = accountingPrepare(t, mux, cid, token, "association_remove", accounting.Spec{Account: "secondary", User: "alice", AssociationID: "6"})
	v = accountingExecute(t, mux, cid, token, p)
	if v.Status != "confirmed" || f.writes.Load() != 2 {
		t.Fatal(v, f.writes.Load())
	}
}
