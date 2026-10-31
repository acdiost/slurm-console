package notifications

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"sync"
	"time"
)

// One evaluator owns each rule; all manual and scheduled checks share two slots.
// Changed events are coalesced hints; durable data is always fetched via the API.
type Service struct {
	app         core.App
	reader      Reader
	mu          sync.Mutex
	checking    map[string]bool
	slots       chan struct{}
	subscribers map[string]map[chan struct{}]bool
	started     bool
	done        chan struct{}
	stopped     sync.Once
}

var serviceMu sync.Mutex

func Register(app core.App, reader Reader) *Service {
	s := &Service{done: make(chan struct{}), app: app, reader: reader, checking: map[string]bool{}, slots: make(chan struct{}, 2), subscribers: map[string]map[chan struct{}]bool{}}
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error { s.stopped.Do(func() { close(s.done) }); return e.Next() })
	app.Store().Set("platform.notifications", s)
	return s
}
func For(app core.App) *Service {
	serviceMu.Lock()
	defer serviceMu.Unlock()
	if s, ok := app.Store().Get("platform.notifications").(*Service); ok {
		return s
	}
	return Register(app, Source(app))
}
func (s *Service) Subscribe(owner string) (<-chan struct{}, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, v := range s.subscribers {
		total += len(v)
	}
	if total >= 64 || len(s.subscribers[owner]) >= 3 {
		return nil, nil, ErrBusy
	}
	if s.subscribers[owner] == nil {
		s.subscribers[owner] = map[chan struct{}]bool{}
	}
	c := make(chan struct{}, 1)
	s.subscribers[owner][c] = true
	return c, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subscribers[owner], c)
		if len(s.subscribers[owner]) == 0 {
			delete(s.subscribers, owner)
		}
	}, nil
}
func (s *Service) Changed(owner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.subscribers[owner] {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}
func Own(app core.App, owner, id string) (*core.Record, error) {
	return app.FindFirstRecordByFilter("notification_rules", "id={:id} && owner={:owner}", dbx.Params{"id": id, "owner": owner})
}
func (s *Service) Save(ctx context.Context, owner, id string, in Input) (*core.Record, error) {
	if id != "" && in.Revision < 1 {
		return nil, ErrInvalid
	}
	if e := in.Validate(); e != nil {
		return nil, e
	}
	if e := authorized(ctx, s.app, owner, in.Kind, in.ClusterID, true); e != nil {
		return nil, e
	}
	var result *core.Record
	e := s.app.RunInTransaction(func(tx core.App) error {
		c, e := tx.FindRecordById("clusters", in.ClusterID)
		if e != nil {
			return e
		}
		if !c.GetBool("enabled") && *in.Enabled {
			return ErrInvalid
		}
		if e := authorized(ctx, tx, owner, in.Kind, in.ClusterID, false); e != nil {
			return e
		}
		var r *core.Record
		if id != "" {
			r, e = Own(tx, owner, id)
			if e != nil {
				return e
			}
			if in.Revision < 1 || r.GetInt("revision") != in.Revision {
				return ErrConflict
			}
			if r.GetString("cluster") != in.ClusterID {
				return ErrInvalid
			}
		} else {
			if in.Revision != 0 {
				return ErrInvalid
			}
			n, e := tx.CountRecords("notification_rules", dbx.HashExp{"owner": owner})
			if e != nil {
				return e
			}
			total, e := tx.CountRecords("notification_rules")
			if e != nil {
				return e
			}
			if n >= MaxPerUser || total >= MaxRules {
				return ErrLimit
			}
			col, e := tx.FindCollectionByNameOrId("notification_rules")
			if e != nil {
				return e
			}
			r = core.NewRecord(col)
		}
		for k, v := range map[string]any{"owner": owner, "cluster": in.ClusterID, "name": in.Name, "kind": in.Kind, "severity": in.Severity, "enabled": *in.Enabled, "threshold": in.Threshold, "hold_minutes": *in.HoldMinutes, "revision": in.Revision + 1, "state": "unknown", "incident_active": false, "incident_id": "", "pending_since": "", "last_checked": "", "summary": "等待有效采样。", "affected_count": 0, "samples": []string{}} {
			r.Set(k, v)
		}
		if !*in.Enabled {
			r.Set("state", "disabled")
			r.Set("summary", "规则已停用。")
		}
		if e := tx.Save(r); e != nil {
			return e
		}
		result = r
		return nil
	})
	if e == nil {
		s.Changed(owner)
	}
	return result, e
}
func (s *Service) Delete(owner, id string, rev int) error {
	if rev < 1 {
		return ErrInvalid
	}
	e := s.app.RunInTransaction(func(tx core.App) error {
		r, e := Own(tx, owner, id)
		if e != nil {
			return e
		}
		if rev < 1 || r.GetInt("revision") != rev {
			return ErrConflict
		}
		return tx.Delete(r)
	})
	if e == nil {
		s.Changed(owner)
	}
	return e
}
func (s *Service) Evaluate(ctx context.Context, owner, id string) (*core.Record, error) {
	// Resolve ownership before reporting per-rule activity or taking shared capacity.
	r, e := Own(s.app, owner, id)
	if e != nil {
		return nil, e
	}
	s.mu.Lock()
	if s.checking[id] {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	select {
	case s.slots <- struct{}{}:
	default:
		s.mu.Unlock()
		return nil, ErrBusy
	}
	s.checking[id] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.checking, id); s.mu.Unlock(); <-s.slots }()
	if !r.GetBool("enabled") {
		return nil, ErrInvalid
	}
	if e := authorized(ctx, s.app, owner, r.GetString("kind"), r.GetString("cluster"), true); e != nil {
		return nil, e
	}
	// Duplicate clicks and a concurrent scheduler tick reuse the last sample.
	now := time.Now().UTC()
	if last := r.GetDateTime("last_checked").Time(); !last.IsZero() && now.Sub(last) >= 0 && now.Sub(last) < 5*time.Second {
		return r, nil
	}
	cluster, e := s.app.FindRecordById("clusters", r.GetString("cluster"))
	if e != nil {
		return nil, e
	}
	revision := cluster.GetInt("revision")
	o := Observation{Summary: "集群连接已停用；不能判断告警是否恢复。", Samples: []string{}}
	if cluster.GetBool("enabled") {
		o = s.reader(ctx, r.GetString("cluster"), r.GetString("kind"), r.GetInt("threshold"), now)
		now = time.Now().UTC()
	}
	if e := authorized(ctx, s.app, owner, r.GetString("kind"), r.GetString("cluster"), true); e != nil {
		return nil, e
	}
	var stored *core.Record
	e = s.app.RunInTransaction(func(tx core.App) error {
		current, e := Own(tx, owner, id)
		if e != nil {
			return e
		}
		if current.GetInt("revision") != r.GetInt("revision") || !current.GetBool("enabled") {
			return ErrConflict
		}
		if e := authorized(ctx, tx, owner, r.GetString("kind"), r.GetString("cluster"), false); e != nil {
			return e
		}
		c, e := tx.FindRecordById("clusters", r.GetString("cluster"))
		if e != nil {
			return e
		}
		if c.GetInt("revision") != revision || c.GetBool("enabled") != cluster.GetBool("enabled") {
			return ErrConflict
		}
		before := State{Active: current.GetBool("incident_active"), Pending: current.GetDateTime("pending_since").Time(), Last: current.GetDateTime("last_checked").Time()}
		next, status, phase := Transition(before, o, now, time.Duration(r.GetInt("hold_minutes"))*time.Minute)
		current.Set("state", status)
		current.Set("incident_active", next.Active)
		current.Set("last_checked", now)
		current.Set("pending_since", next.Pending)
		current.Set("summary", o.Summary)
		current.Set("affected_count", o.Count)
		if o.Samples == nil {
			o.Samples = []string{}
		}
		current.Set("samples", o.Samples)
		if phase == "firing" {
			current.Set("incident_id", rand.Text())
		}
		if phase != "" {
			col, e := tx.FindCollectionByNameOrId("notifications")
			if e != nil {
				return e
			}
			n := core.NewRecord(col)
			message := o.Summary
			if phase == "recovered" {
				message = "已取得有效采样，原告警条件已恢复。"
			}
			for k, v := range map[string]any{"owner": owner, "cluster": r.GetString("cluster"), "rule_id": id, "rule_name": r.GetString("name"), "kind": r.GetString("kind"), "severity": r.GetString("severity"), "phase": phase, "incident_id": current.GetString("incident_id"), "message": message, "affected_count": o.Count, "samples": o.Samples, "read": false} {
				n.Set(k, v)
			}
			if e := tx.Save(n); e != nil {
				return e
			}
			if e := prune(tx, owner); e != nil {
				return e
			}
		}
		if e := tx.Save(current); e != nil {
			return e
		}
		stored = current
		return nil
	})
	if e == nil {
		s.Changed(owner)
	}
	return stored, e
}
func prune(app core.App, owner string) error {
	// Indexed bounded history, with an additional age bound. Deletion is preference retention, not control audit mutation.
	for _, spec := range []struct {
		filter string
		params dbx.Params
		offset int
	}{{"owner={:owner}", dbx.Params{"owner": owner}, 200}, {"", nil, 10000}, {"created < {:cutoff}", dbx.Params{"cutoff": time.Now().UTC().Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05.000Z")}, 0}} {
		rows, e := app.FindRecordsByFilter("notifications", spec.filter, "-created,-id", 1000, spec.offset, spec.params)
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
		ticker := time.NewTicker(Interval)
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
	defer func() { _ = s.app.RunInTransaction(func(tx core.App) error { return prune(tx, "") }) }()
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	rows, e := s.app.FindRecordsByFilter("notification_rules", "enabled=true", "last_checked,id", MaxRules, 0)
	if e != nil {
		return
	}
	var wg sync.WaitGroup
	work := make(chan *core.Record)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range work {
				if ctx.Err() != nil {
					return
				}
				_, e := s.Evaluate(ctx, r.GetString("owner"), r.Id)
				if e != nil && !errors.Is(e, sql.ErrNoRows) {
					continue
				}
			}
		}()
	}
	for _, r := range rows {
		select {
		case <-ctx.Done():
			close(work)
			wg.Wait()
			return
		case work <- r:
		}
	}
	close(work)
	wg.Wait()
}

func (s *Service) Done() <-chan struct{} { return s.done }
