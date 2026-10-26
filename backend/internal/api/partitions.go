package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/overview"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
)

type partitionResources struct {
	Summary         overview.Nodes `json:"summary"`
	ConfiguredNodes int64          `json:"configuredNodes"`
	Complete        bool           `json:"complete"`
}
type partitionDetail struct {
	Item      models.Partition                 `json:"item"`
	FetchedAt string                           `json:"fetchedAt"`
	Cached    bool                             `json:"cached"`
	Revision  int                              `json:"revision"`
	Resources slurmSection[partitionResources] `json:"resources"`
}

func registerPartitionRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	routes := group.Group("/clusters/{clusterId}")
	routes.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("partitions.read", true))
	routes.GET("/partitions", listPartitions)
	routes.GET("/partitions/{partitionName}", getPartition)
}
func listPartitions(e *core.RequestEvent) error {
	if err := slurmQuery(e, "page", "perPage", "q", "state", "sort", "order"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	query := e.Request.URL.Query()
	key, order := query.Get("sort"), query.Get("order")
	if key == "" {
		key = "name"
	}
	if order == "" {
		order = "asc"
	}
	if !slices.Contains([]string{"name", "nodes", "cpus", "maxTime"}, key) || !slices.Contains([]string{"asc", "desc"}, order) {
		return e.BadRequestError("分区排序参数不受支持。", nil)
	}
	values, result, err := fetchList[models.Partition](e, "partitions")
	if err != nil {
		return err
	}
	filtered := make([]models.Partition, 0, len(values))
	for _, p := range values {
		if contains(query.Get("q"), p.Name) && matchesState(query, p.States) {
			filtered = append(filtered, p)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if key == "maxTime" {
			av, bv := a.MaxTimeMinutes, b.MaxTimeMinutes
			aUnknown, bUnknown := av.Value == nil && !av.Infinite, bv.Value == nil && !bv.Infinite
			if aUnknown != bUnknown {
				return !aUnknown
			}
			if av.Infinite != bv.Infinite {
				if order == "asc" {
					return !av.Infinite
				}
				return av.Infinite
			}
			if av.Value != nil && bv.Value != nil && *av.Value != *bv.Value {
				if order == "asc" {
					return *av.Value < *bv.Value
				}
				return *av.Value > *bv.Value
			}
		} else if key != "name" {
			av, bv := a.CPUs, b.CPUs
			if key == "nodes" {
				av, bv = a.NodeCount, b.NodeCount
			}
			if av != bv {
				if order == "asc" {
					return av < bv
				}
				return av > bv
			}
		}
		if key == "name" && order == "desc" {
			return a.Name > b.Name
		}
		return a.Name < b.Name
	})
	return pageResult(e, filtered, result, page, size)
}
func getPartition(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	name := e.Request.PathValue("partitionName")
	if name == "" || len(name) > 255 || strings.TrimSpace(name) != name || strings.ContainsFunc(name, unicode.IsControl) {
		return e.BadRequestError("分区名称不合法。", nil)
	}
	id := e.Request.PathValue("clusterId")
	record, err := e.App.FindRecordById("clusters", id)
	if errors.Is(err, sql.ErrNoRows) {
		return slurmFailure(e, client.Failure("not_found", "集群不存在。"))
	}
	if err != nil {
		return e.InternalServerError("", err)
	}
	revision := record.GetInt("revision")
	source, err := services.SlurmFor(e.App).Read(e.Request.Context(), id, "partitions", "")
	if err != nil {
		return slurmFailure(e, err)
	}
	var values []models.Partition
	if err := json.Unmarshal(source.Data, &values); err != nil {
		return e.InternalServerError("", err)
	}
	index := slices.IndexFunc(values, func(p models.Partition) bool { return p.Name == name })
	if index < 0 {
		return slurmFailure(e, client.Failure("not_found", "分区不存在或对连接身份不可见。"))
	}
	p := values[index]
	access, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return e.InternalServerError("", err)
	}
	result := partitionDetail{Item: p, FetchedAt: source.FetchedAt, Cached: source.Cached, Revision: revision}
	result.Resources = loadSlurmSection(e, id, "nodes", access.Allows("nodes.read", id), func(nodes []models.Node) (partitionResources, error) {
		matched := make([]models.Node, 0)
		for _, n := range nodes {
			if slices.Contains(n.Partitions, name) {
				matched = append(matched, n)
			}
		}
		summary, err := overview.NodeSummary(matched)
		return partitionResources{Summary: summary, ConfiguredNodes: p.NodeCount, Complete: int64(len(matched)) == p.NodeCount}, err
	})
	if e.Request.Context().Err() != nil {
		return e.Error(408, "分区查询已取消。", nil)
	}
	current, err := e.App.FindRecordById("clusters", id)
	if err != nil {
		return e.InternalServerError("", err)
	}
	if !current.GetBool("enabled") || current.GetInt("revision") != revision {
		return slurmFailure(e, client.Failure("configuration_changed", "连接配置已修改，请重新读取分区。"))
	}
	return response(e, result)
}
