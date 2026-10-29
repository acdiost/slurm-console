// Package adapter normalizes the explicitly supported Slurm v0.0.43 wire contract.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
)

const Version = "v0.0.43"

type Reader interface {
	Read(context.Context, client.Resource, string) (client.Envelope, error)
}
type Adapter struct{ reader Reader }

func New(reader Reader, version string) (*Adapter, error) {
	if version != Version {
		return nil, client.Failure("api_unsupported", "Adapter 当前只支持已验证的 v0.0.43，请调整连接版本。")
	}
	return &Adapter{reader}, nil
}
func invalid() error {
	return client.Failure("invalid_response", "Slurm 返回的数据不符合已支持的 API 契约。")
}

// noVal preserves unset/infinite separately from a legitimate zero; integers
// decode without float64 so large identifiers/timestamps retain precision.
type noVal struct{ models.Number }

func (n *noVal) UnmarshalJSON(data []byte) error {
	n.Number = models.Number{}
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) > 0 && data[0] == '{' {
		var wire struct {
			Set      *bool           `json:"set"`
			Infinite bool            `json:"infinite"`
			Number   json.RawMessage `json:"number"`
		}
		if json.Unmarshal(data, &wire) != nil || wire.Set == nil {
			return fmt.Errorf("invalid numeric flags")
		}
		n.Infinite = wire.Infinite
		if *wire.Set && !wire.Infinite {
			var value int64
			if len(wire.Number) == 0 || bytes.Equal(wire.Number, []byte("null")) || json.Unmarshal(wire.Number, &value) != nil || value < 0 {
				return fmt.Errorf("invalid numeric value")
			}
			n.Value = &value
		}
		return nil
	}
	var value int64
	if json.Unmarshal(data, &value) != nil || value < 0 {
		return fmt.Errorf("invalid integer")
	}
	n.Value = &value
	return nil
}
func decode[T any](data json.RawMessage) ([]T, error) {
	if len(data) == 0 || data[0] != '[' {
		return nil, invalid()
	}
	var values []T
	if json.Unmarshal(data, &values) != nil || len(values) > 20000 {
		return nil, invalid()
	}
	if values == nil {
		values = []T{}
	}
	return values, nil
}
func validStates(states []string) bool {
	if len(states) == 0 || len(states) > 32 {
		return false
	}
	for _, state := range states {
		if state == "" || len(state) > 64 {
			return false
		}
	}
	return true
}
func (a *Adapter) Health(ctx context.Context) (models.Health, error) {
	envelope, err := a.reader.Read(ctx, client.Ping, "")
	if err != nil {
		return models.Health{}, err
	}
	pings, err := decode[struct {
		Hostname string `json:"hostname"`
		Pinged   string `json:"pinged"`
		Mode     string `json:"mode"`
	}](envelope.Pings)
	if err != nil || len(pings) == 0 || len(pings) > 16 {
		return models.Health{}, invalid()
	}
	result := models.Health{Status: "controller_down", ServerVersion: envelope.Meta.Slurm.Release, APIVersion: Version, Cluster: envelope.Meta.Slurm.Cluster, Controllers: []models.Controller{}, Capabilities: []string{"jobs.read", "nodes.read", "partitions.read"}}
	for _, ping := range pings {
		if ping.Hostname == "" || len(ping.Hostname) > 255 || (ping.Pinged != "UP" && ping.Pinged != "DOWN") || len(ping.Mode) > 32 {
			return models.Health{}, invalid()
		}
		result.Controllers = append(result.Controllers, models.Controller{Hostname: ping.Hostname, State: ping.Pinged, Role: ping.Mode})
		if ping.Pinged == "UP" {
			result.Status = "connected"
		}
	}
	return result, nil
}

