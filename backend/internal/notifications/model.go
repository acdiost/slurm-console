// Package notifications evaluates bounded personal read-only monitoring rules.
package notifications

import (
	"context"
	"errors"
	"fmt"
	"github.com/pocketbase/pocketbase/core"
	"slices"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxPerUser = 20
const MaxRules = 100
const Interval = 30 * time.Second

var ErrForbidden = errors.New("notification permission denied")
var ErrConflict = errors.New("notification revision conflict")
var ErrLimit = errors.New("notification rule limit")
var ErrBusy = errors.New("notification check busy")
var ErrInvalid = errors.New("invalid notification rule")
var Kinds = []string{"cluster_health", "node_unavailable", "job_pending", "gpu_temperature"}

func Permission(kind string) string {
	return map[string]string{"cluster_health": "clusters.read", "node_unavailable": "nodes.read", "job_pending": "jobs.read", "gpu_temperature": "metrics.read"}[kind]
}
func Allows(a rbac.Access, kind, cluster string) bool {
	p := Permission(kind)
	return p != "" && a.Allows(p, cluster)
}

type Config struct {
	ClusterID   string `json:"clusterId"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Severity    string `json:"severity"`
	Enabled     *bool  `json:"enabled"`
	Threshold   int    `json:"threshold"`
	HoldMinutes *int   `json:"holdMinutes"`
}

type Input struct {
	Config
	Revision int `json:"revision,omitempty"`
}

func (in *Input) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Enabled == nil || in.ClusterID == "" || len(in.ClusterID) > 64 || utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 40 || strings.ContainsFunc(in.Name, unicode.IsControl) || !slices.Contains(Kinds, in.Kind) || !slices.Contains([]string{"warning", "critical"}, in.Severity) || in.HoldMinutes == nil || *in.HoldMinutes < 0 || *in.HoldMinutes > 60 || in.Threshold < 1 || in.Threshold > 10000 {
		return ErrInvalid
	}
	if in.Kind == "cluster_health" && in.Threshold != 1 || in.Kind == "job_pending" && in.Threshold > 10080 || in.Kind == "gpu_temperature" && (in.Threshold < 30 || in.Threshold > 120) {
		return ErrInvalid
	}
	return nil
}
func authorized(ctx context.Context, app core.App, owner, kind, cluster string, external bool) error {
	u, e := app.FindRecordById("users", owner)
	if e != nil || !u.GetBool("active") {
		return ErrForbidden
	}
	if external && auth.CheckBackgroundUser(ctx, app, u) != nil {
		return ErrForbidden
	}
	a, e := rbac.Resolve(app, owner)
	if e != nil {
		return e
	}
	if !Allows(a, kind, cluster) {
		return ErrForbidden
	}
	return nil
}

type Rule struct {
	ID             string   `json:"id"`
	ClusterID      string   `json:"clusterId"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Severity       string   `json:"severity"`
	Enabled        bool     `json:"enabled"`
	Threshold      int      `json:"threshold"`
	HoldMinutes    int      `json:"holdMinutes"`
	Revision       int      `json:"revision"`
	State          string   `json:"state"`
	IncidentActive bool     `json:"incidentActive"`
	PendingSince   string   `json:"pendingSince"`
	LastChecked    string   `json:"lastChecked"`
	Summary        string   `json:"summary"`
	AffectedCount  int      `json:"affectedCount"`
	Samples        []string `json:"samples"`
	Usable         bool     `json:"usable"`
	Stale          bool     `json:"stale"`
}

func RuleView(r *core.Record, a rbac.Access) Rule {
	v := Rule{ID: r.Id, ClusterID: r.GetString("cluster"), Name: r.GetString("name"), Kind: r.GetString("kind"), Severity: r.GetString("severity"), Enabled: r.GetBool("enabled"), Threshold: r.GetInt("threshold"), HoldMinutes: r.GetInt("hold_minutes"), Revision: r.GetInt("revision"), State: r.GetString("state"), IncidentActive: r.GetBool("incident_active"), PendingSince: r.GetString("pending_since"), LastChecked: r.GetString("last_checked"), Summary: r.GetString("summary"), AffectedCount: r.GetInt("affected_count"), Samples: r.GetStringSlice("samples"), Usable: Allows(a, r.GetString("kind"), r.GetString("cluster"))}
	last := r.GetDateTime("last_checked").Time()
	v.Stale = v.Enabled && v.Usable && !last.IsZero() && (time.Since(last) > 90*time.Second || time.Until(last) > 30*time.Second)
	if v.Samples == nil {
		v.Samples = []string{}
	}
	if !v.Usable {
		v.State = "blocked"
		v.Summary = "读取权限已撤销，规则暂停评估。"
		v.Samples = []string{}
		v.AffectedCount = 0
		v.IncidentActive = false
		v.PendingSince = ""
		v.LastChecked = ""
	}
	return v
}

type Observation struct {
	Known    bool
	Breached bool
	Count    int
	Samples  []string
	Summary  string
}
type State struct {
	Active  bool
	Pending time.Time
	Last    time.Time
}

// Unknown samples never recover an incident, and break a pending duration window.
func Transition(old State, o Observation, now time.Time, hold time.Duration) (State, string, string) {
	next := old
	next.Last = now
	if !o.Known {
		next.Pending = time.Time{}
		return next, "unknown", ""
	}
	if !o.Breached {
		next.Pending = time.Time{}
		next.Active = false
		if old.Active {
			return next, "ok", "recovered"
		}
		return next, "ok", ""
	}
	if old.Active {
		return next, "firing", ""
	}
	if next.Pending.IsZero() || now.Sub(old.Last) > 90*time.Second || now.Before(old.Last) {
		next.Pending = now
	}
	if now.Sub(next.Pending) >= hold {
		next.Active = true
		return next, "firing", "firing"
	}
	return next, "pending", ""
}
func summary(kind string, count int, breach bool) string {
	if !breach {
		return "本次有效采样未达到阈值。"
	}
	switch kind {
	case "cluster_health":
		return "集群控制器或 REST 健康访问异常。"
	case "node_unavailable":
		return fmt.Sprintf("%d 个节点处于 DOWN 或 NOT_RESPONDING。", count)
	case "job_pending":
		return fmt.Sprintf("%d 个当前排队作业距提交时间达到阈值。", count)
	case "gpu_temperature":
		return fmt.Sprintf("%d 个 GPU 温度达到阈值。", count)
	}
	return "监控来源暂不可用。"
}
