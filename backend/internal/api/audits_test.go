package api_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/rbac"
)

func auditRow(t *testing.T, app core.App, id, actor, cluster, action, status, target, created string) *core.Record {
	t.Helper()
	c, err := app.FindCollectionByNameOrId("operation_audits")
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRecord(c)
	for k, v := range map[string]any{"operation_key": id, "actor_id": actor, "cluster_id": cluster, "action": action, "status": status, "message": "recorded result", "reason": "planned maintenance", "request_id": "request-" + id, "fingerprint": "PRIVATE-FINGERPRINT", "input_hash": "PRIVATE-HASH", "observed_states": []string{"DOWN"}, "job_id": "42", "node_name": target, "created": created} {
		r.Set(k, v)
	}
	date, err := types.ParseDateTime(created)
	if err != nil {
		t.Fatal(err)
	}
	r.SetRaw("created", date)
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	return r
}
func grantAuditOnly(t *testing.T, app core.App, actor string) *core.Record {
	t.Helper()
	c, _ := app.FindCollectionByNameOrId("roles")
	role := core.NewRecord(c)
	role.Set("code", "auditor")
	role.Set("name", "Auditor")
	role.Set("scope", "platform")
	if err := app.Save(role); err != nil {
		t.Fatal(err)
	}
	p, _ := app.FindFirstRecordByData("permissions", "code", "platform.audit.read")
	c, _ = app.FindCollectionByNameOrId("role_permissions")
	b := core.NewRecord(c)
	b.Set("role", role.Id)
	b.Set("permission", p.Id)
	if err := app.Save(b); err != nil {
		t.Fatal(err)
	}
	g, err := rbac.Grant(app, actor, "auditor", "")
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func TestAuditQueryRequiresPlatformPermissionAndRechecksRevocation(t *testing.T) {
	app, mux := setup(t)
	person := user(t, app, "users")
	cluster := cluster(t, app, "history")
	grant(t, app, person.Id, "viewer", cluster.Id)
	token := login(t, mux)
	auditRow(t, app, "old-operation", "deleted-user", cluster.Id, "nodes.drain", "confirmed", "n1", "2026-10-20 12:00:00.000Z")
	for _, path := range []string{"/operation-audits", "/operation-audits/old-operation"} {
		if r := request(mux, "GET", "/api/v1"+path, "", token); r.Code != 403 {
			t.Fatal(r.Body.String())
		}
	}
	binding := grantAuditOnly(t, app, person.Id)
	// An auditor need not hold a live cluster/nodes/jobs read grant.
	if err := app.Delete(cluster); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/operation-audits", "/operation-audits/old-operation"} {
		r := request(mux, "GET", "/api/v1"+path, "", token)
		if r.Code != 200 || !strings.Contains(r.Body.String(), "old-operation") || strings.Contains(r.Body.String(), "PRIVATE") {
			t.Fatalf("history/DTO: %d %s", r.Code, r.Body.String())
		}
	}
	if err := app.Delete(binding); err != nil {
		t.Fatal(err)
	}
	if r := request(mux, "GET", "/api/v1/operation-audits", "", token); r.Code != 403 {
		t.Fatal("revoked permission survived")
	}
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		if r := request(mux, method, "/api/v1/operation-audits/old-operation", "{}", token); r.Code != 404 {
			t.Fatal("audit mutation route exposed")
		}
	}
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		suffix := ""
		if method == "PATCH" || method == "DELETE" {
			suffix = "/" + person.Id
		}
		if r := request(mux, method, "/api/collections/operation_audits/records"+suffix, "{}", token); r.Code == 200 {
			t.Fatal("native audit CRUD exposed")
		}
	}
}
func TestAuditFiltersUseUTCExclusiveEndLiteralSearchAndStablePages(t *testing.T) {
	app, mux := setup(t)
	person := user(t, app, "users")
	grantAuditOnly(t, app, person.Id)
	token := login(t, mux)
	auditRow(t, app, "before", "actor-one", "old-cluster", "nodes.drain", "confirmed", "n0", "2026-10-20 00:00:00.000Z")
	auditRow(t, app, "middle", "actor-one", "old-cluster", "nodes.drain", "processing", "n1%_", "2026-10-20 01:00:00.000Z")
	auditRow(t, app, "at-end", "actor-two", "other", "jobs.cancel", "failed", "n2", "2026-10-20 02:00:00.000Z")
	path := "/api/v1/operation-audits"
	samples := []struct {
		query string
		total int
		first string
	}{
		{"?from=2026-10-20T09:00:00%2B08:00&to=2026-10-20T10:00:00%2B08:00", 1, "middle"},
		{"?cluster=old-cluster&actor=actor-one&action=nodes.drain&status=unknown", 1, "middle"},
		{"?to=0001-01-01T00:00:00Z", 0, ""},
		{"?q=%25_", 1, "middle"}, {"?q=%27%20OR%201%3D1", 0, ""},
		{"?perPage=1&page=2&sort=created&order=asc", 3, "middle"},
	}
	for _, s := range samples {
		r := request(mux, "GET", path+s.query, "", token)
		var body struct {
			Data struct {
				TotalItems int
				Items      []struct{ OperationID, Status, RecordedStatus string }
			}
		}
		if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if r.Code != 200 || body.Data.TotalItems != s.total || s.total > 0 && body.Data.Items[0].OperationID != s.first {
			t.Fatalf("%s: %d %s", s.query, r.Code, r.Body.String())
		}
		if s.first == "middle" && (body.Data.Items[0].Status != "unknown" || body.Data.Items[0].RecordedStatus != "processing") {
			t.Fatal("unfinished intent misrepresented")
		}
	}
}
func TestAuditQueryRejectsInvalidParametersAndDoesNotChangeEvidence(t *testing.T) {
	app, mux := setup(t)
	person := user(t, app, "users")
	grantAuditOnly(t, app, person.Id)
	token := login(t, mux)
	r := auditRow(t, app, "unfinished", person.Id, "gone", "jobs.cancel", "processing", "", "2026-10-20 00:00:00.000Z")
	for _, query := range []string{"?raw=true", "?page=0", "?perPage=101", "?q=a&q=b", "?sort=actor_id", "?order=desc%3Bdelete", "?from=2026-10-20", "?from=2026-10-20T00:00:00.0001Z", "?from=2026-10-21T00:00:00Z&to=2026-10-20T00:00:00Z", "?status=success", "?q=%00"} {
		if res := request(mux, "GET", "/api/v1/operation-audits"+query, "", token); res.Code != 400 {
			t.Fatalf("%s: %d %s", query, res.Code, res.Body.String())
		}
	}
	for range 2 {
		res := request(mux, "GET", "/api/v1/operation-audits/unfinished", "", token)
		if res.Code != 200 {
			t.Fatal(res.Body.String())
		}
	}
	after, _ := app.FindRecordById("operation_audits", r.Id)
	if after.GetString("status") != "processing" || after.GetString("updated") != r.GetString("updated") {
		t.Fatal("audit read reconciled or changed evidence")
	}
	if res := request(mux, "GET", "/api/v1/operation-audits/missing", "", token); res.Code != 404 {
		t.Fatal(res.Body.String())
	}
}
func TestAuditPreservesSnapshotsAndDropsUnreviewedChangeFields(t *testing.T) {
	app, mux := setup(t)
	person := user(t, app, "users")
	grantAuditOnly(t, app, person.Id)
	token := login(t, mux)
	cl := cluster(t, app, "original")
	err := audit.Write(app, audit.Actor{ID: person.Id, Label: "Original actor", Source: "platform", RequestID: "trace"}, audit.Event{Action: "clusters.update", ClusterID: cl.Id, TargetType: "cluster", TargetID: cl.Id, Message: "saved", Changes: []audit.Change{{Field: "enabled", Before: "true", After: "false"}, {Field: "password", After: "PRIVATE-PASSWORD"}, {Field: "endpoint", After: "PRIVATE-ENDPOINT"}}})
	if err != nil {
		t.Fatal(err)
	}
	person.Set("name", "Changed actor")
	_ = app.Save(person)
	cl.Set("name", "Changed cluster")
	_ = app.Save(cl)
	res := request(mux, "GET", "/api/v1/operation-audits", "", token)
	body := res.Body.String()
	if res.Code != 200 || !strings.Contains(body, "Original actor") || !strings.Contains(body, `"clusterName":"original"`) || strings.Contains(body, "PRIVATE") || strings.Contains(body, "Changed actor") || strings.Contains(body, "Changed cluster") {
		t.Fatal(body)
	}
}
func TestConfigurationAndAuditCommitTogetherAndFailureRollsBack(t *testing.T) {
	app, mux := setup(t)
	registry := connectionRegistry(t, writeFixture)
	clusterconfig.Register(app, registry)
	person := user(t, app, "users")
	grant(t, app, person.Id, "super_admin", "")
	token := login(t, mux)
	input := validConnection()
	encoded, _ := json.Marshal(input)
	res := request(mux, "POST", "/api/v1/cluster-configurations", string(encoded), token)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	var body struct{ Data clusterconfig.Configuration }
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	rows, err := app.FindRecordsByFilter("operation_audits", "action='clusters.create'", "", 10, 0)
	if err != nil || len(rows) != 1 || rows[0].GetString("request_id") != res.Header().Get("X-Request-ID") || rows[0].GetString("actor_id") != person.Id {
		t.Fatalf("missing transactional evidence: %v", err)
	}
	input = body.Data.Input
	input.Name = "Changed config"
	encoded, _ = json.Marshal(input)
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error { return fmt.Errorf("audit unavailable") })
	if res := request(mux, "PATCH", "/api/v1/cluster-configurations/"+body.Data.ID, string(encoded), token); res.Code != 500 {
		t.Fatal(res.Body.String())
	}
	saved, _ := app.FindRecordById("clusters", body.Data.ID)
	if saved.GetString("name") == input.Name || saved.GetInt("revision") != body.Data.Revision {
		t.Fatal("configuration survived audit rollback")
	}
}
func TestLocalAccountMutationRollsBackWhenAuditCannotPersist(t *testing.T) {
	app, _ := setup(t)
	c, _ := app.FindCollectionByNameOrId("users")
	record := core.NewRecord(c)
	record.SetEmail("rollback@example.com")
	record.SetPassword(fixturePassword)
	record.Set("active", true)
	record.Set("auth_source", "local")
	app.OnRecordCreate("operation_audits").BindFunc(func(e *core.RecordEvent) error { return fmt.Errorf("offline") })
	if err := audit.SaveLocal(app, record, audit.Event{Action: "users.create", TargetType: "user"}); err == nil {
		t.Fatal("audit failure ignored")
	}
	if _, err := app.FindAuthRecordByEmail("users", "rollback@example.com"); err == nil {
		t.Fatal("unaudited user persisted")
	}
}
