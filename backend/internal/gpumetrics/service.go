package gpumetrics

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/slurm/client"
	"strconv"
	"sync"
	"time"
)

type cached struct {
	Data    []byte
	Expires time.Time
}
type flight struct {
	Done chan struct{}
	Data []byte
	Err  error
}
type Service struct {
	app      core.App
	registry *Registry
	ctx      context.Context
	mu       sync.Mutex
	cache    map[string]cached
	pending  map[string]*flight
	slots    chan struct{}
	bytes    int
}

var initMu sync.Mutex

func Register(app core.App, r *Registry) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{app: app, registry: r, ctx: ctx, cache: map[string]cached{}, pending: map[string]*flight{}, slots: make(chan struct{}, 4)}
	app.Store().Set("platform.gpuMetrics", s)
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error { cancel(); return e.Next() })
	return s
}
func For(app core.App) *Service {
	initMu.Lock()
	defer initMu.Unlock()
	if s, ok := app.Store().Get("platform.gpuMetrics").(*Service); ok {
		return s
	}
	return Register(app, &Registry{Clusters: map[string]Profile{}})
}
func (s *Service) revision(id string) (int, error) {
	r, e := s.app.FindRecordById("clusters", id)
	if e != nil {
		return 0, client.Failure("not_found", "集群不存在。")
	}
	if !r.GetBool("enabled") {
		return 0, client.Failure("disabled", "集群连接已停用。")
	}
	return r.GetInt("revision"), nil
}
func (s *Service) read(ctx context.Context, id, kind, node, uuid, window string) ([]byte, bool, error) {
	rev, e := s.revision(id)
	if e != nil {
		return nil, false, e
	}
	p, configured := s.registry.Clusters[id]
	if !configured {
		if kind != "snapshot" {
			return nil, false, client.Failure("not_found", "未配置 GPU 监控源。")
		}
		b, _ := json.Marshal(Snapshot{Status: "not_configured", Message: "尚未配置 Prometheus/DCGM 监控源。", FetchedAt: time.Now().UTC().Format(time.RFC3339Nano), Items: []Device{}})
		return b, false, nil
	}
	key := id + ":" + strconv.Itoa(rev) + ":" + kind + ":" + node + ":" + uuid + ":" + window
	s.mu.Lock()
	if c, ok := s.cache[key]; ok && time.Now().Before(c.Expires) {
		data := append([]byte{}, c.Data...)
		s.mu.Unlock()
		return data, true, nil
	}
	f, ok := s.pending[key]
	if !ok {
		select {
		case s.slots <- struct{}{}:
		default:
			s.mu.Unlock()
			return nil, false, client.Failure("busy", "GPU 查询繁忙，请稍后重试。")
		}
		f = &flight{Done: make(chan struct{})}
		s.pending[key] = f
		go s.load(id, key, kind, node, uuid, window, rev, p, f)
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case <-f.Done:
		return append([]byte{}, f.Data...), false, f.Err
	}
}
func (s *Service) load(id, key, kind, node, uuid, window string, rev int, p Profile, f *flight) {
	defer func() { <-s.slots }()
	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()
	var v any
	var e error
	ttl := 15 * time.Second
	if kind == "snapshot" {
		v, e = LoadSnapshot(ctx, p, time.Now())
	} else {
		v, e = LoadHistory(ctx, p, node, uuid, window, time.Now())
		ttl = 30 * time.Second
	}
	var data []byte
	if e == nil {
		data, e = json.Marshal(v)
	}
	latest, re := s.revision(id)
	if e == nil && (re != nil || latest != rev) {
		e = errors.New("metrics connection changed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e == nil && len(data) <= 4<<20 {
		for k, c := range s.cache {
			if time.Now().After(c.Expires) {
				s.bytes -= len(c.Data)
				delete(s.cache, k)
			}
		}
		for len(s.cache) >= 64 || s.bytes+len(data) > 8<<20 {
			for k, c := range s.cache {
				s.bytes -= len(c.Data)
				delete(s.cache, k)
				break
			}
		}
		s.cache[key] = cached{data, time.Now().Add(ttl)}
		s.bytes += len(data)
	}
	f.Data, f.Err = data, e
	delete(s.pending, key)
	close(f.Done)
}
func (s *Service) Snapshot(ctx context.Context, id string) (Snapshot, error) {
	b, c, e := s.read(ctx, id, "snapshot", "", "", "")
	if e != nil {
		return Snapshot{}, e
	}
	var v Snapshot
	e = json.Unmarshal(b, &v)
	v.Cached = c
	for i := range v.Items {
		d := &v.Items[i]
		for _, sample := range []*Sample{&d.Utilization, &d.MemoryUsed, &d.MemoryFree, &d.MemoryTotal, &d.Temperature, &d.Power} {
			if sample.Status == "fresh" && sample.Timestamp != nil && time.Now().Unix()-*sample.Timestamp > 120 {
				sample.Value = nil
				sample.Status = "stale"
			}
		}
		classify(d)
	}
	return v, e
}
func classify(d *Device) {
	valid, stale, ambiguous := 0, false, false
	for _, v := range []Sample{d.Utilization, d.MemoryUsed, d.MemoryTotal, d.Temperature, d.Power} {
		if v.Status == "fresh" {
			valid++
		}
		if v.Status == "stale" {
			stale = true
		}
		if v.Status == "ambiguous" {
			ambiguous = true
		}
	}
	switch {
	case ambiguous:
		d.Status = "ambiguous"
	case valid == 0 && stale:
		d.Status = "stale"
	case valid < 5:
		d.Status = "partial"
	default:
		d.Status = "fresh"
	}
}
func (s *Service) History(ctx context.Context, id, node, uuid, window string) (History, error) {
	snapshot, e := s.Snapshot(ctx, id)
	if e != nil {
		return History{}, e
	}
	found := false
	for _, d := range snapshot.Items {
		if d.Node == node && d.UUID == uuid && d.Status != "ambiguous" {
			found = true
		}
	}
	if !found {
		return History{}, client.Failure("not_found", "此 GPU 不在当前可见监控快照中，或身份存在冲突。")
	}
	b, c, e := s.read(ctx, id, "history", node, uuid, window)
	if e != nil {
		return History{}, e
	}
	var result History
	e = json.Unmarshal(b, &result)
	result.Cached = c
	return result, e
}
func ValidUUID(s string) bool { return uuidPattern.MatchString(s) }
