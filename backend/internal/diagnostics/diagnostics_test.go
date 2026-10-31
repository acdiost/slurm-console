package diagnostics

import (
	"fmt"
	"slurm-console/backend/internal/slurm/models"
	"strings"
	"testing"
)

func n(v int64) models.Number { return models.Number{Value: &v} }
func str(s string) *string    { return &s }
func has(r Report, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}
func checkEvidence(t *testing.T, r Report) {
	t.Helper()
	seen := map[string]bool{}
	for _, f := range r.Evidence {
		if seen[f.ID] {
			t.Fatal("duplicate evidence")
		}
		seen[f.ID] = true
	}
	for _, f := range r.Findings {
		for _, id := range f.EvidenceIDs {
			if !seen[id] {
				t.Fatal("dangling evidence", id)
			}
		}
	}
}
func TestCurrentPendingDoesNotUseForecastAsExecution(t *testing.T) {
	j := models.Job{ID: "42", States: []string{"PENDING"}, Reason: "Resources", SubmitTime: n(1700000000), StartTime: n(1700009000), EndTime: n(1700010000)}
	r := Current(j, "2023-11-14T22:15:00Z")
	if r.Category != "waiting" || r.Timeline.ObservedRunSeconds != nil || r.Timeline.SubmitToStartSeconds != nil || r.Timeline.SinceSubmitSeconds == nil || *r.Timeline.SinceSubmitSeconds != 100 || !has(r, "pending_reason") {
		t.Fatal(r)
	}
	checkEvidence(t, r)
	for _, reason := range []string{"Priority", "Dependency", "DependencyNeverSatisfied", "BeginTime", "JobHeldAdmin", "AssocGrpCpuLimit", "QOSMaxWallDurationPerJobLimit", "PartitionDown", "ReqNodeNotAvail", "InvalidAccount", "UnrecognizedCode"} {
		j.Reason = reason
		r = Current(j, "2023-11-14T22:15:00Z")
		if !has(r, "pending_reason") {
			t.Fatal(reason)
		}
		checkEvidence(t, r)
	}
}
func TestStateFlagsRemainDistinctAndConflictsDoNotInferPhase(t *testing.T) {
	for _, tc := range []struct {
		states         []string
		category, code string
	}{{[]string{"RUNNING", "CONFIGURING"}, "starting", "configuring"}, {[]string{"RUNNING", "POWER_UP_NODE"}, "starting", "power_up_node"}, {[]string{"COMPLETED", "COMPLETING"}, "ending", "completing"}, {[]string{"FAILED", "LAUNCH_FAILED"}, "failure", "launch_failed"}, {[]string{"PENDING", "REQUEUE_HOLD"}, "waiting", "requeue_hold"}, {[]string{"FAILED", "COMPLETED"}, "unknown", "state_conflict"}, {[]string{"FUTURE_FLAG"}, "unknown", "unknown_state"}} {
		r := Current(models.Job{ID: "42", States: tc.states, SubmitTime: n(1700000000), StartTime: n(1700000050)}, "2023-11-14T22:15:00Z")
		if r.Category != tc.category || !has(r, tc.code) {
			t.Fatal(r)
		}
		if tc.category == "starting" && r.Timeline.ObservedRunSeconds != nil {
			t.Fatal("booting counted as runtime")
		}
		checkEvidence(t, r)
	}
	for _, state := range []string{"OUT_OF_MEMORY", "TIMEOUT", "FAILED", "NODE_FAIL", "BOOT_FAIL", "DEADLINE", "CANCELLED", "PREEMPTED", "SUSPENDED", "REVOKED"} {
		r := Current(models.Job{ID: "42", States: []string{state}}, "2023-11-14T22:15:00Z")
		if !has(r, strings.ToLower(state)) {
			t.Fatal(state, r)
		}
		checkEvidence(t, r)
	}
}
func completed() models.HistoryJob {
	cpu := 12.0
	return models.HistoryJob{Key: "record-one", ID: "42", States: []string{"COMPLETED"}, SubmitTime: n(1700000000), StartTime: n(1700000010), EndTime: n(1700000070), ElapsedSeconds: n(60), SuspendedSeconds: n(0), TotalCPUSeconds: &cpu, Exit: models.HistoryExit{Code: n(0), Signal: n(0)}, DerivedExit: models.HistoryExit{Code: n(0), Signal: n(0)}, AllocatedTRES: []models.HistoryTRES{{ID: "1", Type: "cpu", Count: str("2")}}}
}
func TestAccountingKeepsRecordStepsAndExitEvidenceSeparate(t *testing.T) {
	j := completed()
	j.DerivedExit.Code = n(2)
	for i := 0; i < 25; i++ {
		j.Steps = append(j.Steps, models.HistoryStep{ID: fmt.Sprintf("42.%d", i), Name: "step", States: []string{"FAILED"}, Exit: models.HistoryExit{Code: n(2), Signal: n(0)}, TotalCPUSeconds: new(float64)})
	}
	r := Accounting(j, "2023-11-14T22:15:00Z")
	if !strings.Contains(r.Headline, "存在异常") || r.Findings[0].Code != "derived_exit" || r.Identity.RecordKey != "record-one" || r.Category != "completed" || !has(r, "derived_exit") || !has(r, "step_failures") || r.Coverage.TotalSteps != 25 || r.Coverage.AbnormalSteps != 25 || r.Coverage.DisplayedSteps != 20 || has(r, "low_cpu_accounting") {
		t.Fatal(r)
	}
	if r.Metrics.TotalCPUSeconds == nil || *r.Metrics.TotalCPUSeconds != 12 {
		t.Fatal("step CPU summed into job", r)
	}
	if r.Timeline.ObservedRunSeconds != nil || r.Timeline.SinceSubmitSeconds != nil {
		t.Fatal("historical runtime extrapolated")
	}
	checkEvidence(t, r)
}
func TestCPUApproximationPreservesUnknownZeroAndRejectsInvalidDenominators(t *testing.T) {
	j := completed()
	r := Accounting(j, "2023-11-14T22:15:00Z")
	if r.Metrics.CPUAccountingPercent == nil || *r.Metrics.CPUAccountingPercent != 10 || !has(r, "low_cpu_accounting") {
		t.Fatal(r)
	}
	for _, modify := range []func(*models.HistoryJob){func(j *models.HistoryJob) { j.TotalCPUSeconds = nil }, func(j *models.HistoryJob) { j.ElapsedSeconds = n(0) }, func(j *models.HistoryJob) { j.SuspendedSeconds = models.Number{} }, func(j *models.HistoryJob) { j.SuspendedSeconds = n(1) }, func(j *models.HistoryJob) { j.ArrayJobID = n(7); j.ArrayTaskID = n(0) }, func(j *models.HistoryJob) { j.HetJobID = n(7) }, func(j *models.HistoryJob) { j.States = []string{"RUNNING"} }, func(j *models.HistoryJob) { j.States = append(j.States, "REQUEUED") }, func(j *models.HistoryJob) { j.States = append(j.States, "RESIZING") }, func(j *models.HistoryJob) { j.AllocatedTRES = append(j.AllocatedTRES, j.AllocatedTRES[0]) }, func(j *models.HistoryJob) { v := 121.0; j.TotalCPUSeconds = &v }} {
		j := completed()
		modify(&j)
		r := Accounting(j, "2023-11-14T22:15:00Z")
		if r.Metrics.CPUAccountingPercent != nil || has(r, "low_cpu_accounting") {
			t.Fatal("unsafe ratio", r)
		}
		checkEvidence(t, r)
	}
	j = completed()
	zero := 0.0
	j.TotalCPUSeconds = &zero
	r = Accounting(j, "2023-11-14T22:15:00Z")
	if r.Metrics.CPUAccountingPercent == nil || *r.Metrics.CPUAccountingPercent != 0 || has(r, "low_cpu_accounting") {
		t.Fatal("zero became waste or missing", r)
	}
	j = completed()
	j.Exit.Signal = n(9)
	r = Accounting(j, "2023-11-14T22:15:00Z")
	if !has(r, "job_exit") || has(r, "low_cpu_accounting") {
		t.Fatal(r)
	}
	if !strings.Contains(strings.Join(r.Limitations, " "), "零值可能来自采样缺失") {
		t.Fatal("missing caveats")
	}
}
