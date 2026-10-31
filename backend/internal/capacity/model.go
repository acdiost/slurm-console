// Package capacity derives read-only cluster capacity and bounded aggregate trends.
package capacity

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"slurm-console/backend/internal/overview"
	"slurm-console/backend/internal/slurm/models"
	"sort"
	"strings"
	"time"
	"unicode"
)

const Version = 1
const Step = int64(300)
const Retention = int64(7 * 24 * 3600)

var ErrInvalid = errors.New("invalid capacity data or query")
var ErrChanged = errors.New("capacity connection changed")
var ErrBusy = errors.New("capacity query busy")

type Frame struct {
	Version          int               `json:"version"`
	Nodes            int               `json:"nodes"`
	UnavailableNodes int               `json:"unavailableNodes"`
	CPUs             overview.Resource `json:"cpus"`
	MemoryMiB        overview.Resource `json:"memoryMiB"`
	GPUs             overview.GPU      `json:"gpus"`
}

func FromSummary(v overview.Nodes) Frame {
	return Frame{Version, v.Total, v.Unavailable, v.CPUs, v.MemoryMiB, v.GPUs}
}
func (f Frame) Valid() bool {
	if f.Version != Version || f.Nodes < 0 || f.Nodes > 20000 || f.UnavailableNodes < 0 || f.UnavailableNodes > f.Nodes || f.GPUs.UnknownNodes < 0 || f.GPUs.UnknownNodes > f.Nodes {
		return false
	}
	valid := func(v overview.Resource) bool {
		return v.Total >= 0 && v.Allocated >= 0 && v.Idle >= 0 && v.Unavailable >= 0 && v.Allocated <= v.Total && v.Idle <= v.Total-v.Allocated && v.Unavailable == v.Total-v.Allocated-v.Idle
	}
	if !valid(f.CPUs) || !valid(f.MemoryMiB) {
		return false
	}
	g := f.GPUs
	if g.Total != nil && *g.Total < 0 {
		return false
	}
	if g.Total != nil && g.Allocated != nil && g.Idle != nil && g.Unavailable != nil {
		return valid(overview.Resource{Total: *g.Total, Allocated: *g.Allocated, Idle: *g.Idle, Unavailable: *g.Unavailable})
	}
	return g.Allocated == nil && g.Idle == nil && g.Unavailable == nil
}

type Bucket struct {
	Label string `json:"label"`
	Nodes int    `json:"nodes"`
}
type Headroom struct {
	StateAvailableNodes  int      `json:"stateAvailableNodes"`
	LargestCPUIdle       int64    `json:"largestCpuIdle"`
	LargestMemoryIdleMiB int64    `json:"largestMemoryIdleMiB"`
	CPUIdleBuckets       []Bucket `json:"cpuIdleBuckets"`
}
type Partition struct {
	Name   string   `json:"name"`
	States []string `json:"states"`
	Frame  Frame    `json:"frame"`
}
type Query struct {
	Q, Sort, Order string
	Page, PerPage  int
}

func (q Query) Valid() bool {
	return len(q.Q) <= 128 && !strings.ContainsFunc(q.Q, unicode.IsControl) && q.Page >= 1 && q.Page <= 1000000 && q.PerPage >= 1 && q.PerPage <= 100 && (q.Order == "asc" || q.Order == "desc") && slices.Contains([]string{"name", "nodes", "cpuTotal", "cpuIdle", "memoryTotal", "gpuTotal"}, q.Sort)
}

type Report struct {
	Totals     Frame       `json:"totals"`
	Headroom   Headroom    `json:"headroom"`
	Partitions []Partition `json:"partitions"`
	Page       int         `json:"page"`
	PerPage    int         `json:"perPage"`
	TotalItems int         `json:"totalItems"`
}

