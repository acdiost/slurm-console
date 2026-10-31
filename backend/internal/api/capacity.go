package api

import (
	"encoding/json"
	"errors"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/capacity"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"time"
)

func capacityFresh(e *core.RequestEvent, key string) (rbac.Access, error) {
	a, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return a, err
	}
	id := e.Request.PathValue("clusterId")
	if !a.Allows("capacity.read", id) || !a.Allows("nodes.read", id) {
		return a, e.ForbiddenError("容量分析或节点读取权限已撤销。", nil)
	}
	current, _, err := capacity.Key(e.App, id)
	if err != nil {
		return a, slurmFailure(e, err)
	}
	if key != current {
		return a, slurmFailure(e, client.Failure("configuration_changed", "连接配置已变化，请刷新容量分析。"))
	}
	return a, nil
}
func capacityError(e *core.RequestEvent, err error) error {
	if errors.Is(err, capacity.ErrBusy) {
		return e.Error(429, "容量查询繁忙，请稍后刷新。", nil)
	}
	if errors.Is(err, capacity.ErrInvalid) {
		return e.Error(503, "历史容量样本不可用。", nil)
	}
	return slurmFailure(e, err)
}
func registerCapacityRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}/capacity")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("capacity.read", true)).BindFunc(rbac.Require("nodes.read", true))
	r.GET("", func(e *core.RequestEvent) error {
		if err := slurmQuery(e, "q", "sort", "order", "page", "perPage"); err != nil {
			return err
		}
		page, size, err := pagination(e)
		if err != nil {
			return err
		}
		q := e.Request.URL.Query()
		sort, order := q.Get("sort"), q.Get("order")
		if sort == "" {
			sort = "name"
		}
		if order == "" {
			order = "asc"
		}
		query := capacity.Query{Q: q.Get("q"), Sort: sort, Order: order, Page: page, PerPage: size}
		if !query.Valid() {
			return e.BadRequestError("容量分区筛选或排序不合法。", nil)
		}
		id := e.Request.PathValue("clusterId")
		key, record, err := capacity.Key(e.App, id)
		if err != nil {
			return slurmFailure(e, err)
		}
		source, err := services.SlurmFor(e.App).Read(e.Request.Context(), id, "nodes", "")
		if err != nil {
			return slurmFailure(e, err)
		}
		var nodes []models.Node
		if err = json.Unmarshal(source.Data, &nodes); err != nil {
			return e.Error(503, "节点容量数据不可用。", nil)
		}
		a, err := capacityFresh(e, key)
		if err != nil {
			return err
		}
		config := loadSlurmSection(e, id, "partitions", a.Allows("partitions.read", id), func(p []models.Partition) ([]models.Partition, error) { return p, nil })
		partitions := []models.Partition{}
		if config.Item != nil {
			partitions = *config.Item
		}
		report, err := capacity.Analyze(nodes, partitions, config.Status == "ok", query)
		if err != nil {
			return e.Error(503, "容量统计不完整或超过安全范围。", nil)
		}
		fresh, err := capacityFresh(e, key)
		if err != nil {
			return err
		}
		if !fresh.Allows("partitions.read", id) {
			config = slurmSection[[]models.Partition]{Status: "forbidden", Code: "FORBIDDEN", Message: "没有分区配置读取权限。"}
			for i := range report.Partitions {
				report.Partitions[i].States = nil
			}
		}
		// Configuration details are not exposed through the optional source status.
		return response(e, map[string]any{"cluster": rbac.ClusterDTO(record), "revision": record.GetInt("revision"), "generatedAt": time.Now().UTC().Format(time.RFC3339Nano), "source": map[string]any{"fetchedAt": source.FetchedAt, "cached": source.Cached}, "report": report, "partitionSource": map[string]any{"status": config.Status, "code": config.Code, "message": config.Message}})
	})
	r.GET("/history", func(e *core.RequestEvent) error {
		if err := slurmQuery(e, "range"); err != nil {
			return err
		}
		window := e.Request.URL.Query().Get("range")
		if window == "" {
			window = "24h"
		}
		if _, _, err := capacity.Window(window, time.Now()); err != nil {
			return e.BadRequestError("历史窗口仅支持 1h、24h 或 7d。", nil)
		}
		id := e.Request.PathValue("clusterId")
		key, record, err := capacity.Key(e.App, id)
		if err != nil {
			return slurmFailure(e, err)
		}
		history, err := capacity.For(e.App).History(e.Request.Context(), id, key, window, time.Now())
		if err != nil {
			return capacityError(e, err)
		}
		if _, err := capacityFresh(e, key); err != nil {
			return err
		}
		return response(e, map[string]any{"cluster": rbac.ClusterDTO(record), "revision": record.GetInt("revision"), "history": history})
	})
}
