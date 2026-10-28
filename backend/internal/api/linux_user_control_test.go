package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"slurm-console/backend/internal/linuxusers"
)

type adminFixture struct {
	app                      core.App
	prepare, execute, status int
	loseReply                bool
	onExecute                func()
}

func (f *adminFixture) Run(_ context.Context, r linuxusers.AdminRequest) (linuxusers.AdminResult, error) {
	switch r.Mode {
	case "capabilities":
		return linuxusers.AdminResult{Allowed: true, MinUID: 10000, MaxUID: 59999}, nil
	case "prepare":
		f.prepare++
		return linuxusers.AdminResult{Allowed: true, Fingerprint: linuxusers.Hash(r.Spec), MinUID: 10000, MaxUID: 59999, Message: "核对创建信息。"}, nil
	case "execute":
		f.execute++
		n, err := f.app.CountRecords("operation_audits")
		if err != nil || n == 0 {
			return linuxusers.AdminResult{}, errors.New("write without audit")
		}
		if f.onExecute != nil {
			f.onExecute()
		}
		if f.loseReply {
			return linuxusers.AdminResult{}, errors.New("reply lost")
		}
		return linuxusers.AdminResult{Status: "confirmed", OperationID: r.OperationID, Message: "主机已核实。", State: &linuxusers.AccountState{User: linuxusers.User{Name: r.Spec.Name, UID: r.Spec.UID, Home: "/home/" + r.Spec.Name, Shell: r.Spec.Shell}, Expiry: 1}}, nil
	case "status":
		f.status++
		return linuxusers.AdminResult{Status: "confirmed", OperationID: r.OperationID, Message: "只读核实成功。"}, nil
	}
	return linuxusers.AdminResult{}, errors.New("invalid request")
}
func controlFixture(t *testing.T, role string) (core.App, http.Handler, string, string, *adminFixture) {
	t.Helper()
	app, mux := setup(t)
	c := linuxCluster(t, app, "alpha")
	person := user(t, app, "users")
	grant(t, app, person.Id, role, c.Id)
	s := linuxusers.Register(app, &linuxusers.Registry{Clusters: map[string]linuxusers.Profile{c.Id: {Label: "local", Transport: "local", ManageLocal: true}}}, nil)
	f := &adminFixture{app: app}
	s.SetAdminProvider(f)
	return app, mux, c.Id, login(t, mux), f
}
func prepareCreate(t *testing.T, mux http.Handler, cid, token string) linuxusers.Prepared {
	t.Helper()
	r := request(mux, "POST", "/api/v1/clusters/"+cid+"/linux-user-controls/prepare", `{"action":"create","spec":{"name":"worker","uid":10001,"comment":"worker","shell":"/bin/bash"}}`, token)
	var body struct{ Data linuxusers.Prepared }
	_ = json.Unmarshal(r.Body.Bytes(), &body)
	if r.Code != 200 || !body.Data.Allowed {
		t.Fatal(r.Code, r.Body.String())
	}
	return body.Data
}
func TestLinuxUserWritesHaveDistinctPermissionsAndAuditBeforeMutation(t *testing.T) {
	_, mux, cid, token, f := controlFixture(t, "viewer")
	prefix := "/api/v1/clusters/" + cid + "/linux-user-controls"
	if r := request(mux, "POST", prefix+"/prepare", `{"action":"create","spec":{"name":"worker","uid":10001,"shell":"/bin/bash"}}`, token); r.Code != 403 || f.prepare != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	app, mux, cid, token, f := controlFixture(t, "cluster_admin")
	prepared := prepareCreate(t, mux, cid, token)
	prefix = "/api/v1/clusters/" + cid + "/linux-user-controls"
	bad := fmt.Sprintf(`{"proof":%q,"confirmation":"different","reason":"test account"}`, prepared.Proof)
	if r := request(mux, "POST", prefix+"/execute", bad, token); r.Code != 409 || f.execute != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	input := fmt.Sprintf(`{"proof":%q,"confirmation":"worker","reason":"test account"}`, prepared.Proof)
	for i := 0; i < 2; i++ {
		r := request(mux, "POST", prefix+"/execute", input, token)
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
	}
	if f.execute != 1 {
		t.Fatal("host mutation replayed")
	}
	records, err := app.FindRecordsByFilter("operation_audits", "target_type='linux_user'", "", 0, 0)
	if err != nil || len(records) != 1 || records[0].GetString("status") != "confirmed" || records[0].GetString("reason") != "test account" {
		t.Fatal(records, err)
	}
	altered := fmt.Sprintf(`{"proof":%q,"confirmation":"worker","reason":"changed reason"}`, prepared.Proof)
	if r := request(mux, "POST", prefix+"/execute", altered, token); r.Code != 409 || f.execute != 1 {
		t.Fatal("altered replay accepted")
	}
	other := linuxCluster(t, app, "other")
	path := "/api/v1/clusters/" + other.Id + "/linux-user-operations/" + prepared.OperationID
	if r := request(mux, "GET", path, "", token); r.Code != 403 {
		t.Fatal("cross-cluster operation exposed")
	}
}
func TestLinuxUserWritesRefuseRevokedPermissionChangedRevisionAndFailedAudit(t *testing.T) {
	for _, mode := range []string{"revoke", "revision", "audit"} {
		t.Run(mode, func(t *testing.T) {
			app, mux, cid, token, f := controlFixture(t, "cluster_admin")
			p := prepareCreate(t, mux, cid, token)
			switch mode {
			case "revoke":
				rows, _ := app.FindRecordsByFilter("user_cluster_roles", "", "", 0, 0)
				if err := app.Delete(rows[0]); err != nil {
					t.Fatal(err)
				}
			case "revision":
				c, _ := app.FindRecordById("clusters", cid)
				c.Set("revision", c.GetInt("revision")+1)
				if err := app.Save(c); err != nil {
					t.Fatal(err)
				}
			case "audit":
				app.OnRecordCreate("operation_audits").BindFunc(func(*core.RecordEvent) error { return errors.New("audit offline") })
			}
			input := fmt.Sprintf(`{"proof":%q,"confirmation":"worker","reason":"test account"}`, p.Proof)
			r := request(mux, "POST", "/api/v1/clusters/"+cid+"/linux-user-controls/execute", input, token)
			if r.Code == 200 || f.execute != 0 {
				t.Fatal(mode, r.Code, r.Body.String())
			}
		})
	}
}
func TestLinuxUserUnknownResultSurvivesRestartAndOnlyReconcilesByRead(t *testing.T) {
	app, mux, cid, token, f := controlFixture(t, "cluster_admin")
	f.loseReply = true
	p := prepareCreate(t, mux, cid, token)
	input := fmt.Sprintf(`{"proof":%q,"confirmation":"worker","reason":"test account"}`, p.Proof)
	r := request(mux, "POST", "/api/v1/clusters/"+cid+"/linux-user-controls/execute", input, token)
	var body struct{ Data linuxusers.OperationResult }
	_ = json.Unmarshal(r.Body.Bytes(), &body)
	if r.Code != 200 || body.Data.Status != "unknown" || f.execute != 1 {
		t.Fatal(r.Body.String())
	}
	s := linuxusers.Register(app, &linuxusers.Registry{Clusters: map[string]linuxusers.Profile{cid: {Transport: "local", ManageLocal: true}}}, nil)
	s.SetAdminProvider(f)
	r = request(mux, "GET", "/api/v1/clusters/"+cid+"/linux-user-operations/"+p.OperationID, "", token)
	_ = json.Unmarshal(r.Body.Bytes(), &body)
	if r.Code != 200 || body.Data.Status != "confirmed" || f.execute != 1 || f.status != 1 {
		t.Fatal(r.Body.String())
	}
}
