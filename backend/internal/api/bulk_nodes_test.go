package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slurm-console/backend/internal/bulkcontrol"
	"slurm-console/backend/internal/clusterconfig"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBulkNodesPreservePerNodeActionsAndAudit(t *testing.T) {
	app, _ := setup(t)
	var mu sync.Mutex
	states := map[string]string{"node1": `["MIXED"]`, "node2": `["MIXED"]`}
	reasons := map[string]string{}
	writes := 0
	registry := connectionRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/slurm/v0.0.43/node/")
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "POST" {
			var in struct {
				State  []string
				Reason string
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			writes++
			if in.State[0] == "DRAIN" {
				states[name] = `["MIXED","DRAIN"]`
				reasons[name] = in.Reason
			} else {
				states[name] = `["MIXED"]`
				reasons[name] = ""
			}
			fmt.Fprint(w, slurmEnvelope(`"warnings":[]`))
			return
		}
		fmt.Fprint(w, slurmEnvelope(strings.ReplaceAll(nodeFields(states[name], reasons[name]), "node1", name)))
	})
	manager := clusterconfig.Register(app, registry)
	cfg, _ := manager.Save("", validConnection())
	actor := user(t, app, "users")
	grant(t, app, actor.Id, "operator", cfg.ID)
	svc := bulkcontrol.For(app)
	for _, action := range []string{"nodes.drain", "nodes.resume"} {
		p, err := svc.Prepare(context.Background(), actor.Id, cfg.ID, action, []string{"node1", "node2"})
		if err != nil || !p.Allowed {
			t.Fatal(err, p)
		}
		if _, err = svc.Execute(actor.Id, cfg.ID, p.ID, "bulk-node-test", bulkcontrol.Input{Confirmation: bulkcontrol.Confirmation(action, 2), Reason: "节点批量测试"}); err != nil {
			t.Fatal(err)
		}
		var result bulkcontrol.View
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			result, err = svc.Get(actor.Id, cfg.ID, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status == "completed" {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if result.Counts["confirmed"] != 2 {
			t.Fatal(result)
		}
		ids := []string{p.Items[0].OperationID, p.Items[1].OperationID}
		recovered, err := svc.Operations(context.Background(), actor.Id, cfg.ID, "node", ids)
		if err != nil || recovered.Items[0].Action != action || recovered.Counts["confirmed"] != 2 {
			t.Fatal(err, recovered)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 4 {
		t.Fatal("duplicate node writes", writes)
	}
	count, _ := app.CountRecords("operation_audits")
	if count != 4 {
		t.Fatal("missing per-node audit", count)
	}
}
