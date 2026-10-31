package notifications

import (
	"testing"
	"time"
)

func TestTransitionRequiresContinuousEvidenceAndNeverRecoversUnknown(t *testing.T) {
	now := time.Unix(1700000000, 0)
	bad := Observation{Known: true, Breached: true}
	good := Observation{Known: true}
	unknown := Observation{}
	s, status, phase := Transition(State{}, bad, now, 2*time.Minute)
	if status != "pending" || phase != "" {
		t.Fatal(status, phase)
	}
	s, _, _ = Transition(s, bad, now.Add(time.Minute), 2*time.Minute)
	s, status, phase = Transition(s, unknown, now.Add(90*time.Second), 2*time.Minute)
	if status != "unknown" || phase != "" || !s.Pending.IsZero() {
		t.Fatal("unknown continued pending")
	}
	s, status, phase = Transition(s, bad, now.Add(2*time.Minute), 2*time.Minute)
	if status != "pending" || phase != "" {
		t.Fatal("false early fire")
	}
	for i := 3; i <= 4; i++ {
		s, status, phase = Transition(s, bad, now.Add(time.Duration(i)*time.Minute), 2*time.Minute)
	}
	if status != "firing" || phase != "firing" || !s.Active {
		t.Fatal(status, phase)
	}
	s, status, phase = Transition(s, unknown, now.Add(5*time.Minute), 0)
	if status != "unknown" || !s.Active || phase != "" {
		t.Fatal("false recovery")
	}
	s, status, phase = Transition(s, bad, now.Add(6*time.Minute), 0)
	if status != "firing" || phase != "" {
		t.Fatal("duplicate firing")
	}
	s, status, phase = Transition(s, good, now.Add(7*time.Minute), 0)
	if status != "ok" || phase != "recovered" || s.Active {
		t.Fatal("missing recovery")
	}
	_, _, phase = Transition(s, good, now.Add(8*time.Minute), 0)
	if phase != "" {
		t.Fatal("duplicate recovery")
	}
	first, _, _ := Transition(State{}, bad, now, time.Minute)
	_, status, phase = Transition(first, bad, now.Add(3*time.Minute), time.Minute)
	if status != "pending" || phase != "" {
		t.Fatal("stale samples continued timer")
	}
	_, status, phase = Transition(first, bad, now.Add(-time.Minute), 0)
	if status != "firing" || phase != "firing" {
		t.Fatal("zero-hold first current sample invalid")
	}
}
func TestRuleThresholdValidation(t *testing.T) {
	enabled := true
	hold := 2
	in := Input{Config: Config{ClusterID: "cluster", Name: "rule", Kind: "gpu_temperature", Severity: "warning", Enabled: &enabled, Threshold: 85, HoldMinutes: &hold}}
	if in.Validate() != nil {
		t.Fatal("valid rule rejected")
	}
	for _, change := range []func(*Input){func(v *Input) { v.Threshold = 121 }, func(v *Input) { v.Enabled = nil }, func(v *Input) { v.Kind = "command" }, func(v *Input) { n := -1; v.HoldMinutes = &n }, func(v *Input) { v.Severity = "page-root" }, func(v *Input) { v.Name = "unsafe\nname" }} {
		v := in
		change(&v)
		if v.Validate() == nil {
			t.Fatal("invalid rule accepted", v)
		}
	}
}
