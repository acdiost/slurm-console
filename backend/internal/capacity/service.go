package capacity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"log/slog"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/overview"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"sync"
	"time"
)

// Key binds history to the reviewed configuration and actual server-owned profile.
// Only the digest is persisted; connection details are never returned to clients.
func Key(app core.App, id string) (string, *core.Record, error) {
	r, e := app.FindRecordById("clusters", id)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return "", nil, client.Failure("not_found", "集群不存在。")
		}
		return "", nil, e
	}
	if !r.GetBool("enabled") {
		return "", r, client.Failure("disabled", "集群连接已停用。")
	}
	m := clusterconfig.For(app)
	v := m.View(r)
	p, ok := m.Registry.Profiles[v.ConnectionRef]
	if !ok {
		return "", r, client.Failure("profile_unavailable", "集群连接方式不可用。")
	}
	if _, e := services.ConnectionFor(app, id); e != nil {
		return "", r, e
	}
	b, _ := json.Marshal(struct {
		Version  int
		Cluster  string
		Revision int
		Ref, API string
		Profile  clusterconfig.Profile
	}{Version, id, v.Revision, v.ConnectionRef, v.APIVersion, p})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), r, nil
}

type Service struct {
	app          core.App
	slots        chan struct{}
	historySlots chan struct{}
	mu           sync.Mutex
	pending      map[string]bool
	started      bool
	cursor       string
}

var serviceMu sync.Mutex

func For(app core.App) *Service {
	serviceMu.Lock()
	defer serviceMu.Unlock()
	if s, ok := app.Store().Get("platform.capacity").(*Service); ok {
		return s
	}
	s := &Service{app: app, slots: make(chan struct{}, 2), historySlots: make(chan struct{}, 4), pending: map[string]bool{}}
	app.Store().Set("platform.capacity", s)
	return s
}
func (s *Service) Capture(ctx context.Context, id string) error {
	s.mu.Lock()
	if s.pending[id] {
		s.mu.Unlock()
		return ErrBusy
	}
	select {
	case s.slots <- struct{}{}:
	default:
		s.mu.Unlock()
		return ErrBusy
	}
	s.pending[id] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock(); <-s.slots }()
	key, _, e := Key(s.app, id)
	if e != nil {
		return e
	}
	bucket := time.Now().UTC().Unix() / Step * Step
	n, e := s.app.CountRecords("capacity_samples", dbx.HashExp{"cluster": id, "source_key": key, "bucket": bucket})
	if e != nil || n > 0 {
		return e
	}
	source, e := services.SlurmFor(s.app).Read(ctx, id, "nodes", "")
	if e != nil {
		return e
	}
	var nodes []models.Node
	if e := json.Unmarshal(source.Data, &nodes); e != nil {
		return e
	}
	v, e := overview.NodeSummary(nodes)
	if e != nil {
		return e
	}
	frame := FromSummary(v)
	if !frame.Valid() {
		return ErrInvalid
	}
	observed, e := time.Parse(time.RFC3339Nano, source.FetchedAt)
	now := time.Now().UTC()
	if e != nil || observed.After(now.Add(5*time.Second)) || now.Sub(observed) > 30*time.Second {
		return ErrInvalid
	}
	bucket = observed.Unix() / Step * Step
	return s.app.RunInTransaction(func(tx core.App) error {
		current, _, e := Key(tx, id)
		if e != nil {
			return e
		}
		if current != key {
			return ErrChanged
		}
		n, e := tx.CountRecords("capacity_samples", dbx.HashExp{"cluster": id, "source_key": key, "bucket": bucket})
		if e != nil || n > 0 {
			return e
		}
		col, e := tx.FindCollectionByNameOrId("capacity_samples")
		if e != nil {
			return e
		}
		r := core.NewRecord(col)
		r.Set("cluster", id)
		r.Set("source_key", key)
		r.Set("bucket", bucket)
		r.Set("observed_at", observed)
		r.Set("data", frame)
		if e := tx.Save(r); e != nil {
			return e
		}
		return prune(tx, id, now)
	})
}
func prune(app core.App, id string, now time.Time) error {
	// Bound age, per-cluster history across revisions, and total platform storage.
	cutoff := now.Unix()/Step*Step - Retention
	for _, spec := range []struct {
		filter string
		params dbx.Params
		offset int
	}{{"bucket < {:cutoff}", dbx.Params{"cutoff": cutoff}, 0}, {"cluster={:cluster}", dbx.Params{"cluster": id}, 2200}, {"", nil, 100000}} {
		rows, e := app.FindRecordsByFilter("capacity_samples", spec.filter, "-bucket,-id", 1000, spec.offset, spec.params)
		if e != nil {
			return e
		}
		for _, r := range rows {
			if e := app.Delete(r); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Service) History(ctx context.Context, id, key, window string, now time.Time) (History, error) {
	select {
	case s.historySlots <- struct{}{}:
		defer func() { <-s.historySlots }()
	default:
		return History{}, ErrBusy
	}
	start, end, e := Window(window, now)
	if e != nil {
		return History{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows := []*core.Record{}
	e = s.app.RecordQuery("capacity_samples").AndWhere(dbx.HashExp{"cluster": id, "source_key": key}).AndWhere(dbx.NewExp("bucket >= {:start} AND bucket < {:end}", dbx.Params{"start": start, "end": end})).WithContext(ctx).OrderBy("bucket").Limit(2017).All(&rows)
	if e != nil {
		return History{}, e
	}
	samples := []Sample{}
	for _, r := range rows {
		var frame Frame
		if e := r.UnmarshalJSONField("data", &frame); e != nil {
			return History{}, e
		}
		samples = append(samples, Sample{Bucket: int64(r.GetInt("bucket")), ObservedAt: r.GetDateTime("observed_at").Time().UTC().Format(time.RFC3339Nano), Frame: &frame})
	}
	return Trend(start, end, samples)
}
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go func() {
		s.sweep(ctx)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sweep(ctx)
			}
		}
	}()
}
func (s *Service) sweep(parent context.Context) {
	defer func() { _ = s.app.RunInTransaction(func(tx core.App) error { return prune(tx, "", time.Now().UTC()) }) }()
	ctx, cancel := context.WithTimeout(parent, 50*time.Second)
	defer cancel()
	rows, e := s.app.FindRecordsByFilter("clusters", "enabled=true && connection_ref!='' && api_version!='' && id>{:cursor}", "id", 20, 0, dbx.Params{"cursor": s.cursor})
	if e != nil {
		return
	}
	if len(rows) == 0 {
		s.cursor = ""
		rows, e = s.app.FindRecordsByFilter("clusters", "enabled=true && connection_ref!='' && api_version!=''", "id", 20, 0)
		if e != nil {
			return
		}
	}
	work := make(chan *core.Record)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Go(func() {
			for r := range work {
				if ctx.Err() != nil {
					return
				}
				if e := s.Capture(ctx, r.Id); e != nil && !errors.Is(e, ErrBusy) && ctx.Err() == nil {
					slog.WarnContext(ctx, "capacity sample unavailable", "clusterId", r.Id)
				}
			}
		})
	}
	for _, r := range rows {
		select {
		case <-ctx.Done():
			close(work)
			workers.Wait()
			return
		case work <- r:
			s.cursor = r.Id
		}
	}
	close(work)
	workers.Wait()
}
