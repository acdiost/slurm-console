package usagereport

import (
	"encoding/json"
	"testing"

	"slurm-console/backend/internal/slurm/models"
)

func num(v int64) models.Number { return models.Number{Value: &v} }
func tres(id, typ, name, v string) models.HistoryTRES {
	return models.HistoryTRES{ID: id, Type: typ, Name: name, Count: &v}
}
func job(key, id, user string, a, b int64) models.HistoryJob {
	return models.HistoryJob{Key: key, ID: id, Cluster: "alpha", User: user, Account: "project", Partition: "normal", SubmitTime: num(a - 1), StartTime: num(a), EndTime: num(b), ElapsedSeconds: num(b - a), SuspendedSeconds: num(0), States: []string{"COMPLETED"}, AllocatedTRES: []models.HistoryTRES{tres("1", "cpu", "", "4"), tres("2", "mem", "", "1024"), tres("1001", "gres", "gpu", "2"), tres("1002", "gres", "gpu:a100", "2")}}
}
func TestWindowIntersectionExactSumsAndGrouping(t *testing.T) {
	a := job("first", "1", "alice", 50, 150)
	// Steps and requested values must not be added to the allocation.
	a.Steps = []models.HistoryStep{{AllocatedTRES: []models.HistoryTRES{tres("1", "cpu", "", "999")}}}
	a.RequestedTRES = []models.HistoryTRES{tres("1", "cpu", "", "999")}
	b := job("second", "2", "bob", 180, 250)
	b.AllocatedTRES[0] = tres("1", "cpu", "", "9007199254740993")
	c := job("outside", "3", "outside", 20, 100)
	r, e := Analyze(models.HistorySnapshot{Cluster: "alpha", Start: 100, End: 200, Items: []models.HistoryJob{a, b, c}}, "user", 300)
	if e != nil || r.Summary.Records != 2 || r.OutsideRecords != 1 || len(r.Groups) != 2 || *r.Summary.CPU.Seconds != "180143985094820060" || *r.Summary.GPU.Seconds != "140" || *r.Summary.Memory.Seconds != "71680" {
		t.Fatalf("%v %+v", e, r)
	}
	if *r.Groups[0].CPU.Seconds != "200" || r.Groups[0].Key != "alice" {
		t.Fatal(r.Groups)
	}
	r, e = Analyze(models.HistorySnapshot{Cluster: "alpha", Start: 100, End: 200, Items: []models.HistoryJob{a, b}}, "account", 300)
	if e != nil || len(r.Groups) != 1 || r.Groups[0].Records != 2 {
		t.Fatal(e, r)
	}
}
func TestUnknownZeroAndExcludedRecordsRemainVisible(t *testing.T) {
	good := job("ok", "1", "alice", 100, 200)
	good.AllocatedTRES = good.AllocatedTRES[:2] // Missing GPU is unknown, never zero.
	zero := job("zero", "2", "alice", 100, 200)
	zero.AllocatedTRES[2] = tres("1001", "gres", "gpu", "0")
	active := job("active", "3", "alice", 100, 200)
	active.States = []string{"RUNNING"}
	array := job("array-parent", "4", "alice", 100, 200)
	array.ArrayJobID = num(4)
	suspended := job("suspended", "5", "alice", 100, 200)
	suspended.SuspendedSeconds = num(1)
	conflict := job("state-conflict", "6", "alice", 100, 200)
	conflict.States = []string{"COMPLETED", "FAILED"}
	r, e := Analyze(models.HistorySnapshot{Cluster: "alpha", Start: 100, End: 200, Items: []models.HistoryJob{good, zero, active, array, suspended, conflict}}, "user", 300)
	x := r.Summary
	if e != nil || x.CountedRecords != 2 || x.CPU.KnownRecords != 2 || x.CPU.UnknownRecords != 4 || x.GPU.KnownRecords != 1 || x.GPU.UnknownRecords != 5 || *x.GPU.Seconds != "0" || x.Excluded.Active != 1 || x.Excluded.Complex != 1 || x.Excluded.Uncertain != 2 {
		t.Fatal(e, x)
	}
	// Unknown alone remains null, unlike an empty visible result (zero).
	r, _ = Analyze(models.HistorySnapshot{Cluster: "alpha", Start: 100, End: 200, Items: []models.HistoryJob{good}}, "user", 300)
	if r.Summary.GPU.Seconds != nil {
		t.Fatal("missing GPU fabricated")
	}
	r, _ = Analyze(models.HistorySnapshot{Cluster: "alpha", Start: 100, End: 200}, "user", 300)
	if *r.Summary.CPU.Seconds != "0" || len(r.Groups) != 0 {
		t.Fatal("empty semantics", r)
	}
}
func TestDuplicateIntervalsArrayTasksAndAmbiguities(t *testing.T) {
	a := job("a", "1", "alice", 100, 200)
	b := job("b", "1", "alice", 150, 250)
	c := job("c", "1", "alice", 260, 270) // Reused ID, disjoint, kept independent.
	task := job("task", "9", "alice", 100, 200)
	task.ArrayJobID = num(8)
	task.ArrayTaskID = num(0)
	r, e := Analyze(models.HistorySnapshot{Cluster: "alpha", Start: 100, End: 300, Items: []models.HistoryJob{a, b, c, task}}, "partition", 400)
	if e != nil || r.Summary.Excluded.Overlapping != 2 || r.Summary.CountedRecords != 2 || *r.Summary.CPU.Seconds != "440" {
		t.Fatal(e, r)
	}
	cases := []func(*models.HistoryJob){func(j *models.HistoryJob) { j.ElapsedSeconds = num(99) }, func(j *models.HistoryJob) { j.SuspendedSeconds = models.Number{} }, func(j *models.HistoryJob) { j.HetJobID = num(1) }, func(j *models.HistoryJob) { j.States = []string{"COMPLETED", "RESIZING"} }, func(j *models.HistoryJob) { j.EndTime = num(1000) }}
	for _, change := range cases {
		j := job("x", "10", "alice", 100, 200)
		change(&j)
		r, e := Analyze(models.HistorySnapshot{Cluster: "alpha", Start: 100, End: 300, Items: []models.HistoryJob{j}}, "user", 400)
		if e != nil || r.Summary.CPU.Seconds != nil || r.Summary.CPU.UnknownRecords != 1 {
			t.Fatal(e, r)
		}
	}
	a.AllocatedTRES = append(a.AllocatedTRES, tres("3", "cpu", "", "4"))
	r, _ = Analyze(models.HistorySnapshot{Cluster: "alpha", Start: 100, End: 300, Items: []models.HistoryJob{a}}, "user", 400)
	if r.Summary.CPU.Seconds != nil {
		t.Fatal("ambiguous generic TRES summed")
	}
	_, e = Analyze(models.HistorySnapshot{Cluster: "alpha", Start: 100, End: 300, Items: []models.HistoryJob{a, a}}, "user", 400)
	if e == nil {
		t.Fatal("duplicate source identity accepted")
	}
	raw, _ := json.Marshal(r)
	if len(raw) == 0 {
		t.Fatal("unserializable report")
	}
}

func TestReversedIntervalIsUncertainRatherThanOutside(t *testing.T) {
	j := job("reversed", "1", "alice", 400, 200)
	r, e := Analyze(models.HistorySnapshot{Cluster: "alpha", Start: 100, End: 300, Items: []models.HistoryJob{j}}, "user", 500)
	if e != nil || r.OutsideRecords != 0 || r.Summary.Excluded.Uncertain != 1 || r.Summary.CPU.Seconds != nil {
		t.Fatal(e, r)
	}
}