func Analyze(nodes []models.Node, config []models.Partition, configKnown bool, q Query) (Report, error) {
	if !q.Valid() || len(nodes) > 20000 {
		return Report{}, ErrInvalid
	}
	summary, e := overview.NodeSummary(nodes)
	if e != nil {
		return Report{}, e
	}
	out := Report{Totals: FromSummary(summary), Page: q.Page, PerPage: q.PerPage, Partitions: []Partition{}, Headroom: Headroom{CPUIdleBuckets: []Bucket{{Label: "0"}, {Label: "1–7"}, {Label: "8–31"}, {Label: "32–63"}, {Label: "64+"}}}}
	if !out.Totals.Valid() {
		return out, ErrInvalid
	}
	groups := map[string][]int{}
	seen := map[string]bool{}
	members := 0
	for i, n := range nodes {
		if n.Name == "" || seen[n.Name] {
			return out, ErrInvalid
		}
		seen[n.Name] = true
		if !overview.NodeUnavailable(n.States) {
			out.Headroom.StateAvailableNodes++
			cpu, mem := n.EffectiveCPUs-n.AllocatedCPUs, n.MemoryMiB-n.AllocatedMemoryMiB
			out.Headroom.LargestCPUIdle = max(out.Headroom.LargestCPUIdle, cpu)
			out.Headroom.LargestMemoryIdleMiB = max(out.Headroom.LargestMemoryIdleMiB, mem)
			bucket := 0
			switch {
			case cpu >= 64:
				bucket = 4
			case cpu >= 32:
				bucket = 3
			case cpu >= 8:
				bucket = 2
			case cpu >= 1:
				bucket = 1
			}
			out.Headroom.CPUIdleBuckets[bucket].Nodes++
		}
		names := n.Partitions
		if len(names) == 0 {
			names = []string{""}
		}
		assigned := map[string]bool{}
		for _, name := range names {
			if assigned[name] {
				continue
			}
			assigned[name] = true
			if len(name) > 255 || strings.ContainsFunc(name, unicode.IsControl) {
				return out, ErrInvalid
			}
			groups[name] = append(groups[name], i)
			members++
			if len(groups) > 512 || members > 100000 {
				return out, fmt.Errorf("capacity membership limit exceeded")
			}
		}
	}
	states := map[string][]string{}
	if configKnown {
		for _, p := range config {
			states[p.Name] = p.States
		}
	}
	rows := []Partition{}
	for name, indices := range groups {
		if !strings.Contains(strings.ToLower(name), strings.ToLower(q.Q)) {
			continue
		}
		v, e := overview.NodeSummaryFrom(func(yield func(models.Node) bool) {
			for _, i := range indices {
				if !yield(nodes[i]) {
					return
				}
			}
		})
		if e != nil {
			return out, e
		}
		frame := FromSummary(v)
		if !frame.Valid() {
			return out, ErrInvalid
		}
		row := Partition{Name: name, States: states[name], Frame: frame}
		rows = append(rows, row)
	}
	key := func(p Partition) int64 {
		switch q.Sort {
		case "nodes":
			return int64(p.Frame.Nodes)
		case "cpuTotal":
			return p.Frame.CPUs.Total
		case "cpuIdle":
			return p.Frame.CPUs.Idle
		case "memoryTotal":
			return p.Frame.MemoryMiB.Total
		case "gpuTotal":
			if p.Frame.GPUs.Total != nil {
				return *p.Frame.GPUs.Total
			}
			return -1
		}
		return 0
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if q.Sort != "name" {
			x, y := key(a), key(b)
			if x < 0 || y < 0 {
				if x != y {
					return y < 0
				}
			}
			if x != y {
				if q.Order == "desc" {
					return x > y
				}
				return x < y
			}
		}
		if q.Sort == "name" && q.Order == "desc" {
			return a.Name > b.Name
		}
		return a.Name < b.Name
	})
	out.TotalItems = len(rows)
	start := min((q.Page-1)*q.PerPage, len(rows))
	end := min(start+q.PerPage, len(rows))
	out.Partitions = rows[start:end]
	return out, nil
}