type wireJob struct {
	Reservation     *string `json:"reservation"`
	UserID          *int64  `json:"user_id"`
	QOS             string  `json:"qos"`
	Priority        noVal   `json:"priority"`
	MemoryPerNode   noVal   `json:"memory_per_node"`
	MemoryPerCPU    noVal   `json:"memory_per_cpu"`
	HetID           noVal   `json:"het_job_id"`
	HetOffset       noVal   `json:"het_job_offset"`
	HetIDSet        string  `json:"het_job_id_set"`
	ArrayTaskString string  `json:"array_task_string"`
	Restarts        *int64  `json:"restart_cnt"`

	ID        *int64   `json:"job_id"`
	Name      string   `json:"name"`
	User      string   `json:"user_name"`
	Account   string   `json:"account"`
	Partition string   `json:"partition"`
	States    []string `json:"job_state"`
	Reason    string   `json:"state_reason"`
	Nodes     string   `json:"nodes"`
	CPUs      noVal    `json:"cpus"`
	NodeCount noVal    `json:"node_count"`
	TimeLimit noVal    `json:"time_limit"`
	Submit    noVal    `json:"submit_time"`
	Start     noVal    `json:"start_time"`
	End       noVal    `json:"end_time"`
	ArrayID   noVal    `json:"array_job_id"`
	ArrayTask noVal    `json:"array_task_id"`
}

func (a *Adapter) jobs(ctx context.Context, id string) ([]models.Job, error) {
	resource := client.Jobs
	if id != "" {
		resource = client.Job
	}
	envelope, err := a.reader.Read(ctx, resource, id)
	if err != nil {
		return nil, err
	}
	values, err := decode[wireJob](envelope.Jobs)
	if err != nil {
		return nil, err
	}
	result := make([]models.Job, 0, len(values))
	seen := map[int64]bool{}
	for _, j := range values {
		if j.ID == nil || *j.ID <= 0 || *j.ID > 4294967295 || seen[*j.ID] || !validStates(j.States) {
			return nil, invalid()
		}
		seen[*j.ID] = true
		reservation := ""
		if j.Reservation != nil {
			reservation = *j.Reservation
		}
		result = append(result, models.Job{Reservation: reservation, ReservationKnown: j.Reservation != nil, UserID: j.UserID, QOS: j.QOS, Priority: j.Priority.Number, MemoryPerNodeMiB: j.MemoryPerNode.Number, MemoryPerCPUMiB: j.MemoryPerCPU.Number, HetJobID: j.HetID.Number, HetJobOffset: j.HetOffset.Number, HetJobIDSet: j.HetIDSet, ArrayTaskString: j.ArrayTaskString, RestartCount: j.Restarts, ID: strconv.FormatInt(*j.ID, 10), Name: j.Name, User: j.User, Account: j.Account, Partition: j.Partition, States: j.States, Reason: j.Reason, Nodes: j.Nodes, CPUs: j.CPUs.Number, NodeCount: j.NodeCount.Number, TimeLimitMinutes: j.TimeLimit.Number, SubmitTime: j.Submit.Number, StartTime: j.Start.Number, EndTime: j.End.Number, ArrayJobID: j.ArrayID.Number, ArrayTaskID: j.ArrayTask.Number})
	}
	return result, nil
}
func (a *Adapter) GetJobs(ctx context.Context) ([]models.Job, error) { return a.jobs(ctx, "") }
func (a *Adapter) GetJob(ctx context.Context, id string) (models.Job, error) {
	values, err := a.jobs(ctx, id)
	if err != nil {
		return models.Job{}, err
	}
	for _, value := range values {
		if value.ID == id {
			return value, nil
		}
	}
	if len(values) > 0 {
		return models.Job{}, invalid()
	}
	return models.Job{}, client.Failure("not_found", "作业不存在或对连接身份不可见。")
}
func (a *Adapter) nodes(ctx context.Context, name string) ([]models.Node, error) {
	resource := client.Nodes
	if name != "" {
		resource = client.Node
	}
	envelope, err := a.reader.Read(ctx, resource, name)
	if err != nil {
		return nil, err
	}
	values, err := decode[struct {
		Address         string   `json:"address"`
		Version         string   `json:"version"`
		Architecture    string   `json:"architecture"`
		OperatingSystem string   `json:"operating_system"`
		Comment         string   `json:"comment"`
		Reservation     string   `json:"reservation"`
		ReasonUser      string   `json:"reason_set_by_user"`
		Features        []string `json:"features"`
		ActiveFeatures  []string `json:"active_features"`
		BootTime        noVal    `json:"boot_time"`
		SlurmdStartTime noVal    `json:"slurmd_start_time"`
		ReasonChangedAt noVal    `json:"reason_changed_at"`
		ResumeAfter     noVal    `json:"resume_after"`
		Name            string   `json:"name"`
		Hostname        string   `json:"hostname"`
		States          []string `json:"state"`
		CPUs            *int64   `json:"cpus"`
		AllocatedCPUs   int64    `json:"alloc_cpus"`
		EffectiveCPUs   *int64   `json:"effective_cpus"`
		Memory          *int64   `json:"real_memory"`
		AllocatedMemory int64    `json:"alloc_memory"`
		Free            noVal    `json:"free_mem"`
		Partitions      []string `json:"partitions"`
		Reason          string   `json:"reason"`
		GRES            string   `json:"gres"`
		GRESUsed        string   `json:"gres_used"`
	}](envelope.Nodes)
	if err != nil {
		return nil, err
	}
	result := make([]models.Node, 0, len(values))
	seen := map[string]bool{}
	for _, n := range values {
		if !client.ValidNodeName(n.Name) || seen[n.Name] || !validStates(n.States) || n.CPUs == nil || *n.CPUs < 0 || n.Memory == nil || *n.Memory < 0 || n.AllocatedCPUs < 0 || n.AllocatedMemory < 0 {
			return nil, invalid()
		}
		seen[n.Name] = true
		if n.Partitions == nil {
			n.Partitions = []string{}
		}
		effective := n.CPUs
		if n.EffectiveCPUs != nil {
			effective = n.EffectiveCPUs
		}
		if *effective < 0 || *effective > *n.CPUs {
			return nil, invalid()
		}
		if n.Features == nil {
			n.Features = []string{}
		}
		if n.ActiveFeatures == nil {
			n.ActiveFeatures = []string{}
		}
		result = append(result, models.Node{Address: n.Address, Version: n.Version, Architecture: n.Architecture, OperatingSystem: n.OperatingSystem, Comment: n.Comment, Reservation: n.Reservation, ReasonUser: n.ReasonUser, Features: n.Features, ActiveFeatures: n.ActiveFeatures, BootTime: n.BootTime.Number, SlurmdStartTime: n.SlurmdStartTime.Number, ReasonChangedAt: n.ReasonChangedAt.Number, ResumeAfter: n.ResumeAfter.Number, EffectiveCPUs: *effective, GRESUsed: n.GRESUsed, Name: n.Name, Hostname: n.Hostname, States: n.States, CPUs: *n.CPUs, AllocatedCPUs: n.AllocatedCPUs, MemoryMiB: *n.Memory, AllocatedMemoryMiB: n.AllocatedMemory, FreeMemoryMiB: n.Free.Number, Partitions: n.Partitions, Reason: n.Reason, GRES: n.GRES})
	}
	return result, nil
}
func (a *Adapter) GetNodes(ctx context.Context) ([]models.Node, error) { return a.nodes(ctx, "") }
func (a *Adapter) GetNode(ctx context.Context, name string) (models.Node, error) {
	values, err := a.nodes(ctx, name)
	if err != nil {
		return models.Node{}, err
	}
	for _, n := range values {
		if n.Name == name {
			return n, nil
		}
	}
	if len(values) > 0 {
		return models.Node{}, invalid()
	}
	return models.Node{}, client.Failure("not_found", "节点不存在或对连接身份不可见。")
}

