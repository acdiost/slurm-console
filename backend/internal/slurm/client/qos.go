package client

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// QOSSpec deliberately excludes preemption, flags, TRES and association changes.
// A nil limit explicitly means unlimited; it never means an omitted edit.
type QOSSpec struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	Description          string  `json:"description"`
	Priority             uint32  `json:"priority"`
	UsageFactor          float64 `json:"usageFactor"`
	MaxJobsPerUser       *uint32 `json:"maxJobsPerUser"`
	MaxSubmitJobsPerUser *uint32 `json:"maxSubmitJobsPerUser"`
	MaxWallMinutes       *uint32 `json:"maxWallMinutes"`
}

func (q *QOSSpec) UnmarshalJSON(raw []byte) error {
	type plain QOSSpec
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return fmt.Errorf("invalid QOS spec")
	}
	for _, key := range []string{"id", "name", "description", "priority", "usageFactor", "maxJobsPerUser", "maxSubmitJobsPerUser", "maxWallMinutes"} {
		v, ok := fields[key]
		if !ok || (bytes.Equal(bytes.TrimSpace(v), []byte("null")) && key != "maxJobsPerUser" && key != "maxSubmitJobsPerUser" && key != "maxWallMinutes") {
			return fmt.Errorf("missing QOS field")
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var v plain
	if err := d.Decode(&v); err != nil {
		return err
	}
	*q = QOSSpec(v)
	return nil
}

func (q QOSSpec) Valid(action string) bool {
	if !ValidAccountingName(q.Name) || strings.TrimSpace(q.Description) == "" || utf8.RuneCountInString(q.Description) > 200 || strings.ContainsAny(q.Description, "\r\n\x00") || q.Priority > 2147483647 || math.IsNaN(q.UsageFactor) || math.IsInf(q.UsageFactor, 0) || q.UsageFactor < 0 || q.UsageFactor > 1000000 {
		return false
	}
	for _, n := range []*uint32{q.MaxJobsPerUser, q.MaxSubmitJobsPerUser, q.MaxWallMinutes} {
		if n != nil && *n > 2147483647 {
			return false
		}
	}
	return action == "qos_create" && q.ID == "" && q.Name == strings.ToLower(q.Name) || action == "qos_update" && ValidJobID(q.ID)
}

func qosWrite(version string, m AccountingMutation) (string, string, []byte, bool) {
	if !m.Valid() || m.QOS == nil || len(m.QOSPreemptModes) == 0 {
		return "", "", nil, false
	}
	q := m.QOS
	number := func(n *uint32) any {
		if n == nil {
			return map[string]any{"set": false, "infinite": true, "number": 0}
		}
		return map[string]any{"set": true, "infinite": false, "number": *n}
	}
	v := map[string]any{"name": q.Name, "description": q.Description, "flags": []string{"NOT_SET"}, "priority": map[string]any{"set": true, "number": q.Priority}, "usage_factor": map[string]any{"set": true, "number": q.UsageFactor}, "limits": map[string]any{"max": map[string]any{"jobs": map[string]any{"active_jobs": map[string]any{"per": map[string]any{"user": number(q.MaxJobsPerUser)}}, "per": map[string]any{"user": number(q.MaxSubmitJobsPerUser)}}, "wall_clock": map[string]any{"per": map[string]any{"job": number(q.MaxWallMinutes)}}}}}
	// v0.0.43 defaults an omitted mode to DISABLED even during a partial update.
	// This value is materialized by the service from its signed live snapshot.
	v["preempt"] = map[string]any{"mode": m.QOSPreemptModes}
	if q.ID != "" {
		id, _ := strconv.ParseUint(q.ID, 10, 32)
		v["id"] = id
	}
	raw, _ := json.Marshal(map[string]any{"qos": []any{v}})
	return http.MethodPost, "/slurmdb/" + version + "/qos/", raw, true
}

func validQOSWire(r localRequest) bool {
	if r.Method != "POST" || !r.Write || r.Path != "/slurmdb/"+r.Version+"/qos/" {
		return false
	}
	var body struct {
		QOS []struct {
			ID                *uint32
			Name, Description string
			Priority          struct{ Number uint32 }
			UsageFactor       struct{ Number float64 } `json:"usage_factor"`
			Preempt           struct{ Mode []string }
			Limits            struct {
				Max struct {
					Jobs struct {
						Active struct {
							Per struct {
								User struct {
									Infinite bool
									Number   uint32
								}
							}
						} `json:"active_jobs"`
						Per struct {
							User struct {
								Infinite bool
								Number   uint32
							}
						}
					}
					Wall struct {
						Per struct {
							Job struct {
								Infinite bool
								Number   uint32
							}
						}
					} `json:"wall_clock"`
				}
			}
		}
	}
	if json.Unmarshal(r.Body, &body) != nil || len(body.QOS) != 1 {
		return false
	}
	v := body.QOS[0]
	q := QOSSpec{Name: v.Name, Description: v.Description, Priority: v.Priority.Number, UsageFactor: v.UsageFactor.Number}
	limit := func(infinite bool, n uint32) *uint32 {
		if infinite {
			return nil
		}
		return &n
	}
	q.MaxJobsPerUser = limit(v.Limits.Max.Jobs.Active.Per.User.Infinite, v.Limits.Max.Jobs.Active.Per.User.Number)
	q.MaxSubmitJobsPerUser = limit(v.Limits.Max.Jobs.Per.User.Infinite, v.Limits.Max.Jobs.Per.User.Number)
	q.MaxWallMinutes = limit(v.Limits.Max.Wall.Per.Job.Infinite, v.Limits.Max.Wall.Per.Job.Number)
	action := "qos_create"
	if v.ID != nil {
		q.ID = strconv.FormatUint(uint64(*v.ID), 10)
		action = "qos_update"
	}
	method, path, raw, ok := qosWrite(r.Version, AccountingMutation{Action: action, Cluster: "wire-validation", QOS: &q, QOSPreemptModes: v.Preempt.Mode})
	if !ok || method != r.Method || path != r.Path {
		return false
	}
	var a, b any
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(r.Body, &b) != nil {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func validQOSPreservation(m AccountingMutation) bool {
	if len(m.QOSPreemptModes) > 5 {
		return false
	}
	seen := map[string]bool{}
	for _, mode := range m.QOSPreemptModes {
		if seen[mode] || !slices.Contains([]string{"DISABLED", "SUSPEND", "REQUEUE", "CANCEL", "GANG"}, mode) {
			return false
		}
		seen[mode] = true
	}
	if seen["DISABLED"] && len(seen) != 1 {
		return false
	}
	if m.QOSUnmanagedFingerprint != "" {
		raw, err := hex.DecodeString(m.QOSUnmanagedFingerprint)
		if err != nil || len(raw) != 32 {
			return false
		}
	}
	return true
}
