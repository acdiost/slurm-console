package overview

import (
	"math"
	"slurm-console/backend/internal/slurm/models"
	"testing"
)

func TestNodeSummarySeparatesAllocationIdleAndUnavailableWithoutPartitionDuplication(t *testing.T) {
	nodes := []models.Node{
		{Name: "healthy", States: []string{"MIXED", "MIXED"}, CPUs: 8, EffectiveCPUs: 6, AllocatedCPUs: 2, MemoryMiB: 1000, AllocatedMemoryMiB: 250, GRES: "gpu:a100:4(S:0)", GRESUsed: "gpu:a100:1(IDX:0)", Partitions: []string{"a", "b"}},
		{Name: "blocked", States: []string{"MIXED", "DRAIN"}, CPUs: 4, EffectiveCPUs: 4, AllocatedCPUs: 1, MemoryMiB: 500, AllocatedMemoryMiB: 100, GRES: "gpu:4", GRESUsed: "gpu:2(IDX:0,2)", Reason: "maintenance"},
	}
	result, err := NodeSummary(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if result.CPUs != (Resource{Total: 12, Allocated: 3, Idle: 4, Unavailable: 5}) {
		t.Fatalf("CPU accounting: %+v", result.CPUs)
	}
	if result.MemoryMiB != (Resource{Total: 1500, Allocated: 350, Idle: 750, Unavailable: 400}) {
		t.Fatalf("memory accounting: %+v", result.MemoryMiB)
	}
	if *result.GPUs.Total != 8 || *result.GPUs.Allocated != 3 || *result.GPUs.Idle != 3 || *result.GPUs.Unavailable != 2 {
		t.Fatalf("GPU accounting: %+v", result.GPUs)
	}
	if result.Total != 2 || result.Mixed != 1 || result.Unavailable != 1 || len(result.Issues) != 1 {
		t.Fatal("nodes double counted")
	}
	for _, state := range result.States {
		if state.State == "MIXED" && state.Count != 2 {
			t.Fatal("repeated flag counted twice")
		}
	}
}
func TestGPUCountsPreserveUnknownAllocationAndParseAnnotatedComponents(t *testing.T) {
	for _, sample := range []struct {
		input string
		count int64
		known bool
	}{
		{"gpu:a100:2(IDX:0,2),gpu:h100:1(IDX:1)", 3, true},
		{"gpu:4,shard:20(-/5,5/5)", 4, true},
		{"gpu:4,gpu:a100:4", 0, false},
		{"gpu:a100:many", 0, false},
		{"gpu:2(IDX:0,1", 0, false},
		{"", 0, true},
	} {
		count, known := gpuCount(sample.input, false)
		if count != sample.count || known != sample.known {
			t.Fatalf("parse %q => %d %v", sample.input, count, known)
		}
	}
	result, err := NodeSummary([]models.Node{{Name: "gpu", States: []string{"IDLE"}, GRES: "gpu:2"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.GPUs.Total == nil || *result.GPUs.Total != 2 || result.GPUs.Allocated != nil || result.GPUs.UnknownNodes != 1 {
		t.Fatal("unknown allocation represented as zero")
	}
	result, err = NodeSummary(nil)
	if err != nil || result.GPUs.Total == nil || *result.GPUs.Total != 0 || len(result.States) != 0 {
		t.Fatal("empty visible node set not represented correctly")
	}
}
func TestResourceSummaryRejectsInconsistentOrOverflowedTotalsAndBoundsIssues(t *testing.T) {
	for _, nodes := range [][]models.Node{
		{{CPUs: 1, EffectiveCPUs: 1, AllocatedCPUs: 2}},
		{{MemoryMiB: 1, AllocatedMemoryMiB: 2}},
		{{CPUs: math.MaxInt64, EffectiveCPUs: math.MaxInt64}, {CPUs: 1, EffectiveCPUs: 1}},
	} {
		if _, err := NodeSummary(nodes); err == nil {
			t.Fatal("invalid capacities produced a plausible total")
		}
	}
	nodes := make([]models.Node, 10)
	for i := range nodes {
		nodes[i] = models.Node{Name: string(rune('a' + i)), States: []string{"DOWN"}}
	}
	result, err := NodeSummary(nodes)
	if err != nil || result.Unavailable != 10 || len(result.Issues) != 5 {
		t.Fatal("unbounded node preview or lost total")
	}
}
func TestJobBucketsAreExclusiveAndFailuresIncludeTerminations(t *testing.T) {
	result := JobSummary([]models.Job{
		{States: []string{"RUNNING", "COMPLETING"}},
		{States: []string{"RUNNING", "RUNNING"}},
		{States: []string{"PENDING"}},
		{States: []string{"TIMEOUT"}},
		{States: []string{"OUT_OF_MEMORY"}},
		{States: []string{"COMPLETED"}},
		{States: []string{"CANCELLED"}},
		{States: []string{"FUTURE_STATE"}},
	})
	if result.Total != 8 || result.Running != 1 || result.Completing != 1 || result.Pending != 1 || result.Failed != 2 || result.Completed != 1 || result.Cancelled != 1 || result.Other != 1 {
		t.Fatalf("job buckets: %+v", result)
	}
}
