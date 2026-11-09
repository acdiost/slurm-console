// Package overview derives bounded summaries from normalized Slurm DTOs.
package overview

import (
	"cmp"
	"fmt"
	"iter"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"slurm-console/backend/internal/slurm/models"
)

type Resource struct {
	Total       int64 `json:"total"`
	Allocated   int64 `json:"allocated"`
	Idle        int64 `json:"idle"`
	Unavailable int64 `json:"unavailable"`
}
type GPU struct {
	Total        *int64 `json:"total"`
	Allocated    *int64 `json:"allocated"`
	Idle         *int64 `json:"idle"`
	Unavailable  *int64 `json:"unavailable"`
	UnknownNodes int    `json:"unknownNodes"`
}
type StateCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}
type NodeIssue struct {
	Name   string   `json:"name"`
	States []string `json:"states"`
	Reason string   `json:"reason"`
}
type Nodes struct {
	Total       int          `json:"total"`
	Idle        int          `json:"idle"`
	Allocated   int          `json:"allocated"`
	Mixed       int          `json:"mixed"`
	Unavailable int          `json:"unavailable"`
	CPUs        Resource     `json:"cpus"`
	MemoryMiB   Resource     `json:"memoryMiB"`
	GPUs        GPU          `json:"gpus"`
	States      []StateCount `json:"states"`
	Issues      []NodeIssue  `json:"issues"`
}
type Jobs struct {
	Total      int          `json:"total"`
	Running    int          `json:"running"`
	Pending    int          `json:"pending"`
	Failed     int          `json:"failed"`
	Completing int          `json:"completing"`
	Completed  int          `json:"completed"`
	Cancelled  int          `json:"cancelled"`
	Other      int          `json:"other"`
	States     []StateCount `json:"states"`
}
type Partitions struct {
	Total int                `json:"total"`
	Up    int                `json:"up"`
	Items []models.Partition `json:"items"`
}

func counts(values map[string]int) []StateCount {
	result := make([]StateCount, 0, len(values))
	for key, value := range values {
		result = append(result, StateCount{key, value})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].State < result[j].State })
	return result
}
func flags(states []string, wanted ...string) bool {
	for _, state := range wanted {
		if slices.Contains(states, state) {
			return true
		}
	}
	return false
}

// State lists are short; avoid allocating and sorting a copy for every resource.
func countStates(counts map[string]int, states []string) {
	for i, state := range states {
		if !slices.Contains(states[:i], state) {
			counts[state]++
		}
	}
}

// Keep only the alphabetically first preview entries while scanning the full set.
func includePreview[T any](items []T, value T, limit int, compare func(T, T) int) []T {
	i, _ := slices.BinarySearchFunc(items, value, compare)
	if i >= limit {
		return items
	}
	if len(items) == limit {
		items = items[:limit-1]
	}
	return slices.Insert(items, i, value)
}

func JobSummary(jobs []models.Job) Jobs {
	result := Jobs{Total: len(jobs)}
	states := map[string]int{}
	for _, job := range jobs {
		countStates(states, job.States)
		switch {
		case flags(job.States, "FAILED", "TIMEOUT", "NODE_FAIL", "BOOT_FAIL", "OUT_OF_MEMORY", "DEADLINE"):
			result.Failed++
		case flags(job.States, "CANCELLED"):
			result.Cancelled++
		case flags(job.States, "COMPLETED"):
			result.Completed++
		case flags(job.States, "COMPLETING"):
			result.Completing++
		case flags(job.States, "PENDING"):
			result.Pending++
		case flags(job.States, "RUNNING"):
			result.Running++
		default:
			result.Other++
		}
	}
	result.States = counts(states)
	return result
}
func NodeUnavailable(states []string) bool {
	return !flags(states, "IDLE", "MIXED", "ALLOCATED") || flags(states, "DOWN", "DRAIN", "FAIL", "FAILING", "NO_RESPOND", "NOT_RESPONDING", "MAINTENANCE", "UNKNOWN", "INVALID", "FUTURE", "MAINT", "RESERVED", "REBOOT_REQUESTED", "REBOOT_ISSUED", "POWER_DOWN", "POWERED_DOWN", "POWERING_DOWN", "POWERING_UP", "COMPLETING")
}
func add(target *int64, value int64) error {
	if value < 0 || *target > math.MaxInt64-value {
		return fmt.Errorf("resource total out of range")
	}
	*target += value
	return nil
}
func addResource(target *Resource, total, allocated, effective int64, unavailable bool) error {
	if allocated < 0 || effective < 0 || effective > total || allocated > effective {
		return fmt.Errorf("resource allocation exceeds configured capacity")
	}
	idle := effective - allocated
	if unavailable {
		idle = 0
	}
	for _, pair := range []struct {
		dst   *int64
		value int64
	}{{&target.Total, total}, {&target.Allocated, allocated}, {&target.Idle, idle}, {&target.Unavailable, total - allocated - idle}} {
		if err := add(pair.dst, pair.value); err != nil {
			return err
		}
	}
	return nil
}