type wirePartition struct {
	Name      string `json:"name"`
	Partition struct {
		States []string `json:"state"`
	} `json:"partition"`
	Nodes struct {
		Configured string `json:"configured"`
		Total      *int64 `json:"total"`
		Allowed    string `json:"allowed_allocation"`
	} `json:"nodes"`
	CPUs struct {
		Total *int64 `json:"total"`
	} `json:"cpus"`
	Defaults struct {
		Time       noVal  `json:"time"`
		MemoryCPU  noVal  `json:"partition_memory_per_cpu"`
		MemoryNode noVal  `json:"partition_memory_per_node"`
		Job        string `json:"job"`
	} `json:"defaults"`
	Maximums struct {
		Time          noVal `json:"time"`
		MemoryCPU     noVal `json:"partition_memory_per_cpu"`
		MemoryNode    noVal `json:"partition_memory_per_node"`
		Nodes         noVal `json:"nodes"`
		CPUsNode      noVal `json:"cpus_per_node"`
		CPUsSocket    noVal `json:"cpus_per_socket"`
		OverTime      noVal `json:"over_time_limit"`
		Oversubscribe struct {
			Jobs  noVal    `json:"jobs"`
			Flags []string `json:"flags"`
		} `json:"oversubscribe"`
	} `json:"maximums"`
	Minimums struct {
		Nodes noVal `json:"nodes"`
	} `json:"minimums"`
	Accounts struct {
		Allowed string `json:"allowed"`
		Deny    string `json:"deny"`
	} `json:"accounts"`
	Groups struct {
		Allowed string `json:"allowed"`
	} `json:"groups"`
	QOS struct {
		Allowed  string `json:"allowed"`
		Deny     string `json:"deny"`
		Assigned string `json:"assigned"`
	} `json:"qos"`
	TRES struct {
		Configured string `json:"configured"`
		Billing    string `json:"billing_weights"`
	} `json:"tres"`
	Priority struct {
		Tier      noVal `json:"tier"`
		JobFactor noVal `json:"job_factor"`
	} `json:"priority"`
	Grace      noVal    `json:"grace_time"`
	Alternate  string   `json:"alternate"`
	NodeSets   string   `json:"node_sets"`
	Topology   string   `json:"topology"`
	SelectType []string `json:"select_type"`
}