type Sample struct {
	Bucket     int64  `json:"bucket"`
	ObservedAt string `json:"observedAt"`
	Frame      *Frame `json:"frame"`
}
type Statistic struct {
	KnownSamples             int      `json:"knownSamples"`
	WeightedAllocatedPercent *float64 `json:"weightedAllocatedPercent"`
	PeakAllocatedPercent     *float64 `json:"peakAllocatedPercent"`
	MinimumIdle              *int64   `json:"minimumIdle"`
}
type History struct {
	Start            int64     `json:"start"`
	End              int64     `json:"end"`
	StepSeconds      int64     `json:"stepSeconds"`
	ExpectedSamples  int       `json:"expectedSamples"`
	AvailableSamples int       `json:"availableSamples"`
	Points           []Sample  `json:"points"`
	CPUs             Statistic `json:"cpus"`
	MemoryMiB        Statistic `json:"memoryMiB"`
	GPUs             Statistic `json:"gpus"`
}

func Window(window string, now time.Time) (int64, int64, error) {
	span := map[string]int64{"1h": 3600, "24h": 86400, "7d": Retention}[window]
	if span == 0 {
		return 0, 0, ErrInvalid
	}
	end := now.UTC().Unix() / Step * Step
	return end - span, end, nil
}
func Trend(start, end int64, samples []Sample) (History, error) {
	h := History{Start: start, End: end, StepSeconds: Step, ExpectedSamples: int((end - start) / Step), Points: []Sample{}}
	if end <= start || end-start > Retention || start%Step != 0 || end%Step != 0 {
		return h, ErrInvalid
	}
	by := map[int64]Sample{}
	for _, s := range samples {
		if s.Bucket < start || s.Bucket >= end || s.Bucket%Step != 0 || s.Frame == nil || !s.Frame.Valid() {
			return h, ErrInvalid
		}
		observed, e := time.Parse(time.RFC3339Nano, s.ObservedAt)
		if e != nil || observed.Unix() < s.Bucket || observed.Unix() >= s.Bucket+Step {
			return h, ErrInvalid
		}
		if _, ok := by[s.Bucket]; ok {
			return h, ErrInvalid
		}
		by[s.Bucket] = s
	}
	for bucket := start; bucket < end; bucket += Step {
		if s, ok := by[bucket]; ok {
			h.Points = append(h.Points, s)
			h.AvailableSamples++
		} else {
			h.Points = append(h.Points, Sample{Bucket: bucket})
		}
	}
	stat := func(which string) Statistic {
		out := Statistic{}
		var total, allocated float64
		peak := 0.0
		var idle *int64
		for _, s := range h.Points {
			if s.Frame == nil {
				continue
			}
			v := s.Frame.CPUs
			if which == "memory" {
				v = s.Frame.MemoryMiB
			}
			if which == "gpu" {
				g := s.Frame.GPUs
				if g.Total == nil || g.Allocated == nil || g.Idle == nil || g.Unavailable == nil {
					continue
				}
				v = overview.Resource{Total: *g.Total, Allocated: *g.Allocated, Idle: *g.Idle, Unavailable: *g.Unavailable}
			}
			out.KnownSamples++
			if idle == nil || v.Idle < *idle {
				n := v.Idle
				idle = &n
			}
			if v.Total <= 0 {
				continue
			}
			total += float64(v.Total)
			allocated += float64(v.Allocated)
			peak = math.Max(peak, 100*float64(v.Allocated)/float64(v.Total))
		}
		out.MinimumIdle = idle
		if total > 0 {
			mean := 100 * allocated / total
			out.WeightedAllocatedPercent = &mean
			out.PeakAllocatedPercent = &peak
		}
		return out
	}
	h.CPUs = stat("cpu")
	h.MemoryMiB = stat("memory")
	h.GPUs = stat("gpu")
	return h, nil
}
