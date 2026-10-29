package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/slurm/adapter"
	"slurm-console/backend/internal/slurm/client"
)

type SlurmResult struct {
	Data      json.RawMessage
	FetchedAt string
	Cached    bool
}
type entry struct {
	result  SlurmResult
	expires time.Time
}
type flight struct {
	done   chan struct{}
	result SlurmResult
	err    error
}
type Slurm struct {
	app     core.App
	ctx     context.Context
	mu      sync.Mutex
	cache   map[string]entry
	pending map[string]*flight
	slots   chan struct{}
	bytes   int
	epochs  map[string]uint64
}

var serviceMu sync.Mutex

func SlurmFor(app core.App) *Slurm {
	serviceMu.Lock()
	defer serviceMu.Unlock()
	if existing, ok := app.Store().Get("platform.slurmAdapter").(*Slurm); ok {
		return existing
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Slurm{app: app, ctx: ctx, cache: map[string]entry{}, pending: map[string]*flight{}, slots: make(chan struct{}, 4), epochs: map[string]uint64{}}
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error { cancel(); return e.Next() })
	app.Store().Set("platform.slurmAdapter", s)
	return s
}
func (s *Slurm) Read(ctx context.Context, clusterID, kind, id string) (SlurmResult, error) {
	connection, err := ConnectionFor(s.app, clusterID)
	if err != nil {
		return SlurmResult{}, err
	}
	s.mu.Lock()
	epoch := s.epochs[clusterID]
	key := clusterID + ":" + strconv.Itoa(connection.Revision) + ":" + strconv.FormatUint(epoch, 10) + ":" + kind + ":" + id
	if cached, ok := s.cache[key]; ok && time.Now().Before(cached.expires) {
		result := cached.result
		result.Data = append(json.RawMessage(nil), result.Data...)
		result.Cached = true
		s.mu.Unlock()
		return result, nil
	}
	current, exists := s.pending[key]
	if !exists {
		select {
		case s.slots <- struct{}{}:
		default:
			s.mu.Unlock()
			return SlurmResult{}, client.Failure("busy", "Slurm 查询繁忙，请稍后重试。")
		}
		current = &flight{done: make(chan struct{})}
		s.pending[key] = current
		go s.load(key, clusterID, connection.Revision, epoch, kind, id, connection.Adapter, current)
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SlurmResult{}, ctx.Err()
	case <-current.done:
		result := current.result
		result.Data = append(json.RawMessage(nil), result.Data...)
		return result, current.err
	}
}
func (s *Slurm) load(key, clusterID string, revision int, epoch uint64, kind, id string, a *adapter.Adapter, current *flight) {
	defer func() { <-s.slots }()
	var value any
	var err error
	ttl := 5 * time.Second
	switch kind {
	case "accounts":
		value, err = a.GetAccounts(s.ctx)
		ttl = 60 * time.Second
	case "slurm_users":
		value, err = a.GetSlurmUsers(s.ctx)
		ttl = 60 * time.Second
	case "associations":
		value, err = a.GetAssociations(s.ctx)
		ttl = 60 * time.Second
	case "health":
		value, err = a.Health(s.ctx)
	case "jobs":
		value, err = a.GetJobs(s.ctx)
	case "job":
		value, err = a.GetJob(s.ctx, id)
	case "nodes":
		value, err = a.GetNodes(s.ctx)
		ttl = 10 * time.Second
	case "node":
		value, err = a.GetNode(s.ctx, id)
		ttl = 10 * time.Second
	case "partitions":
		value, err = a.GetPartitions(s.ctx)
		ttl = 15 * time.Second
	default:
		err = client.Failure("invalid_request", "不支持的 Slurm 查询。")
	}
	var data []byte
	if err == nil {
		data, err = json.Marshal(value)
	}
	if err == nil {
		record, readErr := s.app.FindRecordById("clusters", clusterID)
		if readErr != nil {
			err = readErr
		} else if !record.GetBool("enabled") || record.GetInt("revision") != revision {
			err = client.Failure("configuration_changed", "连接配置已修改，请重新查询。")
		}
	}
	result := SlurmResult{Data: data, FetchedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil && s.epochs[clusterID] != epoch {
		err = client.Failure("data_changed", "作业数据已变化，请重新读取。")
	}
	if err == nil {
		// Bound both entry count and total serialized DTO memory; never cache failures.
		for cacheKey, cached := range s.cache {
			if !time.Now().Before(cached.expires) {
				s.bytes -= len(cached.result.Data)
				delete(s.cache, cacheKey)
			}
		}
		if len(data) <= 32<<20 {
			for len(s.cache) >= 128 || s.bytes+len(data) > 32<<20 {
				for cacheKey, cached := range s.cache {
					s.bytes -= len(cached.result.Data)
					delete(s.cache, cacheKey)
					break
				}
			}
			s.cache[key] = entry{result: result, expires: time.Now().Add(ttl)}
			s.bytes += len(data)
		}
	}
	current.result = result
	current.err = err
	delete(s.pending, key)
	close(current.done)
}

type Connection struct {
	Client   *client.Client
	Adapter  *adapter.Adapter
	Revision int
	Timeout  time.Duration
}

func ConnectionFor(app core.App, clusterID string) (Connection, error) {
	record, err := app.FindRecordById("clusters", clusterID)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, client.Failure("not_found", "集群不存在。")
	}
	if err != nil {
		return Connection{}, err
	}
	if !record.GetBool("enabled") {
		return Connection{}, client.Failure("disabled", "集群连接已停用。")
	}
	manager := clusterconfig.For(app)
	view := manager.View(record)
	profile, ok := manager.Registry.Profiles[view.ConnectionRef]
	if !ok {
		return Connection{}, client.Failure("profile_unavailable", "服务端集群连接方式不可用。")
	}
	// Check the approved version again even when a prior result is cached.
	approved := false
	for _, v := range profile.Versions {
		if v == view.APIVersion {
			approved = true
		}
	}
	if !approved {
		return Connection{}, client.Failure("api_unsupported", "连接版本未获服务端配置批准。")
	}
	connection, err := profile.Client(view.APIVersion, time.Duration(view.TimeoutSeconds)*time.Second)
	if err != nil {
		return Connection{}, err
	}
	adapted, err := adapter.New(connection, view.APIVersion)
	if err != nil {
		return Connection{}, err
	}
	return Connection{connection, adapted, view.Revision, time.Duration(view.TimeoutSeconds) * time.Second}, nil
}
func (s *Slurm) Invalidate(clusterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epochs[clusterID]++
	for key, cached := range s.cache {
		if strings.HasPrefix(key, clusterID+":") {
			s.bytes -= len(cached.result.Data)
			delete(s.cache, key)
		}
	}
}

// Global accounting identity metadata is shared across profile/cluster views.
func (s *Slurm) InvalidateAccounting() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, cached := range s.cache {
		if strings.Contains(key, ":accounts:") || strings.Contains(key, ":slurm_users:") || strings.Contains(key, ":associations:") {
			s.bytes -= len(cached.result.Data)
			delete(s.cache, key)
		}
	}
	// In-flight reads also become stale, including aliases to the same SlurmDBD.
	for cluster := range s.epochs {
		s.epochs[cluster]++
	}
	for key := range s.pending {
		cluster, _, _ := strings.Cut(key, ":")
		s.epochs[cluster]++
	}
}
