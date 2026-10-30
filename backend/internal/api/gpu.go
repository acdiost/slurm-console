package api

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slices"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/gpumetrics"
	"slurm-console/backend/internal/overview"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strings"
)

func registerGPURoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}/metrics/gpu")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("metrics.read", true))
	r.GET("", listGPU)
	r.GET("/{uuid}/history", func(e *core.RequestEvent) error {
		if err := slurmQuery(e, "node", "range"); err != nil {
			return err
		}
		record, err := e.App.FindRecordById("clusters", e.Request.PathValue("clusterId"))
		if err != nil {
			return e.NotFoundError("集群不存在。", nil)
		}
		revision := record.GetInt("revision")
		uuid, node := e.Request.PathValue("uuid"), e.Request.URL.Query().Get("node")
		window := e.Request.URL.Query().Get("range")
		if window == "" {
			window = "1h"
		}
		if !gpumetrics.ValidUUID(uuid) || !client.ValidNodeName(node) || !slices.Contains([]string{"1h", "6h", "24h"}, window) {
			return e.BadRequestError("GPU 身份或趋势窗口不合法。", nil)
		}
		v, err := gpumetrics.For(e.App).History(e.Request.Context(), e.Request.PathValue("clusterId"), node, uuid, window)
		if err != nil {
			if client.Code(err) == "not_found" {
				return slurmFailure(e, err)
			}
			return e.Error(503, "GPU 趋势暂不可用。", nil)
		}
		if err := gpuReadAllowed(e); err != nil {
			return err
		}
		current, err := e.App.FindRecordById("clusters", e.Request.PathValue("clusterId"))
		if err != nil || !current.GetBool("enabled") || current.GetInt("revision") != revision {
			return slurmFailure(e, client.Failure("configuration_changed", "集群配置变化，请重新查询。"))
		}
		return response(e, v)
	})
}
func gpuReadAllowed(e *core.RequestEvent) error {
	a, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return err
	}
	if !a.Allows("metrics.read", e.Request.PathValue("clusterId")) {
		return e.ForbiddenError("GPU 监控读取权限已撤销。", nil)
	}
	return nil
}
func listGPU(e *core.RequestEvent) error {
	if err := slurmQuery(e, "q", "node", "status", "sort", "order", "page", "perPage"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	q := e.Request.URL.Query()
	status := q.Get("status")
	sort, order := q.Get("sort"), q.Get("order")
	if sort == "" {
		sort = "node"
	}
	if order == "" {
		order = "asc"
	}
	if !slices.Contains([]string{"", "fresh", "partial", "stale", "ambiguous"}, status) || !slices.Contains([]string{"node", "utilization", "temperature"}, sort) || !slices.Contains([]string{"asc", "desc"}, order) || q.Get("node") != "" && !client.ValidNodeName(q.Get("node")) {
		return e.BadRequestError("GPU 筛选或排序不合法。", nil)
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
	snapshot, err := gpumetrics.For(e.App).Snapshot(e.Request.Context(), cid)
	if err != nil {
		snapshot = gpumetrics.Snapshot{Status: "error", Message: "GPU 监控源暂不可用，请检查连接、采集和标签配置。", Items: []gpumetrics.Device{}}
	}
	access, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return err
	}
	allocation := slurmSection[overview.Nodes]{Status: "forbidden", Code: "FORBIDDEN", Message: "未授予节点读取权限，未查询 Slurm 分配量。"}
	if access.Allows("nodes.read", cid) {
		allocation = loadSlurmSection(e, cid, "nodes", true, func(nodes []models.Node) (overview.Nodes, error) { return overview.NodeSummary(nodes) })
	}
	if err := gpuReadAllowed(e); err != nil {
		return err
	}
	access, err = rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return err
	}
	if !access.Allows("nodes.read", cid) {
		allocation = slurmSection[overview.Nodes]{Status: "forbidden", Code: "FORBIDDEN", Message: "节点读取权限已撤销。"}
	}
	current, err := e.App.FindRecordById("clusters", cid)
	if err != nil {
		return e.InternalServerError("", nil)
	}
	if !current.GetBool("enabled") || current.GetInt("revision") != revision {
		return slurmFailure(e, client.Failure("configuration_changed", "集群配置变化，请重新查询。"))
	}
	filtered := []gpumetrics.Device{}
	for _, d := range snapshot.Items {
		if contains(q.Get("q"), d.Node+" "+d.UUID+" "+d.Model) && (q.Get("node") == "" || d.Node == q.Get("node")) && (status == "" || d.Status == status) {
			filtered = append(filtered, d)
		}
	}
	slices.SortFunc(filtered, func(a, b gpumetrics.Device) int {
		cmp := 0
		if sort != "node" {
			x, y := a.Utilization.Value, b.Utilization.Value
			if sort == "temperature" {
				x, y = a.Temperature.Value, b.Temperature.Value
			}
			if x == nil && y != nil {
				return 1
			}
			if x != nil && y == nil {
				return -1
			}
			if x != nil && y != nil {
				if *x < *y {
					cmp = -1
				} else if *x > *y {
					cmp = 1
				}
			}
		}
		if cmp == 0 {
			cmp = strings.Compare(a.Node+"/"+a.UUID, b.Node+"/"+b.UUID)
		}
		if order == "desc" {
			cmp = -cmp
		}
		return cmp
	})
	total := len(filtered)
	start := min((page-1)*size, total)
	counts := map[string]int{"fresh": 0, "partial": 0, "stale": 0, "ambiguous": 0}
	for _, d := range filtered {
		counts[d.Status]++
	}
	items := filtered[start:min(start+size, total)]
	snapshot.Items = []gpumetrics.Device{} // Page items are returned once; source metadata never includes hidden or unpaged rows.
	return response(e, map[string]any{"monitoring": snapshot, "allocation": allocation, "items": items, "summary": counts, "page": page, "perPage": size, "totalItems": total, "revision": revision})
}
