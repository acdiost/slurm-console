package api

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/overview"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/slurm/models"
)

type fleetItem struct {
	Cluster       rbac.Cluster                 `json:"cluster"`
	Revision      int                          `json:"revision"`
	Configuration string                       `json:"configuration"`
	State         string                       `json:"state"`
	Permissions   []string                     `json:"permissions"`
	Health        slurmSection[models.Health]  `json:"health"`
	Nodes         slurmSection[overview.Nodes] `json:"nodes"`
	Jobs          slurmSection[overview.Jobs]  `json:"jobs"`
}

func registerFleetRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	group.GET("/cluster-fleet", listFleet).Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
}

// One authorization/filter builder is shared by all cluster selectors and fleet
// pagination. Search is literal and scoped before counts or upstream reads.
func clusterFilter(access rbac.Access, query, configuration string) dbx.Expression {
	filters := []dbx.Expression{dbx.NewExp("1=1")}
	if !access.Allows("clusters.read", "") {
		ids := []any{}
		for _, entry := range access.Clusters {
			if access.Allows("clusters.read", entry.Cluster.ID) {
				ids = append(ids, entry.Cluster.ID)
			}
		}
		if len(ids) == 0 {
			filters = append(filters, dbx.NewExp("0=1"))
		} else {
			filters = append(filters, dbx.In("id", ids...))
		}
	}
	if term := strings.TrimSpace(query); term != "" {
		filters = append(filters, dbx.NewExp("(instr(lower(name),lower({:term}))>0 OR instr(lower(slug),lower({:term}))>0 OR instr(lower(description),lower({:term}))>0)", dbx.Params{"term": term}))
	}
	configured := dbx.NewExp("connection_ref != '' AND api_version != ''")
	switch configuration {
	case "enabled":
		filters = append(filters, configured, dbx.HashExp{"enabled": true})
	case "disabled":
		filters = append(filters, configured, dbx.HashExp{"enabled": false})
	case "unconfigured":
		filters = append(filters, dbx.NewExp("connection_ref = '' OR api_version = ''"))
	}
	return dbx.And(filters...)
}
func clusterQuery(e *core.RequestEvent, allowed ...string) error {
	for key, values := range e.Request.URL.Query() {
		limit := 32
		if key == "q" {
			limit = 512
		}
		if !slices.Contains(allowed, key) || len(values) != 1 || len(values[0]) > limit || strings.ContainsFunc(values[0], unicode.IsControl) || key == "q" && utf8.RuneCountInString(values[0]) > 128 {
			return e.BadRequestError("集群查询参数不合法或不受支持。", nil)
		}
	}
	return nil
}
func permissionFingerprint(access rbac.Access) string {
	type scope struct {
		ID      string
		Actions []string
	}
	value := struct {
		Platform []string
		Clusters []scope
	}{Platform: access.PlatformPermissions, Clusters: []scope{}}
	for _, entry := range access.Clusters {
		value.Clusters = append(value.Clusters, scope{entry.Cluster.ID, entry.Permissions})
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}
func unavailableSource[T any](allowed bool, code, message string) slurmSection[T] {
	if !allowed {
		return slurmSection[T]{Status: "forbidden", Code: "FORBIDDEN", Message: "未授予该项读取权限。"}
	}
	return slurmSection[T]{Status: "error", Code: code, Message: message}
}
func initialFleetItem(record *core.Record, access rbac.Access) fleetItem {
	id := record.Id
	configuration := "enabled"
	state, code, message := "pending", "NOT_QUERIED", "本次尚未取得该集群快照，请缩小范围或稍后刷新。"
	if record.GetString("connection_ref") == "" || record.GetString("api_version") == "" {
		configuration = "unconfigured"
		state = "unconfigured"
		code = "NOT_CONFIGURED"
		message = "尚未配置集群连接。"
	} else if !record.GetBool("enabled") {
		configuration = "disabled"
		state = "disabled"
		code = "DISABLED"
		message = "集群连接已停用。"
	}
	permissions := []string{}
	for _, action := range []string{"clusters.read", "nodes.read", "jobs.read", "partitions.read"} {
		if access.Allows(action, id) {
			permissions = append(permissions, action)
		}
	}
	return fleetItem{Cluster: rbac.ClusterDTO(record), Revision: record.GetInt("revision"), Configuration: configuration, State: state, Permissions: permissions, Health: unavailableSource[models.Health](true, code, message), Nodes: unavailableSource[overview.Nodes](access.Allows("nodes.read", id), code, message), Jobs: unavailableSource[overview.Jobs](access.Allows("jobs.read", id), code, message)}
}
func loadFleetItem(ctx context.Context, app core.App, record *core.Record, access rbac.Access) fleetItem {
	item := initialFleetItem(record, access)
	if item.Configuration != "enabled" {
		return item
	}
	id := record.Id
	item.Health = loadSlurmSectionContext(ctx, app, id, "health", true, func(v models.Health) (models.Health, error) { return v, nil })
	if item.Health.Status != "ok" {
		item.State = "unavailable"
		if item.Health.Code == "SLURM_BUSY" {
			item.State = "busy"
		}
		item.Nodes = unavailableSource[overview.Nodes](access.Allows("nodes.read", id), "NOT_QUERIED", "本次健康检查未完成，未读取节点资源。")
		item.Jobs = unavailableSource[overview.Jobs](access.Allows("jobs.read", id), "NOT_QUERIED", "本次健康检查未完成，未读取作业。")
		return item
	}
	item.State = item.Health.Item.Status
	// Per-cluster sources run sequentially, so four fleet workers never exceed
	// the shared adapter's four upstream slots by themselves.
	item.Nodes = loadSlurmSectionContext(ctx, app, id, "nodes", access.Allows("nodes.read", id), overview.NodeSummary)
	item.Jobs = loadSlurmSectionContext(ctx, app, id, "jobs", access.Allows("jobs.read", id), func(values []models.Job) (overview.Jobs, error) { return overview.JobSummary(values), nil })
	return item
}
func collectFleet(ctx context.Context, items []fleetItem, load func(int) fleetItem) {
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(4, len(items)) {
		workers.Go(func() {
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				value := load(index)
				if ctx.Err() == nil {
					items[index] = value
				}
			}
		})
	}
feed:
	for index := range items {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- index:
		}
	}
	close(jobs)
	workers.Wait()
}
func listFleet(e *core.RequestEvent) error {
	if err := clusterQuery(e, "page", "perPage", "q", "configuration", "sort", "order"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	q := e.Request.URL.Query()
	if q.Get("perPage") == "" {
		size = 12
	}
	if size > 24 {
		return e.BadRequestError("工作台每页最多 24 个集群。", nil)
	}
	configuration, key, order := q.Get("configuration"), q.Get("sort"), q.Get("order")
	if key == "" {
		key = "name"
	}
	if order == "" {
		order = "asc"
	}
	if !slices.Contains([]string{"", "enabled", "disabled", "unconfigured"}, configuration) || !slices.Contains([]string{"name", "slug"}, key) || !slices.Contains([]string{"asc", "desc"}, order) {
		return e.BadRequestError("集群筛选或排序参数不受支持。", nil)
	}
	access, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return e.InternalServerError("", err)
	}
	filter := clusterFilter(access, q.Get("q"), configuration)
	var records []*core.Record
	var total int64
	err = e.App.RunInTransaction(func(tx core.App) error {
		var err error
		total, err = tx.CountRecords("clusters", filter)
		if err != nil {
			return err
		}
		return tx.RecordQuery("clusters").AndWhere(filter).OrderBy(key+" "+strings.ToUpper(order), "id").Limit(int64(size)).Offset(int64((page - 1) * size)).All(&records)
	})
	if err != nil {
		return e.InternalServerError("", err)
	}
	items := make([]fleetItem, len(records))
	for i, r := range records {
		items[i] = initialFleetItem(r, access)
	}
	ctx, cancel := context.WithTimeout(e.Request.Context(), 15*time.Second)
	defer cancel()
	collectFleet(ctx, items, func(i int) fleetItem { return loadFleetItem(ctx, e.App, records[i], access) })
	if e.Request.Context().Err() != nil {
		return e.Error(408, "工作台查询已取消。", nil)
	}
	currentAccess, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return e.InternalServerError("", err)
	}
	if permissionFingerprint(access) != permissionFingerprint(currentAccess) {
		return e.Error(409, "授权范围已变化，请重新读取工作台。", nil)
	}
	for i, item := range items {
		current, err := e.App.FindRecordById("clusters", item.Cluster.ID)
		if err != nil || current.GetInt("revision") != item.Revision || rbac.ClusterDTO(current) != item.Cluster || initialFleetItem(current, currentAccess).Configuration != item.Configuration {
			replacement := initialFleetItem(records[i], currentAccess)
			replacement.State = "changed"
			replacement.Health = unavailableSource[models.Health](true, "CONFIGURATION_CHANGED", "集群配置已变化，请刷新后读取。")
			replacement.Nodes = unavailableSource[overview.Nodes](access.Allows("nodes.read", item.Cluster.ID), "CONFIGURATION_CHANGED", "集群配置已变化，请刷新后读取。")
			replacement.Jobs = unavailableSource[overview.Jobs](access.Allows("jobs.read", item.Cluster.ID), "CONFIGURATION_CHANGED", "集群配置已变化，请刷新后读取。")
			items[i] = replacement
		}
	}
	summary := map[string]int{"connected": 0, "controllerDown": 0, "unavailable": 0, "disabled": 0, "unconfigured": 0, "pending": 0, "partial": 0, "needsAttention": 0}
	for _, item := range items {
		if !slices.Contains([]string{"connected", "disabled", "unconfigured"}, item.State) || item.Health.Status == "ok" && (item.Nodes.Status == "error" || item.Jobs.Status == "error") {
			summary["needsAttention"]++
		}
		switch item.State {
		case "connected":
			summary["connected"]++
		case "controller_down":
			summary["controllerDown"]++
		case "disabled":
			summary["disabled"]++
		case "unconfigured":
			summary["unconfigured"]++
		case "pending":
			summary["pending"]++
		default:
			summary["unavailable"]++
		}
		if item.Health.Status == "ok" && (item.Nodes.Status == "error" || item.Jobs.Status == "error") {
			summary["partial"]++
		}
	}
	return response(e, map[string]any{"items": items, "page": page, "perPage": size, "totalItems": total, "generatedAt": time.Now().UTC().Format(time.RFC3339Nano), "pageSummary": summary, "canManage": currentAccess.Allows("platform.clusters.manage", "")})
}
