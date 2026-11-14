package adapter

import (
	"context"
	"encoding/json"
	"testing"

	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"slurm-console/backend/internal/slurm/protocol"
)

// Fixtures select fields from SchedMD's tagged data_parser contracts; missing
// optional newer fields stay unknown instead of preventing older REST reads.
func TestControllerReadsNormalizeEveryReviewedVersion(t *testing.T) {
	for _, version := range protocol.Versions() {
		t.Run(version, func(t *testing.T) {
			env := client.Envelope{
				Pings:      json.RawMessage(`[{"hostname":"controller","pinged":"UP","mode":"primary"}]`),
				Jobs:       json.RawMessage(`[{"job_id":42,"name":"compute","user_name":"alice","job_state":["RUNNING"],"cpus":{"set":true,"infinite":false,"number":4},"node_count":{"set":true,"number":1},"time_limit":{"set":false,"infinite":true,"number":0},"environment":["private"],"script":"private"}]`),
				Nodes:      json.RawMessage(`[{"name":"node1","state":["IDLE"],"cpus":16,"effective_cpus":14,"real_memory":32000,"alloc_cpus":0,"alloc_memory":0,"features":["avx2"],"free_mem":{"set":false,"infinite":false,"number":0}}]`),
				Partitions: json.RawMessage(`[{"name":"normal","partition":{"state":["UP"]},"nodes":{"configured":"node1","total":1},"cpus":{"total":16},"defaults":{"partition_memory_per_cpu":{"set":true,"number":1024}},"maximums":{"time":{"set":false,"infinite":true,"number":0}}}]`),
			}
			a, err := New(fixtureReader{env}, version)
			if err != nil {
				t.Fatal(err)
			}
			health, err := a.Health(context.Background())
			if err != nil || health.APIVersion != version || health.Status != "connected" {
				t.Fatal(health, err)
			}
			jobs, err := a.GetJobs(context.Background())
			if err != nil || len(jobs) != 1 || *jobs[0].CPUs.Value != 4 || !jobs[0].TimeLimitMinutes.Infinite {
				t.Fatal(jobs, err)
			}
			nodes, err := a.GetNodes(context.Background())
			if err != nil || len(nodes) != 1 || nodes[0].EffectiveCPUs != 14 || nodes[0].FreeMemoryMiB.Value != nil {
				t.Fatal(nodes, err)
			}
			partitions, err := a.GetPartitions(context.Background())
			if err != nil || len(partitions) != 1 || *partitions[0].DefaultMemoryPerCPUMiB.Value != 1024 || !partitions[0].MaxTimeMinutes.Infinite || partitions[0].SelectType == nil {
				t.Fatal(partitions, err)
			}
			accounts, err := New(accountingReader{}, version)
			if err != nil {
				t.Fatal(err)
			}
			if values, err := accounts.GetAccounts(context.Background()); err != nil || len(values) != 1 {
				t.Fatal(values, err)
			}
		})
	}
}

func TestHealthAcceptsNewBooleanFieldsAndRejectsConflictingStates(t *testing.T) {
	for _, sample := range []struct{ raw, status string }{{`[{"hostname":"c","responding":true,"primary":true}]`, "connected"}, {`[{"hostname":"c","responding":false,"primary":false}]`, "controller_down"}} {
		a, _ := New(fixtureReader{client.Envelope{Pings: json.RawMessage(sample.raw)}}, "v0.0.44")
		if h, err := a.Health(context.Background()); err != nil || h.Status != sample.status {
			t.Fatal(h, err)
		}
	}
	a, _ := New(fixtureReader{client.Envelope{Pings: json.RawMessage(`[{"hostname":"c","pinged":"UP","responding":false}]`)}}, "v0.0.44")
	if _, err := a.Health(context.Background()); client.Code(err) != "invalid_response" {
		t.Fatal("contradictory controller state accepted", err)
	}
}

func TestLegacyQOSDoesNotReportUnsupportedQuotaAsUnset(t *testing.T) {
	for _, version := range protocol.Versions() {
		target, err := projectTRES(models.TRESTarget{Kind: "qos", Raw: json.RawMessage(`{"id":1,"name":"normal","limits":{"max":{"tres":{"total":[{"id":1,"count":4}]}}}}`)}, version)
		if err != nil {
			t.Fatal(err)
		}
		_, exists := target.Limits["group_minutes"]
		if exists != protocol.QOSGroupMinutes(version) {
			t.Fatal("unsupported quota presented as unset", version, target)
		}
		if !exists && len(target.UnsupportedBuckets) != 1 {
			t.Fatal("missing capability metadata", version)
		}
	}
}
