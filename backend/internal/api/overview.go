package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

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

type overviewSection[T any] struct {
	Status    string `json:"status"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	FetchedAt string `json:"fetchedAt"`
	Cached    bool   `json:"cached"`
	Item      *T     `json:"item"`
}
type clusterOverview struct {
	Cluster     rbac.Cluster                         `json:"cluster"`
	Revision    int                                  `json:"revision"`
	GeneratedAt string                               `json:"generatedAt"`
	Health      overviewSection[models.Health]       `json:"health"`
	Nodes       overviewSection[overview.Nodes]      `json:"nodes"`
	Jobs        overviewSection[overview.Jobs]       `json:"jobs"`
	Partitions  overviewSection[overview.Partitions] `json:"partitions"`
}

func registerOverviewRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	group.GET("/clusters/{clusterId}/overview", getOverview).Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("clusters.read", true))
}
func getOverview(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	id := e.Request.PathValue("clusterId")
	record, err := e.App.FindRecordById("clusters", id)
	if errors.Is(err, sql.ErrNoRows) {
		return e.NotFoundError("集群不存在。", nil)
	}
	if err != nil {
		return e.InternalServerError("", err)
	}
	if !record.GetBool("enabled") {
		return slurmFailure(e, client.Failure("disabled", "集群连接已停用，请联系管理员。"))
	}
	access, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return e.InternalServerError("", err)
	}
	result := clusterOverview{Cluster: rbac.ClusterDTO(record), Revision: record.GetInt("revision")}
	var group sync.WaitGroup
	group.Go(func() {
		result.Health = loadOverviewSection(e, id, "health", true, func(value models.Health) (models.Health, error) { return value, nil })
	})
	group.Go(func() {
		result.Jobs = loadOverviewSection(e, id, "jobs", access.Allows("jobs.read", id), func(values []models.Job) (overview.Jobs, error) { return overview.JobSummary(values), nil })
	})
	group.Go(func() {
		result.Nodes = loadOverviewSection(e, id, "nodes", access.Allows("nodes.read", id), overview.NodeSummary)
	})
	group.Go(func() {
		result.Partitions = loadOverviewSection(e, id, "partitions", access.Allows("partitions.read", id), func(values []models.Partition) (overview.Partitions, error) {
			return overview.PartitionSummary(values), nil
		})
	})
	group.Wait()
	if e.Request.Context().Err() != nil {
		return e.Error(408, "总览查询已取消。", nil)
	}
	current, err := e.App.FindRecordById("clusters", id)
	if err != nil {
		return e.InternalServerError("", err)
	}
	if !current.GetBool("enabled") || current.GetInt("revision") != result.Revision {
		return slurmFailure(e, client.Failure("configuration_changed", "连接配置已修改，请重新读取总览。"))
	}
	result.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return response(e, result)
}
func loadOverviewSection[T, U any](e *core.RequestEvent, id, kind string, allowed bool, summarize func(T) (U, error)) overviewSection[U] {
	result := overviewSection[U]{Status: "forbidden", Code: "FORBIDDEN", Message: "未授予该项读取权限。"}
	if !allowed {
		return result
	}
	source, err := services.SlurmFor(e.App).Read(e.Request.Context(), id, kind, "")
	var value T
	var summary U
	if err == nil {
		err = json.Unmarshal(source.Data, &value)
	}
	if err == nil {
		summary, err = summarize(value)
	}
	if err != nil {
		result.Status = "error"
		result.Code = "UNAVAILABLE"
		result.Message = "暂时无法读取此项数据，请稍后刷新。"
		var failure *client.Error
		if errors.As(err, &failure) {
			result.Code = "SLURM_" + strings.ToUpper(failure.Code)
			result.Message = failure.Message
		}
		slog.WarnContext(e.Request.Context(), "overview source unavailable", "clusterId", id, "source", kind, "code", result.Code)
		return result
	}
	return overviewSection[U]{Status: "ok", Item: &summary, FetchedAt: source.FetchedAt, Cached: source.Cached}
}
