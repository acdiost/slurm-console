// Package usagereport measures verifiable allocation intervals, not telemetry or billing.
package usagereport

import (
	"errors"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"unicode/utf8"

	"slurm-console/backend/internal/slurm/models"
)

var ErrInvalid = errors.New("invalid accounting report source")

type Metric struct {
	Seconds        *string `json:"resourceSeconds"`
	KnownRecords   int     `json:"knownRecords"`
	UnknownRecords int     `json:"unknownRecords"`
	sum            big.Int
}
type Exclusions struct {
	Active      int `json:"active"`
	Complex     int `json:"complex"`
	Uncertain   int `json:"uncertain"`
	Overlapping int `json:"overlapping"`
}
type Row struct {
	Key            string     `json:"key"`
	Records        int        `json:"records"`
	CountedRecords int        `json:"countedRecords"`
	Excluded       Exclusions `json:"excluded"`
	CPU            Metric     `json:"cpu"`
	Memory         Metric     `json:"memory"`
	GPU            Metric     `json:"gpu"`
}
type Report struct {
	Version        int    `json:"version"`
	Basis          string `json:"basis"`
	Cluster        string `json:"cluster"`
	Start          int64  `json:"start"`
	End            int64  `json:"end"`
	GroupBy        string `json:"groupBy"`
	SourceRecords  int    `json:"sourceRecords"`
	OutsideRecords int    `json:"outsideRecords"`
	Summary        Row    `json:"summary"`
	Groups         []Row  `json:"groups"`
}

func (m *Metric) add(count *big.Int, seconds int64) {
	if count == nil {
		m.UnknownRecords++
		return
	}
	m.KnownRecords++
	m.sum.Add(&m.sum, new(big.Int).Mul(count, big.NewInt(seconds)))
}
func (m *Metric) finish() {
	if m.KnownRecords > 0 || m.UnknownRecords == 0 {
		s := m.sum.String()
		m.Seconds = &s
	}
}
func (r *Row) finish() { r.CPU.finish(); r.Memory.finish(); r.GPU.finish() }
func resource(j models.HistoryJob, typ, name string) *big.Int {
	var n *big.Int
	found := false
	for _, t := range j.AllocatedTRES {
		if t.Type != typ || t.Name != name {
			continue
		}
		if found || t.Count == nil {
			return nil
		}
		found = true
		v, ok := new(big.Int).SetString(*t.Count, 10)
		if !ok || v.Sign() < 0 || len(*t.Count) > 20 || v.String() != *t.Count {
			return nil
		}
		n = v
	}
	return n
}
func value(n models.Number) int64 {
	if n.Value == nil || n.Infinite {
		return -1
	}
	return *n.Value
}
func terminal(j models.HistoryJob) bool {
	return len(j.States) == 1 && slices.Contains([]string{"COMPLETED", "CANCELLED", "FAILED", "TIMEOUT", "NODE_FAIL", "PREEMPTED", "BOOT_FAIL", "DEADLINE", "OUT_OF_MEMORY", "REVOKED"}, j.States[0])
}
func complex(j models.HistoryJob) bool {
	return value(j.HetJobID) > 0 || value(j.HetJobOffset) > 0 || value(j.ArrayJobID) > 0 && value(j.ArrayTaskID) < 0 || value(j.ArrayTaskID) >= 0 && value(j.ArrayJobID) <= 0
}
func active(j models.HistoryJob) bool {
	for _, s := range j.States {
		if slices.Contains([]string{"PENDING", "RUNNING", "SUSPENDED", "CONFIGURING", "COMPLETING"}, s) {
			return true
		}
	}
	return false
}
func identity(j models.HistoryJob) string {
	if value(j.ArrayJobID) > 0 && value(j.ArrayTaskID) >= 0 {
		return "array:" + strconv.FormatInt(value(j.ArrayJobID), 10) + ":" + strconv.FormatInt(value(j.ArrayTaskID), 10)
	}
	return "job:" + j.ID
}

