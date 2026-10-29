package client

import (
	"encoding/json"
	"testing"
)

func TestReservationWireOnlyReviewedFields(t *testing.T) {
	for _, action := range []string{"reservation_create", "reservation_update", "reservation_delete"} {
		v := &ReservationSpec{Name: "reserved", EndTime: 1900000600}
		if action == "reservation_create" {
			v.StartTime = 1900000000
			v.Nodes = []string{"node1"}
			v.Users = []string{"alice"}
		}
		if action == "reservation_delete" {
			v.EndTime = 0
		}
		m := AccountingMutation{Action: action, Cluster: "cluster", Reservation: v}
		method, path, body, ok := reservationWrite("v0.0.43", m)
		r := localRequest{Method: method, Path: path, Body: body, Write: true, Version: "v0.0.43"}
		if !ok || !validLocalRequest(r) {
			t.Fatal(action, string(body))
		}
		if method == "POST" {
			var raw map[string]any
			_ = json.Unmarshal(body, &raw)
			raw["flags"] = []string{"IGNORE_JOBS"}
			r.Body, _ = json.Marshal(raw)
			if validLocalRequest(r) {
				t.Fatal("dangerous flags allowed")
			}
		}
	}
	for _, n := range []string{"ALL", "node[1-2]", "node1,node2", "../bad"} {
		v := ReservationSpec{Name: "qa", StartTime: 1900000000, EndTime: 1900000600, Nodes: []string{n}, Users: []string{"alice"}}
		if v.Valid("reservation_create") {
			t.Fatal(n)
		}
	}
}
