package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/pocketbase/pocketbase/core"
	"slices"
	"slurm-console/backend/internal/gpumetrics"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"sort"
	"time"
)

type Reader func(context.Context, string, string, int, time.Time) Observation

func Source(app core.App) Reader {
	return func(ctx context.Context, cid, kind string, threshold int, now time.Time) Observation {
		unknown := Observation{Summary: "来源缺失、过期或查询失败；不能判断告警是否恢复。", Samples: []string{}}
		o := Observation{Known: true, Samples: []string{}}
		if kind == "gpu_temperature" {
			v, e := gpumetrics.For(app).Snapshot(ctx, cid)
			if e != nil || v.Status == "not_configured" || len(v.Items) == 0 {
				return unknown
			}
			complete := v.IgnoredSeries == 0
			for _, d := range v.Items {
				p := d.Temperature
				if d.Status == "ambiguous" || p.Status != "fresh" || p.Value == nil || p.Timestamp == nil || now.Unix()-*p.Timestamp > 120 || *p.Timestamp > now.Unix()+30 {
					complete = false
					continue
				}
				if *p.Value >= float64(threshold) {
					o.Count++
					o.Samples = append(o.Samples, d.Node+" / "+d.UUID)
				}
			}
			// A valid high sample proves a breach; incomplete cool samples cannot prove recovery.
			if !complete && o.Count == 0 {
				return unknown
			}
		} else {
			query := map[string]string{"cluster_health": "health", "node_unavailable": "nodes", "job_pending": "jobs"}[kind]
			if query == "" {
				return unknown
			}
			v, e := services.SlurmFor(app).Read(ctx, cid, query, "")
			if e != nil {
				var failure *client.Error
				if kind == "cluster_health" && ctx.Err() == nil && errors.As(e, &failure) && slices.Contains([]string{"timeout", "unreachable", "tls_failed", "auth_failed", "invalid_response"}, failure.Code) {
					o.Count = 1
				} else {
					return unknown
				}
			} else {
				switch kind {
				case "cluster_health":
					var h models.Health
					if json.Unmarshal(v.Data, &h) != nil || !slices.Contains([]string{"connected", "controller_down"}, h.Status) {
						return unknown
					}
					if h.Status != "connected" {
						o.Count = 1
					}
				case "node_unavailable":
					var nodes []models.Node
					if json.Unmarshal(v.Data, &nodes) != nil {
						return unknown
					}
					for _, n := range nodes {
						if slices.Contains(n.States, "DOWN") || slices.Contains(n.States, "NOT_RESPONDING") {
							o.Count++
							o.Samples = append(o.Samples, n.Name)
						}
					}
				case "job_pending":
					var jobs []models.Job
					if json.Unmarshal(v.Data, &jobs) != nil {
						return unknown
					}
					complete := true
					for _, j := range jobs {
						if !slices.Contains(j.States, "PENDING") {
							continue
						}
						if j.SubmitTime.Value == nil || j.SubmitTime.Infinite || *j.SubmitTime.Value <= 0 || *j.SubmitTime.Value > now.Unix() {
							complete = false
							continue
						}
						if now.Unix()-*j.SubmitTime.Value >= int64(threshold)*60 {
							o.Count++
							o.Samples = append(o.Samples, j.ID)
						}
					}
					if !complete && o.Count == 0 {
						return unknown
					}
				}
			}
		}
		o.Breached = o.Count > 0
		if kind == "node_unavailable" {
			o.Breached = o.Count >= threshold
		}
		sort.Strings(o.Samples)
		if len(o.Samples) > 5 {
			o.Samples = o.Samples[:5]
		}
		o.Summary = summary(kind, o.Count, o.Breached)
		return o
	}
}
