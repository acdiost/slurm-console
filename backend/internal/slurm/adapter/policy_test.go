package adapter

import (
	"context"
	"encoding/json"
	"slurm-console/backend/internal/slurm/client"
	"strings"
	"testing"
)

func TestQOSNumbersAndPrivatePolicyFingerprint(t *testing.T) {
	raw := `[{"id":1,"name":"normal","description":"Normal","priority":{"set":true,"number":0},"usage_factor":{"set":true,"number":0},"limits":{"max":{"jobs":{"active_jobs":{"per":{"user":{"set":false,"infinite":true}}}},"wall_clock":{"per":{"job":{"set":false,"infinite":false}}}}},"flags":[],"preempt":{"mode":["DISABLED"]},"unexposed_setting":"private"},{"id":2,"name":"removed","flags":["DELETED"]}]`
	a, _ := New(fixtureReader{client.Envelope{QOS: json.RawMessage(raw)}}, Version)
	v, err := a.AllQOS(context.Background())
	if err != nil || len(v) != 1 || v[0].Priority.Value == nil || *v[0].Priority.Value != 0 || v[0].UsageFactor.Value == nil || *v[0].UsageFactor.Value != 0 || !v[0].MaxJobsPerUser.Infinite || v[0].MaxWallMinutes.Value != nil || v[0].MaxWallMinutes.Infinite {
		t.Fatal(v, err)
	}
	encoded, _ := json.Marshal(v)
	if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), v[0].Fingerprint) {
		t.Fatal("unreviewed policy escaped DTO boundary")
	}
	a, _ = New(fixtureReader{client.Envelope{QOS: json.RawMessage(strings.Replace(raw, "private", "changed", 1))}}, Version)
	changed, err := a.AllQOS(context.Background())
	if err != nil || changed[0].Fingerprint == v[0].Fingerprint {
		t.Fatal("advanced policy not covered by confirmation snapshot")
	}
}
func TestFairshareDecimalPreservesUnsetInfiniteAndZero(t *testing.T) {
	var n decimal
	for _, v := range []struct {
		Raw      string
		Value    *float64
		Infinite bool
	}{
		{`{"set":true,"number":0}`, new(float64), false},
		{`{"set":false,"infinite":false,"number":0}`, nil, false},
		{`{"set":false,"infinite":true,"number":0}`, nil, true},
		{`null`, nil, false},
	} {
		if err := json.Unmarshal([]byte(v.Raw), &n); err != nil || (n.Value == nil) != (v.Value == nil) || n.Infinite != v.Infinite {
			t.Fatal(v.Raw, n, err)
		}
	}
	for _, raw := range []string{`{"number":0}`, `{"set":true}`, `{"set":true,"number":-1}`, `{"set":true,"number":1e999}`} {
		if json.Unmarshal([]byte(raw), &n) == nil {
			t.Fatal("invalid Fairshare number accepted", raw)
		}
	}
}