// Parse GRES components without splitting commas inside IDX/topology annotations.
// Unknown/ambiguous GPU forms remain unknown instead of silently becoming zero.
func gpuCount(raw string, missing bool) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "(null)" || raw == "N/A" {
		return 0, !missing
	}
	parts := []string{}
	depth, start := 0, 0
	for i, c := range raw {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return 0, false
			}
		case ',':
			if depth == 0 {
				parts = append(parts, raw[start:i])
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return 0, false
	}
	parts = append(parts, raw[start:])
	var total int64
	typed, untyped := false, false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if i := strings.IndexByte(part, '('); i >= 0 {
			part = part[:i]
		}
		fields := strings.Split(part, ":")
		if fields[0] != "gpu" {
			continue
		}
		if len(fields) != 2 && len(fields) != 3 {
			return 0, false
		}
		if len(fields) == 3 {
			if fields[1] == "" {
				return 0, false
			}
			typed = true
		} else {
			untyped = true
		}
		if typed && untyped {
			return 0, false
		}
		count, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
		if err != nil || count < 0 || add(&total, count) != nil {
			return 0, false
		}
	}
	return total, true
}
func NodeSummary(nodes []models.Node) (Nodes, error) { return NodeSummaryFrom(slices.Values(nodes)) }

// NodeSummaryFrom permits partition subsets without copying full node DTOs.
func NodeSummaryFrom(nodes iter.Seq[models.Node]) (Nodes, error) {
	result := Nodes{Issues: make([]NodeIssue, 0, 5)}
	states := map[string]int{}
	gpuResource := Resource{}
	gpuTotalKnown, gpuAllocationKnown := true, true
	for node := range nodes {
		result.Total++
		down := NodeUnavailable(node.States)
		countStates(states, node.States)
		switch {
		case down:
			result.Unavailable++
			result.Issues = includePreview(result.Issues, NodeIssue{node.Name, node.States, node.Reason}, 5, func(a, b NodeIssue) int {
				return cmp.Compare(a.Name, b.Name)
			})
		case flags(node.States, "MIXED"):
			result.Mixed++
		case flags(node.States, "ALLOCATED"):
			result.Allocated++
		default:
			result.Idle++
		}
		if err := addResource(&result.CPUs, node.CPUs, node.AllocatedCPUs, node.EffectiveCPUs, down); err != nil {
			return Nodes{}, err
		}
		if err := addResource(&result.MemoryMiB, node.MemoryMiB, node.AllocatedMemoryMiB, node.MemoryMiB, down); err != nil {
			return Nodes{}, err
		}
		total, known := gpuCount(node.GRES, false)
		used, allocationKnown := gpuCount(node.GRESUsed, known && total > 0)
		if !known {
			gpuTotalKnown = false
		}
		if !known || !allocationKnown || used > total {
			gpuAllocationKnown = false
			result.GPUs.UnknownNodes++
			if known {
				if err := add(&gpuResource.Total, total); err != nil {
					return Nodes{}, err
				}
			}
			continue
		}
		if err := addResource(&gpuResource, total, used, total, down); err != nil {
			return Nodes{}, err
		}
	}
	if gpuTotalKnown {
		result.GPUs.Total = &gpuResource.Total
	}
	if gpuTotalKnown && gpuAllocationKnown {
		result.GPUs.Allocated = &gpuResource.Allocated
		result.GPUs.Idle = &gpuResource.Idle
		result.GPUs.Unavailable = &gpuResource.Unavailable
	}
	result.States = counts(states)
	return result, nil
}
func PartitionSummary(partitions []models.Partition) Partitions {
	result := Partitions{Total: len(partitions), Items: make([]models.Partition, 0, 6)}
	// Select by index so comparisons and insertions do not copy full DTOs.
	indices := make([]int, 0, 6)
	for i := range partitions {
		if flags(partitions[i].States, "UP") {
			result.Up++
		}
		indices = includePreview(indices, i, 6, func(a, b int) int {
			return cmp.Compare(partitions[a].Name, partitions[b].Name)
		})
	}
	for _, i := range indices {
		result.Items = append(result.Items, partitions[i])
	}
	return result
}
