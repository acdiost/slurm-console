package capacity

import (
	"slurm-console/backend/internal/overview"
	"slurm-console/backend/internal/slurm/models"
	"testing"
	"time"
)

func TestAnalyzeSharedPartitionsHeadroomAndUnknownGPU(t *testing.T) {
	nodes := []models.Node{{Name: "n1", States: []string{"MIXED"}, CPUs: 8, EffectiveCPUs: 6, AllocatedCPUs: 2, MemoryMiB: 1000, AllocatedMemoryMiB: 250, GRES: "gpu:2", GRESUsed: "gpu:1", Partitions: []string{"a", "b", "a"}}, {Name: "n2", States: []string{"IDLE", "NOT_RESPONDING"}, CPUs: 4, EffectiveCPUs: 4, MemoryMiB: 500, Partitions: []string{"b"}}}
	q := Query{Sort: "name", Order: "asc", Page: 1, PerPage: 20}
	r, e := Analyze(nodes, []models.Partition{{Name: "a", States: []string{"UP"}}, {Name: "b", States: []string{"DOWN"}}}, true, q)
	if e != nil {
		t.Fatal(e)
	}
	if r.Totals.CPUs != (overview.Resource{Total: 12, Allocated: 2, Idle: 4, Unavailable: 6}) || r.Totals.Nodes != 2 || r.Totals.UnavailableNodes != 1 {
		t.Fatal(r)
	}
	if r.Partitions[0].Frame.CPUs.Total != 8 || r.Partitions[1].Frame.CPUs.Total != 12 || r.TotalItems != 2 {
		t.Fatal("partition double count", r.Partitions)
	}
	if r.Headroom.LargestCPUIdle != 4 || r.Headroom.LargestMemoryIdleMiB != 750 || r.Headroom.CPUIdleBuckets[1].Nodes != 1 || r.Headroom.StateAvailableNodes != 1 {
		t.Fatal(r.Headroom)
	}
	nodes[0].GRESUsed = ""
	q.Q = "a"
	r, e = Analyze(nodes, nil, false, q)
	if e != nil || r.TotalItems != 1 || r.Totals.GPUs.Allocated != nil || r.Totals.GPUs.UnknownNodes != 1 || r.Partitions[0].States != nil {
		t.Fatal("unknown masked", r, e)
	}
	nodes = append(nodes, nodes[0])
	if _, e := Analyze(nodes, nil, false, q); e == nil {
		t.Fatal("duplicate nodes accepted")
	}
	q.Sort = "nodes|cpuTotal"
	if q.Valid() {
		t.Fatal("unknown sort allowed")
	}
}
func TestTrendWeightedSamplesGapsAndZeroCapacity(t *testing.T) {
	end := time.Unix(1700000000, 0).Unix() / Step * Step
	start := end - 3600
	frame := func(total, allocated, idle int64) *Frame {
		return &Frame{Version: 1, Nodes: 1, CPUs: overview.Resource{Total: total, Allocated: allocated, Idle: idle, Unavailable: total - allocated - idle}, GPUs: overview.GPU{UnknownNodes: 1}}
	}
	makeSample := func(bucket int64, f *Frame) Sample {
		return Sample{Bucket: bucket, ObservedAt: time.Unix(bucket+10, 0).UTC().Format(time.RFC3339), Frame: f}
	}
	data := []Sample{makeSample(start, frame(8, 4, 4)), makeSample(start+Step, frame(16, 16, 0)), makeSample(end-Step, frame(8, 0, 0))}
	h, e := Trend(start, end, data)
	if e != nil {
		t.Fatal(e)
	}
	if h.AvailableSamples != 3 || h.ExpectedSamples != 12 || h.Points[2].Frame != nil || h.CPUs.WeightedAllocatedPercent == nil || *h.CPUs.WeightedAllocatedPercent != 62.5 || *h.CPUs.PeakAllocatedPercent != 100 || h.GPUs.WeightedAllocatedPercent != nil || h.GPUs.KnownSamples != 0 {
		t.Fatal(h)
	}
	empty, e := Trend(start, end, nil)
	if e != nil || empty.CPUs.WeightedAllocatedPercent != nil || empty.CPUs.MinimumIdle != nil {
		t.Fatal("missing samples became zero", empty, e)
	}
	bad := data[0]
	bad.ObservedAt = time.Unix(bad.Bucket-1, 0).UTC().Format(time.RFC3339)
	if _, e := Trend(start, end, []Sample{bad}); e == nil {
		t.Fatal("mis-bucketed sample accepted")
	}
	if _, e := Trend(start, end, []Sample{data[0], data[0]}); e == nil {
		t.Fatal("duplicate bucket accepted")
	}
	zero := FromSummary(overview.Nodes{GPUs: overview.GPU{Total: new(int64), Allocated: new(int64), Idle: new(int64), Unavailable: new(int64)}})
	z, e := Trend(start, end, []Sample{makeSample(start, &zero)})
	if e != nil || z.CPUs.WeightedAllocatedPercent != nil || z.CPUs.KnownSamples != 1 {
		t.Fatal("zero denominator", z, e)
	}
	a, b, e := Window("7d", time.Unix(end+299, 0))
	if e != nil || b-a != Retention || b != end {
		t.Fatal(a, b, e)
	}
	if _, _, e := Window("30d", time.Now()); e == nil {
		t.Fatal("unbounded window")
	}
}
