package client

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTRESWireWhitelistAndDecimalPrecision(t *testing.T) {
	for _, kind := range []string{"qos", "association"} {
		v := &TRESSpec{Kind: kind, TargetID: "4", Account: "lab", User: "alice", ResourceID: "1", Bucket: "group_total", Mode: "set", Value: "9007199254740993"}
		action := "association_tres_update"
		m := AccountingMutation{Cluster: "cluster", TRES: v, TRESEntries: []TRESWireEntry{{ID: 1, Type: "cpu", Count: 9007199254740993}, {ID: 2, Type: "mem", Count: 512}}}
		if kind == "qos" {
			m.TRESEntries = m.TRESEntries[:1]
			action = "qos_tres_update"
			v.Name = "lab"
			v.Account = ""
			v.User = ""
			m.QOSPreemptModes = []string{"REQUEUE"}
		}
		if kind == "association" {
			m.TRESLists = map[string][]TRESWireEntry{}
			for _, k := range TRESBuckets(kind) {
				m.TRESLists[k] = []TRESWireEntry{}
			}
			m.TRESLists[v.Bucket] = m.TRESEntries
		}
		m.Action = action
		method, path, body, ok := tresWrite("v0.0.43", m)
		r := localRequest{Method: method, Path: path, Version: "v0.0.43", Body: body, Write: true}
		if !ok || !validLocalRequest(r) || !strings.Contains(string(body), "9007199254740993") {
			t.Fatal(string(body))
		}
		var raw map[string]any
		d := json.NewDecoder(strings.NewReader(string(body)))
		d.UseNumber()
		_ = d.Decode(&raw)
		key := "associations"
		if kind == "qos" {
			key = "qos"
		}
		target := raw[key].([]any)[0].(map[string]any)
		target["shares_raw"] = 100
		r.Body, _ = json.Marshal(raw)
		if validLocalRequest(r) {
			t.Fatal("unreviewed policy field accepted")
		}
		if kind == "association" {
			delete(target, "shares_raw")
			delete(target, "id")
			r.Body, _ = json.Marshal(raw)
			if validLocalRequest(r) {
				t.Fatal("association without ID accepted")
			}
		}
		v.Value = "9223372036854775808"
		if v.Valid() {
			t.Fatal("overflow accepted")
		}
		v.Value = "1,2"
		if v.Valid() {
			t.Fatal("CSV value accepted")
		}
	}
}
