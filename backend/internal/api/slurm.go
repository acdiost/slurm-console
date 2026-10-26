package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"sort"
	"strings"

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

func registerSlurmRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	routes := group.Group("/clusters/{clusterId}")
	routes.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	routes.GET("/health", func(e *core.RequestEvent) error { return slurmDetail[models.Health](e, "health", "") }).BindFunc(rbac.Require("clusters.read", true))
	routes.GET("/jobs", listJobs).BindFunc(rbac.Require("jobs.read", true))
	routes.GET("/jobs/{jobId}", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("jobId")
		if !client.ValidJobID(id) {
			return e.BadRequestError("作业 ID 必须为有效的正整数。", nil)
		}
		return slurmDetail[models.Job](e, "job", id)
	}).BindFunc(rbac.Require("jobs.read", true))
	routes.GET("/nodes", listNodes).BindFunc(rbac.Require("nodes.read", true))
	routes.GET("/nodes/{nodeName}", func(e *core.RequestEvent) error {
		name := e.Request.PathValue("nodeName")
		if !client.ValidNodeName(name) {
			return e.BadRequestError("节点名称不合法。", nil)
		}
		return getNode(e, name)
	}).BindFunc(rbac.Require("nodes.read", true))
}
func slurmFailure(e *core.RequestEvent, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return e.Error(408, "Slurm 查询已取消。", nil)
	}
	var failure *client.Error
	if !errors.As(err, &failure) {
		return e.InternalServerError("", err)
	}
	status := 503
	switch failure.Code {
	case "not_found":
		status = 404
	case "operation_forbidden":
		status = 403
	case "invalid_request":
		status = 400
	case "disabled", "configuration_changed", "data_changed", "confirmation_invalid", "confirmation_expired", "confirmation_changed":
		status = 409
	case "busy":
		status = 429
	}
	e.Set("publicErrorCode", "SLURM_"+strings.ToUpper(failure.Code))
	e.Set("publicErrorMessage", failure.Message)
	return e.Error(status, failure.Message, nil)
}
func slurmDetail[T any](e *core.RequestEvent, kind, id string) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	result, err := services.SlurmFor(e.App).Read(e.Request.Context(), e.Request.PathValue("clusterId"), kind, id)
	if err != nil {
		return slurmFailure(e, err)
	}
	var value T
	if err = json.Unmarshal(result.Data, &value); err != nil {
		return e.InternalServerError("", err)
	}
	return response(e, map[string]any{"item": value, "fetchedAt": result.FetchedAt, "cached": result.Cached})
}
func slurmQuery(e *core.RequestEvent, allowed ...string) error {
	for key, values := range e.Request.URL.Query() {
		if !slices.Contains(allowed, key) || len(values) != 1 || len(values[0]) > queryLimit(key) {
			return e.BadRequestError("查询参数不合法或不受支持。", nil)
		}
	}
	return nil
}
func pageResult[T any](e *core.RequestEvent, values []T, result services.SlurmResult, page, size int) error {
	total := len(values)
	start := min((page-1)*size, total)
	end := min(start+size, total)
	return response(e, map[string]any{"items": values[start:end], "page": page, "perPage": size, "totalItems": total, "fetchedAt": result.FetchedAt, "cached": result.Cached})
}
func fetchList[T any](e *core.RequestEvent, kind string) ([]T, services.SlurmResult, error) {
	result, err := services.SlurmFor(e.App).Read(e.Request.Context(), e.Request.PathValue("clusterId"), kind, "")
	if err != nil {
		return nil, result, slurmFailure(e, err)
	}
	var values []T
	if err = json.Unmarshal(result.Data, &values); err != nil {
		return nil, result, e.InternalServerError("", err)
	}
	return values, result, nil
}
func contains(query, value string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(query))
}
func matchesState(query url.Values, states []string) bool {
	state := strings.ToUpper(query.Get("state"))
	return state == "" || slices.Contains(states, state)
}
func listJobs(e *core.RequestEvent) error {
	if err := slurmQuery(e, "page", "perPage", "q", "user", "partition", "state", "account", "qos", "sort", "order"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	query := e.Request.URL.Query()
	sortKey, order := query.Get("sort"), query.Get("order")
	if sortKey == "" {
		sortKey = "id"
	}
	if order == "" {
		order = "desc"
	}
	if !slices.Contains([]string{"id", "submitTime", "cpus"}, sortKey) || !slices.Contains([]string{"asc", "desc"}, order) {
		return e.BadRequestError("排序参数不受支持。", nil)
	}
	values, result, err := fetchList[models.Job](e, "jobs")
	if err != nil {
		return err
	}
	filtered := make([]models.Job, 0, len(values))
	for _, job := range values {
		if (contains(query.Get("q"), job.Name) || contains(query.Get("q"), job.ID)) && (query.Get("user") == "" || query.Get("user") == job.User) && (query.Get("partition") == "" || query.Get("partition") == job.Partition) && matchesState(query, job.States) && (query.Get("account") == "" || query.Get("account") == job.Account) && (query.Get("qos") == "" || query.Get("qos") == job.QOS) {
			filtered = append(filtered, job)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if sortKey != "id" {
			var av, bv *int64
			if sortKey == "cpus" {
				av, bv = a.CPUs.Value, b.CPUs.Value
			} else {
				av, bv = a.SubmitTime.Value, b.SubmitTime.Value
			}
			if av == nil || bv == nil {
				if av == nil && bv != nil {
					return false
				}
				if av != nil && bv == nil {
					return true
				}
			} else if *av != *bv {
				if order == "asc" {
					return *av < *bv
				}
				return *av > *bv
			}
		}
		if len(a.ID) != len(b.ID) {
			if order == "asc" && sortKey == "id" {
				return len(a.ID) < len(b.ID)
			}
			return len(a.ID) > len(b.ID)
		}
		if order == "asc" && sortKey == "id" {
			return a.ID < b.ID
		}
		return a.ID > b.ID
	})
	return pageResult(e, filtered, result, page, size)
}
func listNodes(e *core.RequestEvent) error {
	if err := slurmQuery(e, "page", "perPage", "q", "partition", "state", "sort", "order"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	query := e.Request.URL.Query()
	sortKey, order := query.Get("sort"), query.Get("order")
	if sortKey == "" {
		sortKey = "name"
	}
	if order == "" {
		order = "asc"
	}
	if !slices.Contains([]string{"name", "cpus", "memory"}, sortKey) || !slices.Contains([]string{"asc", "desc"}, order) {
		return e.BadRequestError("排序参数不受支持。", nil)
	}
	values, result, err := fetchList[models.Node](e, "nodes")
	if err != nil {
		return err
	}
	filtered := make([]models.Node, 0, len(values))
	for _, node := range values {
		if (contains(query.Get("q"), node.Name) || contains(query.Get("q"), node.Hostname)) && (query.Get("partition") == "" || slices.Contains(node.Partitions, query.Get("partition"))) && matchesState(query, node.States) {
			filtered = append(filtered, node)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		av, bv := a.CPUs, b.CPUs
		if sortKey == "memory" {
			av, bv = a.MemoryMiB, b.MemoryMiB
		}
		if sortKey != "name" && av != bv {
			if order == "asc" {
				return av < bv
			}
			return av > bv
		}
		if sortKey == "name" && order == "desc" {
			return a.Name > b.Name
		}
		return a.Name < b.Name
	})
	views := make([]nodeView, 0, len(filtered))
	for _, n := range filtered {
		view, err := viewNode(n)
		if err != nil {
			return slurmFailure(e, client.Failure("invalid_response", "节点资源数据不一致。"))
		}
		views = append(views, view)
	}
	return pageResult(e, views, result, page, size)
}

type nodeResources struct {
	CPUs      overview.Resource `json:"cpus"`
	MemoryMiB overview.Resource `json:"memoryMiB"`
	GPUs      overview.GPU      `json:"gpus"`
}
type nodeView struct {
	models.Node
	Resources nodeResources `json:"resources"`
}

func viewNode(n models.Node) (nodeView, error) {
	summary, err := overview.NodeSummary([]models.Node{n})
	return nodeView{n, nodeResources{summary.CPUs, summary.MemoryMiB, summary.GPUs}}, err
}
func getNode(e *core.RequestEvent, name string) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	result, err := services.SlurmFor(e.App).Read(e.Request.Context(), e.Request.PathValue("clusterId"), "node", name)
	if err != nil {
		return slurmFailure(e, err)
	}
	var n models.Node
	if err := json.Unmarshal(result.Data, &n); err != nil {
		return e.InternalServerError("", err)
	}
	view, err := viewNode(n)
	if err != nil {
		return slurmFailure(e, client.Failure("invalid_response", "节点资源数据不一致。"))
	}
	return response(e, map[string]any{"item": view, "fetchedAt": result.FetchedAt, "cached": result.Cached})
}

func queryLimit(key string) int {
	if key == "partition" {
		return 255
	}
	return 128
}
