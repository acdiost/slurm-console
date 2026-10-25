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
		result = append(result, models.Job{ID: strconv.FormatInt(*j.ID, 10), Name: j.Name, User: j.User, Account: j.Account, Partition: j.Partition, States: j.States, Reason: j.Reason, Nodes: j.Nodes, CPUs: j.CPUs.Number, NodeCount: j.NodeCount.Number, TimeLimitMinutes: j.TimeLimit.Number, SubmitTime: j.Submit.Number, StartTime: j.Start.Number, EndTime: j.End.Number, ArrayJobID: j.ArrayID.Number, ArrayTaskID: j.ArrayTask.Number})
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
		result = append(result, models.Node{EffectiveCPUs: *effective, GRESUsed: n.GRESUsed, Name: n.Name, Hostname: n.Hostname, States: n.States, CPUs: *n.CPUs, AllocatedCPUs: n.AllocatedCPUs, MemoryMiB: *n.Memory, AllocatedMemoryMiB: n.AllocatedMemory, FreeMemoryMiB: n.Free.Number, Partitions: n.Partitions, Reason: n.Reason, GRES: n.GRES})
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
func (a *Adapter) GetPartitions(ctx context.Context) ([]models.Partition, error) {
	envelope, err := a.reader.Read(ctx, client.Partitions, "")
	if err != nil {
		return nil, err
	}
	values, err := decode[struct {
		Name      string `json:"name"`
		Partition struct {
			States []string `json:"state"`
		} `json:"partition"`
		Nodes struct {
			Configured string `json:"configured"`
			Total      *int64 `json:"total"`
		} `json:"nodes"`
		CPUs struct {
			Total *int64 `json:"total"`
		} `json:"cpus"`
		Defaults struct {
			Time noVal `json:"time"`
		} `json:"defaults"`
		Maximums struct {
			Time noVal `json:"time"`
		} `json:"maximums"`
	}](envelope.Partitions)
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
		result = append(result, models.Partition{Name: p.Name, States: p.Partition.States, Nodes: p.Nodes.Configured, NodeCount: *p.Nodes.Total, CPUs: *p.CPUs.Total, DefaultTimeMinutes: p.Defaults.Time.Number, MaxTimeMinutes: p.Maximums.Time.Number})
	}
	return result, nil
}
