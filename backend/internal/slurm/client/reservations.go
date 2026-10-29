package client

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
)

type ReservationSpec struct {
	Name        string   `json:"name"`
	StartTime   int64    `json:"startTime"`
	EndTime     int64    `json:"endTime"`
	Nodes       []string `json:"nodes"`
	Users       []string `json:"users"`
	Accounts    []string `json:"accounts"`
	Maintenance bool     `json:"maintenance"`
}

func IsReservation(action string) bool {
	return slices.Contains([]string{"reservation_create", "reservation_update", "reservation_delete"}, action)
}
func literalList(v []string, node bool) bool {
	if len(v) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, n := range v {
		if seen[n] || strings.EqualFold(n, "ALL") || node && !ValidNodeTarget(n) || !node && !ValidAccountingName(n) {
			return false
		}
		seen[n] = true
	}
	return true
}
func (v ReservationSpec) Valid(action string) bool {
	if !ValidAccountingName(v.Name) || strings.EqualFold(v.Name, "ALL") {
		return false
	}
	if action == "reservation_create" {
		return v.StartTime > 0 && v.EndTime > v.StartTime && v.EndTime <= 4102444800 && v.EndTime-v.StartTime <= 30*86400 && len(v.Nodes) > 0 && len(v.Users)+len(v.Accounts) > 0 && literalList(v.Nodes, true) && literalList(v.Users, false) && literalList(v.Accounts, false)
	}
	if v.StartTime != 0 || len(v.Nodes)+len(v.Users)+len(v.Accounts) != 0 || v.Maintenance {
		return false
	}
	return action == "reservation_update" && v.EndTime > 0 && v.EndTime <= 4102444800 || action == "reservation_delete" && v.EndTime == 0
}
func validReservationMutation(m AccountingMutation) bool {
	return ValidAccountingName(m.Cluster) && m.Reservation != nil && m.Reservation.Valid(m.Action) && m.Account == "" && m.User == "" && m.Partition == "" && m.AssociationID == "" && m.Description == "" && m.Organization == "" && m.QOS == nil && m.TRES == nil && len(m.TRESEntries) == 0 && len(m.TRESLists) == 0 && m.TRESUnmanagedFingerprint == "" && len(m.QOSPreemptModes) == 0 && m.QOSUnmanagedFingerprint == ""
}
func reservationWrite(version string, m AccountingMutation) (string, string, []byte, bool) {
	if !m.Valid() || m.Reservation == nil {
		return "", "", nil, false
	}
	v := m.Reservation
	prefix := "/slurm/" + version + "/reservation"
	if m.Action == "reservation_delete" {
		return "DELETE", prefix + "/" + v.Name, nil, true
	}
	stamp := func(n int64) any { return map[string]any{"set": true, "infinite": false, "number": n} }
	b := map[string]any{"name": v.Name, "end_time": stamp(v.EndTime)}
	if m.Action == "reservation_create" {
		b["start_time"] = stamp(v.StartTime)
		b["node_list"] = v.Nodes
		b["users"] = append([]string{}, v.Users...)
		b["accounts"] = append([]string{}, v.Accounts...)
		flags := []string{}
		if v.Maintenance {
			flags = append(flags, "MAINT")
		}
		b["flags"] = flags
	}
	raw, _ := json.Marshal(b)
	return "POST", prefix, raw, true
}
func validReservationWire(r localRequest) bool {
	prefix := "/slurm/" + r.Version + "/reservation"
	if !r.Write {
		return false
	}
	if r.Method == "DELETE" && len(r.Body) == 0 && strings.HasPrefix(r.Path, prefix+"/") {
		return ValidAccountingName(strings.TrimPrefix(r.Path, prefix+"/")) && !strings.EqualFold(strings.TrimPrefix(r.Path, prefix+"/"), "ALL")
	}
	if r.Method != "POST" || r.Path != prefix {
		return false
	}
	var v struct {
		Name       string `json:"name"`
		Start, End struct {
			Set      bool  `json:"set"`
			Infinite bool  `json:"infinite"`
			Number   int64 `json:"number"`
		}
		Nodes, Users, Accounts, Flags []string
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(r.Body, &raw) != nil {
		return false
	}
	if json.Unmarshal(raw["name"], &v.Name) != nil || json.Unmarshal(raw["end_time"], &v.End) != nil || !v.End.Set || v.End.Infinite {
		return false
	}
	s := ReservationSpec{Name: v.Name, EndTime: v.End.Number}
	action := "reservation_update"
	if raw["start_time"] != nil {
		action = "reservation_create"
		if json.Unmarshal(raw["start_time"], &v.Start) != nil || !v.Start.Set || v.Start.Infinite {
			return false
		}
		s.StartTime = v.Start.Number
		if json.Unmarshal(raw["node_list"], &s.Nodes) != nil || json.Unmarshal(raw["users"], &s.Users) != nil || json.Unmarshal(raw["accounts"], &s.Accounts) != nil || json.Unmarshal(raw["flags"], &v.Flags) != nil {
			return false
		}
		if len(v.Flags) > 1 || len(v.Flags) == 1 && v.Flags[0] != "MAINT" {
			return false
		}
		s.Maintenance = len(v.Flags) == 1
	}
	_, _, expected, ok := reservationWrite(r.Version, AccountingMutation{Action: action, Cluster: "validation", Reservation: &s})
	if !ok {
		return false
	}
	decode := func(b []byte) (any, bool) {
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		var value any
		if d.Decode(&value) != nil || d.Decode(new(any)) != io.EOF {
			return nil, false
		}
		return value, true
	}
	a, aok := decode(r.Body)
	b, bok := decode(expected)
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return aok && bok && bytes.Equal(aa, bb)
}
