package adapter

import (
	"context"
	"encoding/json"
	"slurm-console/backend/internal/slurm/client"
	"testing"
)

type fixtureReader struct{ envelope client.Envelope }

func (f fixtureReader) Read(context.Context, client.Resource, string) (client.Envelope, error) {
	return f.envelope, nil
}
func TestNumbersPreserveUnsetInfiniteZeroAndIntegerPrecision(t *testing.T) {
	for _, sample := range []struct {
		wire     string
		value    *int64
		infinite bool
	}{
		{`{"set":false,"infinite":false,"number":0}`, nil, false},
		{`{"set":false,"infinite":true,"number":0}`, nil, true},
		{`{"set":false,"infinite":false,"number":18446744073709551615}`, nil, false},
		{`{"set":true,"infinite":false,"number":0}`, integer(0), false},
		{`{"set":true,"infinite":false,"number":9007199254740993}`, integer(9007199254740993), false},
	} {
		var n noVal
		if err := json.Unmarshal([]byte(sample.wire), &n); err != nil {
			t.Fatal(err)
		}
		if n.Infinite != sample.infinite || (n.Value == nil) != (sample.value == nil) || n.Value != nil && *n.Value != *sample.value {
			t.Fatalf("numeric semantics lost: %s", sample.wire)
		}
	}
	for _, wire := range []string{`{"number":2}`, `{"set":true}`, `-1`, `1.2`, `{"set":true,"number":-1}`} {
		var n noVal
		if json.Unmarshal([]byte(wire), &n) == nil {
			t.Fatalf("accepted invalid numeric form: %s", wire)
		}
	}
}
func TestNumberResetDoesNotRetainAFormerValue(t *testing.T) {
	var n noVal
	for _, wire := range []string{`2`, `{"set":false,"infinite":false,"number":0}`} {
		if err := json.Unmarshal([]byte(wire), &n); err != nil {
			t.Fatal(err)
		}
	}
	if n.Value != nil || n.Infinite {
		t.Fatal("unset number retained a previous allocation")
	}
}
func integer(n int64) *int64 { return &n }
func TestAdapterPreservesFlagsAndDropsUnselectedSensitiveFields(t *testing.T) {
	envelope := client.Envelope{Jobs: json.RawMessage(`[{"job_id":4294967295,"name":"compute","job_state":["RUNNING","CONFIGURING"],"cpus":{"set":true,"infinite":false,"number":2},"script":"private batch script","environment":["private values"]}]`)}
	a, err := New(fixtureReader{envelope}, Version)
	if err != nil {
		t.Fatal(err)
	}
	values, err := a.GetJobs(context.Background())
	if err != nil || len(values) != 1 || values[0].ID != "4294967295" || len(values[0].States) != 2 {
		t.Fatalf("job normalization: %v %+v", err, values)
	}
	data, _ := json.Marshal(values)
	var normalized []map[string]any
	_ = json.Unmarshal(data, &normalized)
	if normalized[0]["script"] != nil || normalized[0]["environment"] != nil {
		t.Fatal("sensitive wire fields escaped DTO boundary")
	}
	if _, err := New(fixtureReader{}, "v0.0.44"); client.Code(err) != "api_unsupported" {
		t.Fatal("unverified adapter version accepted")
	}
}
func TestAdapterRejectsIncompleteAndDuplicateResources(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `[{"name":"missing id"}]`, `[{"job_id":1,"job_state":[]}]`, `[{"job_id":1,"job_state":["RUNNING"]},{"job_id":1,"job_state":["RUNNING"]}]`} {
		a, _ := New(fixtureReader{client.Envelope{Jobs: json.RawMessage(body)}}, Version)
		if _, err := a.GetJobs(context.Background()); client.Code(err) != "invalid_response" {
			t.Fatalf("accepted incomplete response: %s %v", body, err)
		}
	}
	a, _ := New(fixtureReader{client.Envelope{Nodes: json.RawMessage(`[{"name":"node1","state":["DOWN"]}]`)}}, Version)
	if _, err := a.GetNodes(context.Background()); client.Code(err) != "invalid_response" {
		t.Fatal("missing capacity represented as zero")
	}
}

func TestPartitionConstraintsUseExplicitMemoryFieldsAndPreserveLimits(t *testing.T) {
	a, _ := New(fixtureReader{client.Envelope{Partitions: json.RawMessage(`[{"name":"normal","partition":{"state":["DRAIN"]},"nodes":{"configured":"n[1-2]","total":2,"allowed_allocation":"submit1"},"cpus":{"total":16},"defaults":{"memory_per_cpu":9223372036854775809,"partition_memory_per_cpu":{"set":false,"infinite":false,"number":0},"partition_memory_per_node":{"set":true,"infinite":false,"number":0},"time":{"set":false,"infinite":false}},"maximums":{"time":{"infinite":true,"set":false},"nodes":{"set":true,"number":4},"partition_memory_per_cpu":{"set":true,"number":1024},"oversubscribe":{"jobs":2,"flags":["FORCE"]}},"accounts":{"allowed":"research","deny":"blocked"},"qos":{"assigned":"normal"},"minimums":{"nodes":0},"priority":{"tier":3,"job_factor":1},"grace_time":0,"tres":{"configured":"cpu=16"}}]`)}}, Version)
	values, err := a.GetPartitions(context.Background())
	if err != nil || len(values) != 1 {
		t.Fatalf("partition normalization: %v", err)
	}
	p := values[0]
	if p.DefaultMemoryPerCPUMiB.Value != nil || p.DefaultMemoryPerNodeMiB.Value == nil || *p.DefaultMemoryPerNodeMiB.Value != 0 || p.DefaultTimeMinutes.Value != nil || !p.MaxTimeMinutes.Infinite || *p.MaxMemoryPerCPUMiB.Value != 1024 || *p.MinNodes.Value != 0 || *p.OversubscribeJobs.Value != 2 || p.OversubscribeFlags[0] != "FORCE" || p.AllowedAccounts != "research" || p.DeniedAccounts != "blocked" || p.AllowedAllocationNodes != "submit1" || p.SelectType == nil {
		t.Fatalf("lost constraints: %+v", p)
	}
}
