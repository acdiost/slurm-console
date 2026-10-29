package client

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQOSWireOnlyAllowsReviewedSinglePolicyFields(t *testing.T) {
	zero := uint32(0)
	m := AccountingMutation{Action: "qos_create", Cluster: "cluster", QOS: &QOSSpec{Name: "lab", Description: "Lab", Priority: 0, UsageFactor: 1, MaxJobsPerUser: &zero}, QOSPreemptModes: []string{"DISABLED"}}
	method, path, body, ok := qosWrite("v0.0.43", m)
	r := localRequest{Method: method, Path: path, Body: body, Write: true, Version: "v0.0.43"}
	if !ok || !validLocalRequest(r) {
		t.Fatal(string(body))
	}
	for _, bad := range []string{strings.Replace(string(body), `"NOT_SET"`, `"RELATIVE"`, 1), strings.Replace(string(body), `"name":"lab"`, `"name":"lab,normal"`, 1), strings.Replace(string(body), `"mode":["DISABLED"]`, `"mode":["DISABLED"],"list":["normal"]`, 1)} {
		r.Body = []byte(bad)
		if validLocalRequest(r) {
			t.Fatal("unsafe QOS write accepted", bad)
		}
	}
	m.QOS = nil
	if m.Valid() {
		t.Fatal("nil policy accepted")
	}
	var q QOSSpec
	if json.Unmarshal([]byte(`{"id":"","name":"lab","description":"Lab","priority":0,"usageFactor":1}`), &q) == nil {
		t.Fatal("missing limits can clear existing settings")
	}
	for _, path := range []string{"/slurmdb/v0.0.43/qos/", "/slurm/v0.0.43/shares"} {
		if !validLocalRequest(localRequest{Method: "GET", Path: path, Version: "v0.0.43"}) {
			t.Fatal(path)
		}
	}
	if validLocalRequest(localRequest{Method: "DELETE", Path: "/slurmdb/v0.0.43/qos/normal", Version: "v0.0.43", Write: true}) {
		t.Fatal("QOS deletion allowed")
	}
}