func (a *Adapter) GetPartitions(ctx context.Context) ([]models.Partition, error) {
	envelope, err := a.reader.Read(ctx, client.Partitions, "")
	if err != nil {
		return nil, err
	}
	values, err := decode[wirePartition](envelope.Partitions)
	if err != nil {
		return nil, err
	}
	result := make([]models.Partition, 0, len(values))
	seen := map[string]bool{}
	for _, p := range values {
		if strings.TrimSpace(p.Name) == "" || len(p.Name) > 255 || seen[p.Name] || !validStates(p.Partition.States) || p.Nodes.Total == nil || *p.Nodes.Total < 0 || p.CPUs.Total == nil || *p.CPUs.Total < 0 {
			return nil, invalid()
		}
		seen[p.Name] = true
		if p.SelectType == nil {
			p.SelectType = []string{}
		}
		if p.Maximums.Oversubscribe.Flags == nil {
			p.Maximums.Oversubscribe.Flags = []string{}
		}
		result = append(result, models.Partition{
			Name: p.Name, States: p.Partition.States, Nodes: p.Nodes.Configured, NodeCount: *p.Nodes.Total, CPUs: *p.CPUs.Total, DefaultTimeMinutes: p.Defaults.Time.Number, MaxTimeMinutes: p.Maximums.Time.Number,
			AllowedAccounts: p.Accounts.Allowed, DeniedAccounts: p.Accounts.Deny, AllowedGroups: p.Groups.Allowed, AllowedQOS: p.QOS.Allowed, DeniedQOS: p.QOS.Deny, AssignedQOS: p.QOS.Assigned,
			AllowedAllocationNodes: p.Nodes.Allowed, Alternate: p.Alternate, ConfiguredTRES: p.TRES.Configured, BillingWeights: p.TRES.Billing, NodeSets: p.NodeSets, Topology: p.Topology, SelectType: p.SelectType, DefaultJob: p.Defaults.Job,
			DefaultMemoryPerCPUMiB: p.Defaults.MemoryCPU.Number, DefaultMemoryPerNodeMiB: p.Defaults.MemoryNode.Number, MaxMemoryPerCPUMiB: p.Maximums.MemoryCPU.Number, MaxMemoryPerNodeMiB: p.Maximums.MemoryNode.Number,
			MinNodes: p.Minimums.Nodes.Number, MaxNodes: p.Maximums.Nodes.Number, MaxCPUsPerNode: p.Maximums.CPUsNode.Number, MaxCPUsPerSocket: p.Maximums.CPUsSocket.Number,
			PriorityTier: p.Priority.Tier.Number, PriorityJobFactor: p.Priority.JobFactor.Number, GraceTimeSeconds: p.Grace.Number, OverTimeLimitMinutes: p.Maximums.OverTime.Number,
			OversubscribeJobs: p.Maximums.Oversubscribe.Jobs.Number, OversubscribeFlags: p.Maximums.Oversubscribe.Flags,
		})
	}
	return result, nil
}