// Analyze clips only ended, non-suspended, consistent time records to [start,end).
// Steps and TotalCPU are deliberately never added. Missing generic GPU stays unknown.
func Analyze(s models.HistorySnapshot, groupBy string, observed int64) (Report, error) {
	out := Report{Version: 1, Basis: "execution_overlap_estimate", Cluster: s.Cluster, Start: s.Start, End: s.End, GroupBy: groupBy, SourceRecords: len(s.Items), Groups: []Row{}}
	if !slices.Contains([]string{"user", "account", "partition"}, groupBy) || s.Start <= 0 || s.End <= s.Start || s.End-s.Start > 31*86400 || len(s.Items) > 5000 || observed <= 0 {
		return out, ErrInvalid
	}
	// Reused IDs with overlapping intervals are ambiguous, never summed twice.
	byID := map[string][]int{}
	seen := map[string]bool{}
	for i, j := range s.Items {
		if j.Cluster != s.Cluster || j.Key == "" || seen[j.Key] {
			return out, ErrInvalid
		}
		seen[j.Key] = true
		for _, v := range []string{j.User, j.Account, j.Partition} {
			if len(v) > 256 || !utf8.ValidString(v) {
				return out, ErrInvalid
			}
		}
		if value(j.StartTime) > 0 && value(j.EndTime) > value(j.StartTime) {
			k := identity(j)
			byID[k] = append(byID[k], i)
		}
	}
	overlapping := map[int]bool{}
	for _, indices := range byID {
		sort.Slice(indices, func(i, j int) bool {
			return value(s.Items[indices[i]].StartTime) < value(s.Items[indices[j]].StartTime)
		})
		if len(indices) == 0 {
			continue
		}
		furthest := indices[0]
		for _, i := range indices[1:] {
			if value(s.Items[i].StartTime) < value(s.Items[furthest].EndTime) {
				overlapping[i] = true
				overlapping[furthest] = true
			}
			if value(s.Items[i].EndTime) > value(s.Items[furthest].EndTime) {
				furthest = i
			}
		}
	}
	groups := map[string]*Row{}
	for i, j := range s.Items {
		a, b := value(j.StartTime), value(j.EndTime)
		// Recheck the source's runtime selector. Unknown intervals remain explicit exclusions.
		if a > 0 && a >= s.End && (b < 0 || b >= a) || b > 0 && b <= s.Start && (a < 0 || b >= a) {
			out.OutsideRecords++
			continue
		}
		key := j.User
		if groupBy == "account" {
			key = j.Account
		}
		if groupBy == "partition" {
			key = j.Partition
		}
		r := groups[key]
		if r == nil {
			r = &Row{Key: key}
			groups[key] = r
		}
		reason := ""
		seconds := int64(0)
		switch {
		case complex(j):
			reason = "complex"
		case active(j):
			reason = "active"
		case !terminal(j) || a <= 0 || b < a || b > observed || value(j.SubmitTime) <= 0 || value(j.SubmitTime) > a || value(j.ElapsedSeconds) != b-a || value(j.SuspendedSeconds) != 0:
			reason = "uncertain"
		case overlapping[i]:
			reason = "overlapping"
		default:
			seconds = max(0, min(b, s.End)-max(a, s.Start))
		}
		cpu, mem, gpu := resource(j, "cpu", ""), resource(j, "mem", ""), resource(j, "gres", "gpu")
		if reason != "" {
			cpu = nil
			mem = nil
			gpu = nil
		}
		for _, row := range []*Row{r, &out.Summary} {
			row.Records++
			switch reason {
			case "complex":
				row.Excluded.Complex++
			case "active":
				row.Excluded.Active++
			case "uncertain":
				row.Excluded.Uncertain++
			case "overlapping":
				row.Excluded.Overlapping++
			default:
				row.CountedRecords++
			}
			row.CPU.add(cpu, seconds)
			row.Memory.add(mem, seconds)
			row.GPU.add(gpu, seconds)
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r := groups[k]
		r.finish()
		out.Groups = append(out.Groups, *r)
	}
	out.Summary.finish()
	return out, nil
}
