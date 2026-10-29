package client

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestAccountingIPCRestrictsSelectorsAndMutationFields(t *testing.T) {
	for _, m := range []AccountingMutation{
		{Action: "account_create", Cluster: "alpha", Account: "research", Description: "Research", Organization: "Lab"},
		{Action: "account_update", Cluster: "alpha", Account: "research", Description: "Changed", Organization: "Lab"},
		{Action: "user_create", Cluster: "alpha", Account: "research", User: "alice"},
		{Action: "association_create", Cluster: "alpha", Account: "research", User: "alice", Partition: "gpu"},
		{Action: "association_remove", Cluster: "alpha", Account: "research", User: "alice", AssociationID: "42"},
	} {
		method, path, body, ok := accountingWrite("v0.0.43", m)
		if !ok {
			t.Fatal(m)
		}
		r := localRequest{Version: "v0.0.43", Method: method, Path: path, Body: body, Write: true}
		if !validLocalRequest(r) {
			t.Fatalf("valid IPC rejected: %s %s %s", method, path, body)
		}
		r.Write = false
		if validLocalRequest(r) {
			t.Fatal("unmarked write accepted")
		}
		r.Write = true
		if len(body) > 0 {
			var v map[string]any
			_ = json.Unmarshal(body, &v)
			v["unexpected"] = "admin"
			r.Body, _ = json.Marshal(v)
			if validLocalRequest(r) {
				t.Fatal("extra mutation fields accepted")
			}
		}
		if m.Action == "user_create" {
			r.Body = bytes.ReplaceAll(body, []byte(`"None"`), []byte(`"Administrator"`))
			if validLocalRequest(r) {
				t.Fatal("administrator creation accepted")
			}
		}
		if m.Action == "association_remove" {
			r.Path = path + "&account=other"
			if validLocalRequest(r) {
				t.Fatal("ambiguous delete accepted")
			}
		}
	}
	for _, path := range []string{"/slurmdb/v0.0.43/accounts/", "/slurmdb/v0.0.43/users/?with_assocs=true", "/slurmdb/v0.0.43/associations/?cluster=alpha"} {
		if !validLocalRequest(localRequest{Version: "v0.0.43", Method: "GET", Path: path}) {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"/slurmdb/v0.0.43/users/?with_assocs=true&with_deleted=true", "/slurmdb/v0.0.43/associations/", "/slurmdb/v0.0.43/associations/?cluster=alpha%2Cbeta", "/slurmdb/v0.0.43/associations/?cluster=alpha&cluster=beta", "/slurmdb/v0.0.43/config"} {
		if validLocalRequest(localRequest{Version: "v0.0.43", Method: "GET", Path: path}) {
			t.Fatal(path)
		}
	}
	for _, m := range []AccountingMutation{{Action: "account_create", Cluster: "alpha", Account: "UPPER", Description: "x", Organization: "x"}, {Action: "user_create", Cluster: "alpha", Account: "research", User: "Upper"}, {Action: "association_remove", Cluster: "alpha", Account: "research", User: "ALL", AssociationID: "42"}} {
		if m.Valid() {
			t.Fatal("unsafe selector accepted", m)
		}
	}
}
