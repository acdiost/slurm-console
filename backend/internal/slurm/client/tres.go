package client

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Values remain decimal strings at the application boundary, including zero.
type TRESSpec struct {
	Kind       string `json:"kind"`
	TargetID   string `json:"targetId"`
	Name       string `json:"name"`
	Account    string `json:"account"`
	User       string `json:"user"`
	Partition  string `json:"partition"`
	ResourceID string `json:"resourceId"`
	Bucket     string `json:"bucket"`
	Mode       string `json:"mode"`
	Value      string `json:"value"`
}
type TRESWireEntry struct {
	ID    uint32 `json:"id"`
	Type  string `json:"type"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

var tresType = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
var tresName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
var tresValue = regexp.MustCompile(`^(0|[1-9][0-9]{0,18})$`)

func ValidTRESIdentity(t, n string) bool {
	return tresType.MatchString(t) && (n == "" || tresName.MatchString(n))
}
func EditableTRES(t, n string) bool {
	return (n == "" && slices.Contains([]string{"cpu", "mem", "node", "billing"}, t)) || t == "license" && n != "" || t == "gres" && (n == "gpu" || strings.HasPrefix(n, "gpu:"))
}
func TRESBuckets(kind string) []string {
	if kind == "qos" {
		return []string{"group_total", "per_job", "per_user", "per_account", "per_node", "group_minutes", "run_minutes", "job_minutes"}
	}
	if kind == "association" {
		return []string{"group_total", "per_job", "per_node", "group_minutes", "run_minutes", "job_minutes"}
	}
	return nil
}
func TRESPath(kind, bucket string) []string {
	if !slices.Contains(TRESBuckets(kind), bucket) {
		return nil
	}
	prefix := []string{"max", "tres"}
	if kind == "qos" {
		prefix = []string{"limits", "max", "tres"}
	}
	parts := map[string][]string{"group_total": {"total"}, "per_job": {"per", "job"}, "per_user": {"per", "user"}, "per_account": {"per", "account"}, "per_node": {"per", "node"}, "job_minutes": {"minutes", "per", "job"}}
	if kind == "qos" {
		parts["group_minutes"] = []string{"minutes", "total"}
		parts["run_minutes"] = []string{"minutes", "per", "qos"}
	} else {
		parts["group_minutes"] = []string{"group", "minutes"}
		parts["run_minutes"] = []string{"group", "active"}
	}
	return append(prefix, parts[bucket]...)
}
func (v TRESSpec) Valid() bool {
	if !ValidJobID(v.TargetID) || !ValidJobID(v.ResourceID) || !slices.Contains(TRESBuckets(v.Kind), v.Bucket) || !slices.Contains([]string{"set", "clear"}, v.Mode) {
		return false
	}
	if v.Mode == "clear" && v.Value != "" || v.Mode == "set" && !tresValue.MatchString(v.Value) {
		return false
	}
	if v.Mode == "set" {
		if _, err := strconv.ParseInt(v.Value, 10, 64); err != nil {
			return false
		}
	}
	if v.Kind == "qos" {
		return ValidAccountingName(v.Name) && v.Account == "" && v.User == "" && v.Partition == ""
	}
	return v.Kind == "association" && v.Name == "" && ValidAccountingName(v.Account) && (v.User == "" || ValidAccountingName(v.User)) && (v.Partition == "" || ValidAccountingName(v.Partition))
}
func validTRESMutation(m AccountingMutation) bool {
	if !ValidAccountingName(m.Cluster) || m.TRES == nil || !m.TRES.Valid() || m.QOS != nil || m.Account != "" || m.User != "" || m.Partition != "" || m.AssociationID != "" || m.Description != "" || m.Organization != "" {
		return false
	}
	if len(m.TRESEntries) > 256 {
		return false
	}
	if m.TRES.Kind == "qos" && len(m.TRESEntries) > 1 {
		return false
	}
	if m.TRES.Kind == "qos" && len(m.TRESLists) > 0 {
		return false
	}
	if len(m.TRESLists) > 0 {
		if len(m.TRESLists) != len(TRESBuckets("association")) {
			return false
		}
		for _, key := range TRESBuckets("association") {
			list, ok := m.TRESLists[key]
			if !ok || len(list) > 256 {
				return false
			}
			seen := map[uint32]bool{}
			for _, entry := range list {
				if entry.ID == 0 || seen[entry.ID] || !ValidTRESIdentity(entry.Type, entry.Name) || entry.Count < -1 {
					return false
				}
				seen[entry.ID] = true
			}
		}
		a, _ := json.Marshal(m.TRESEntries)
		b, _ := json.Marshal(m.TRESLists[m.TRES.Bucket])
		if string(a) != string(b) {
			return false
		}
	}
	seen := map[uint32]bool{}
	if m.Action != "qos_tres_update" && m.Action != "association_tres_update" || m.Action == "qos_tres_update" && m.TRES.Kind != "qos" || m.Action == "association_tres_update" && m.TRES.Kind != "association" {
		return false
	}
	for _, v := range m.TRESEntries {
		if v.ID == 0 || seen[v.ID] || !ValidTRESIdentity(v.Type, v.Name) || v.Count < -1 {
			return false
		}
		seen[v.ID] = true
	}
	if len(m.TRESEntries) > 0 {
		found := false
		for _, entry := range m.TRESEntries {
			if strconv.FormatUint(uint64(entry.ID), 10) == m.TRES.ResourceID {
				expected := int64(-1)
				if m.TRES.Mode == "set" {
					expected, _ = strconv.ParseInt(m.TRES.Value, 10, 64)
				}
				if entry.Count != expected {
					return false
				}
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return validQOSPreservation(m) && (m.TRES.Kind == "qos" || len(m.QOSPreemptModes) == 0 && m.QOSUnmanagedFingerprint == "")
}
func tresWrite(version string, m AccountingMutation) (string, string, []byte, bool) {
	if !m.Valid() || m.TRES == nil || len(m.TRESEntries) == 0 {
		return "", "", nil, false
	}
	s := m.TRES
	id, _ := strconv.ParseUint(s.TargetID, 10, 32)
	var target map[string]any
	resource := "associations"
	if s.Kind == "qos" {
		if len(m.QOSPreemptModes) == 0 {
			return "", "", nil, false
		}
		resource = "qos"
		target = map[string]any{"id": id, "name": s.Name, "flags": []string{"NOT_SET"}, "preempt": map[string]any{"mode": m.QOSPreemptModes}}
	} else {
		target = map[string]any{"id": id, "cluster": m.Cluster, "account": s.Account, "user": s.User, "partition": s.Partition}
	}
	lists := map[string][]TRESWireEntry{s.Bucket: m.TRESEntries}
	if s.Kind == "association" {
		if len(m.TRESLists) != len(TRESBuckets("association")) {
			return "", "", nil, false
		}
		lists = m.TRESLists
	}
	for bucket, entries := range lists {
		current := target
		path := TRESPath(s.Kind, bucket)
		for _, key := range path[:len(path)-1] {
			child, ok := current[key].(map[string]any)
			if !ok {
				child = map[string]any{}
				current[key] = child
			}
			current = child
		}
		current[path[len(path)-1]] = entries
	}
	raw, _ := json.Marshal(map[string]any{resource: []any{target}})
	return http.MethodPost, "/slurmdb/" + version + "/" + resource + "/", raw, true
}
func validTRESWire(r localRequest) bool {
	if r.Method != "POST" || !r.Write {
		return false
	}
	for _, kind := range []string{"qos", "association"} {
		resource := "associations"
		if kind == "qos" {
			resource = "qos"
		}
		if r.Path != "/slurmdb/"+r.Version+"/"+resource+"/" {
			continue
		}
		var body map[string][]json.RawMessage
		if json.Unmarshal(r.Body, &body) != nil || len(body[resource]) != 1 {
			return false
		}
		var target map[string]json.RawMessage
		if json.Unmarshal(body[resource][0], &target) != nil {
			return false
		}
		var id uint32
		_ = json.Unmarshal(target["id"], &id)
		get := func(key string) string { var v string; _ = json.Unmarshal(target[key], &v); return v }
		for _, bucket := range TRESBuckets(kind) {
			raw := body[resource][0]
			for _, key := range TRESPath(kind, bucket) {
				var node map[string]json.RawMessage
				if json.Unmarshal(raw, &node) != nil {
					raw = nil
					break
				}
				raw = node[key]
			}
			var entries []TRESWireEntry
			if len(raw) == 0 || json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
				continue
			}
			s := TRESSpec{Kind: kind, TargetID: strconv.FormatUint(uint64(id), 10), Name: get("name"), Account: get("account"), User: get("user"), Partition: get("partition"), Bucket: bucket, Mode: "clear", ResourceID: strconv.FormatUint(uint64(entries[0].ID), 10)}
			if entries[0].Count >= 0 {
				s.Mode = "set"
				s.Value = strconv.FormatInt(entries[0].Count, 10)
			}
			cluster := get("cluster")
			if kind == "qos" {
				cluster = "wire-validation"
			}
			action := "association_tres_update"
			if kind == "qos" {
				action = "qos_tres_update"
			}
			m := AccountingMutation{Action: action, Cluster: cluster, TRES: &s, TRESEntries: entries}
			if kind == "association" {
				m.TRESLists = map[string][]TRESWireEntry{}
				for _, key := range TRESBuckets(kind) {
					raw := body[resource][0]
					for _, part := range TRESPath(kind, key) {
						var node map[string]json.RawMessage
						if json.Unmarshal(raw, &node) != nil {
							raw = nil
							break
						}
						raw = node[part]
					}
					var list []TRESWireEntry
					if len(raw) == 0 || json.Unmarshal(raw, &list) != nil {
						break
					}
					m.TRESLists[key] = list
				}
			}
			var preempt struct{ Mode []string }
			_ = json.Unmarshal(target["preempt"], &preempt)
			m.QOSPreemptModes = preempt.Mode
			method, path, expected, ok := tresWrite(r.Version, m)
			if !ok || method != r.Method || path != r.Path {
				continue
			}
			var a, b any
			d := json.NewDecoder(strings.NewReader(string(expected)))
			d.UseNumber()
			if d.Decode(&a) != nil {
				return false
			}
			d = json.NewDecoder(strings.NewReader(string(r.Body)))
			d.UseNumber()
			if d.Decode(&b) != nil {
				return false
			}
			aa, _ := json.Marshal(a)
			bb, _ := json.Marshal(b)
			if string(aa) == string(bb) {
				return true
			}
		}
	}
	return false
}
