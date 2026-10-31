package api

import (
	"github.com/pocketbase/pocketbase/core"
	"slices"
	"slurm-console/backend/internal/gpumetrics"
	"slurm-console/backend/internal/slurm/client"
	"strings"
	"unicode"
	"unicode/utf8"
)

func gpuEfficiency(e *core.RequestEvent) error {
	if err := slurmQuery(e, "q", "status", "sort", "order", "range", "page", "perPage"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	q := e.Request.URL.Query()
	window, sort, order := q.Get("range"), q.Get("sort"), q.Get("order")
	if window == "" {
		window = "1h"
	}
	if sort == "" {
		sort = "node"
	}
	if order == "" {
		order = "asc"
	}
	if !slices.Contains([]string{"1h", "6h", "24h"}, window) || !slices.Contains([]string{"node", "mean", "coverage", "low", "memory", "power"}, sort) || !slices.Contains([]string{"asc", "desc"}, order) || !slices.Contains([]string{"", "low", "active", "insufficient", "ambiguous"}, q.Get("status")) || len(q.Get("q")) > 128 || !utf8.ValidString(q.Get("q")) || strings.ContainsFunc(q.Get("q"), unicode.IsControl) || size > 100 {
		return e.BadRequestError("GPU 效率筛选或窗口不合法。", nil)
	}
	cid := e.Request.PathValue("clusterId")
	record, err := e.App.FindRecordById("clusters", cid)
	if err != nil {
		return e.NotFoundError("集群不存在。", nil)
	}
	revision := record.GetInt("revision")
	if !record.GetBool("enabled") {
		return slurmFailure(e, client.Failure("disabled", "集群连接已停用。"))
	}
	v, err := gpumetrics.For(e.App).Efficiency(e.Request.Context(), cid, window)
	if err != nil {
		return e.Error(503, "GPU 效率来源暂不可用，请检查监控连接、采样和标签。", nil)
	}
	if err := gpuReadAllowed(e); err != nil {
		return err
	}
	current, err := e.App.FindRecordById("clusters", cid)
	if err != nil || !current.GetBool("enabled") || current.GetInt("revision") != revision {
		return slurmFailure(e, client.Failure("configuration_changed", "集群配置变化，请重新查询。"))
	}
	filtered := []gpumetrics.EfficiencyDevice{}
	for _, d := range v.Items {
		if contains(q.Get("q"), d.Node+" "+d.UUID+" "+d.Model) && (q.Get("status") == "" || d.Status == q.Get("status")) {
			filtered = append(filtered, d)
		}
	}
	key := func(d gpumetrics.EfficiencyDevice) *float64 {
		switch sort {
		case "mean":
			return d.Utilization.Mean
		case "coverage":
			x := d.Utilization.CoveragePercent
			return &x
		case "low":
			return d.LowSamplePercent
		case "memory":
			return d.MemoryPercent.Mean
		case "power":
			return d.PowerW.Mean
		}
		return nil
	}
	slices.SortFunc(filtered, func(a, b gpumetrics.EfficiencyDevice) int {
		cmp := 0
		if sort != "node" {
			x, y := key(a), key(b)
			if x == nil && y != nil {
				return 1
			}
			if x != nil && y == nil {
				return -1
			}
			if x != nil && y != nil {
				if *x < *y {
					cmp = -1
				}
				if *x > *y {
					cmp = 1
				}
			}
		}
		if cmp != 0 {
			if order == "desc" {
				return -cmp
			}
			return cmp
		}
		cmp = strings.Compare(a.Node+"/"+a.UUID, b.Node+"/"+b.UUID)
		if sort == "node" && order == "desc" {
			return -cmp
		}
		return cmp
	})
	total := len(filtered)
	start := min((page-1)*size, total)
	v.Items = filtered[start:min(start+size, total)]
	return response(e, map[string]any{"revision": revision, "analysis": v, "page": page, "perPage": size, "totalItems": total})
}
