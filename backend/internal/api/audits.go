package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
)

type auditItem struct {
	OperationID     string         `json:"operationId"`
	ActorID         string         `json:"actorId"`
	ActorLabel      string         `json:"actorLabel"`
	ActorSource     string         `json:"actorSource"`
	ClusterID       string         `json:"clusterId"`
	ClusterName     string         `json:"clusterName"`
	ClusterSlug     string         `json:"clusterSlug"`
	ClusterRevision int            `json:"clusterRevision"`
	Action          string         `json:"action"`
	TargetType      string         `json:"targetType"`
	TargetID        string         `json:"targetId"`
	RequestID       string         `json:"requestId"`
	Status          string         `json:"status"`
	RecordedStatus  string         `json:"recordedStatus"`
	Message         string         `json:"message"`
	Reason          string         `json:"reason"`
	States          []string       `json:"states"`
	Changes         []audit.Change `json:"changes"`
	CreatedAt       string         `json:"createdAt"`
	UpdatedAt       string         `json:"updatedAt"`
}

func auditView(r *core.Record) auditItem {
	v := auditItem{OperationID: r.GetString("operation_key"), ActorID: r.GetString("actor_id"), ActorLabel: r.GetString("actor_label"), ActorSource: r.GetString("actor_source"), ClusterID: r.GetString("cluster_id"), ClusterName: r.GetString("cluster_name"), ClusterSlug: r.GetString("cluster_slug"), ClusterRevision: r.GetInt("cluster_revision"), Action: r.GetString("action"), TargetType: r.GetString("target_type"), TargetID: r.GetString("target_id"), RequestID: r.GetString("request_id"), Status: r.GetString("status"), RecordedStatus: r.GetString("status"), Message: r.GetString("message"), Reason: r.GetString("reason"), States: []string{}, Changes: []audit.Change{}, CreatedAt: r.GetString("created"), UpdatedAt: r.GetString("updated")}
	if v.TargetType == "" {
		if v.Action == "jobs.cancel" {
			v.TargetType = "job"
			v.TargetID = r.GetString("job_id")
		} else if strings.HasPrefix(v.Action, "nodes.") {
			v.TargetType = "node"
			v.TargetID = r.GetString("node_name")
		}
	}
	if v.ActorSource == "" {
		v.ActorSource = "legacy"
	}
	if v.Status == "processing" {
		v.Status = "unknown"
		v.Message = "记录尚未完成，可能仍在处理或执行已中断；请核对目标状态，勿重复提交。"
	}
	data, _ := json.Marshal(r.GetRaw("observed_states"))
	_ = json.Unmarshal(data, &v.States)
	if v.States == nil {
		v.States = []string{}
	}
	var changes []audit.Change
	data, _ = json.Marshal(r.GetRaw("changes"))
	_ = json.Unmarshal(data, &changes)
	// Expose only reviewed scalar change fields, even for malformed local records.
	for _, c := range changes {
		if slices.Contains([]string{"name", "slug", "enabled", "apiVersion", "connectionRef", "timeoutSeconds", "revision", "authSource", "username", "uid", "scope", "role", "home", "shell", "accountExpiry", "account", "slurmCluster", "partition", "associationId", "description", "organization"}, c.Field) {
			v.Changes = append(v.Changes, c)
		}
	}
	return v
}
func registerAuditRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	routes := group.Group("/operation-audits")
	routes.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("platform.audit.read", false))
	routes.GET("", listAudits)
	routes.GET("/{operationId}", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		id := e.Request.PathValue("operationId")
		if len(id) < 1 || len(id) > 64 || strings.ContainsFunc(id, unicode.IsControl) {
			return e.BadRequestError("操作编号不合法。", nil)
		}
		row, err := e.App.FindFirstRecordByData("operation_audits", "operation_key", id)
		if errors.Is(err, sql.ErrNoRows) {
			return e.NotFoundError("审计记录不存在。", nil)
		}
		if err != nil {
			return e.InternalServerError("", err)
		}
		return response(e, map[string]any{"item": auditView(row), "fetchedAt": time.Now().UTC().Format(time.RFC3339Nano)})
	})
}
func listAudits(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	limits := map[string]int{"page": 10, "perPage": 3, "q": 255, "cluster": 32, "actor": 32, "action": 64, "status": 32, "from": 40, "to": 40, "sort": 16, "order": 4}
	for k, v := range q {
		limit, ok := limits[k]
		if !ok || len(v) != 1 || len(v[0]) > limit || strings.ContainsFunc(v[0], unicode.IsControl) {
			return e.BadRequestError("审计查询参数不合法或不受支持。", nil)
		}
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	key, order := q.Get("sort"), q.Get("order")
	if key == "" {
		key = "created"
	}
	if order == "" {
		order = "desc"
	}
	if !slices.Contains([]string{"created", "updated"}, key) || !slices.Contains([]string{"asc", "desc"}, order) {
		return e.BadRequestError("审计排序参数不受支持。", nil)
	}
	filters := []dbx.Expression{dbx.NewExp("1=1")}
	for param, field := range map[string]string{"cluster": "cluster_id", "actor": "actor_id", "action": "action"} {
		if value := q.Get(param); value != "" {
			filters = append(filters, dbx.HashExp{field: value})
		}
	}
	if status := q.Get("status"); status != "" {
		if !slices.Contains([]string{"succeeded", "confirmed", "accepted", "failed", "unknown", "processing"}, status) {
			return e.BadRequestError("审计结果筛选不受支持。", nil)
		}
		if status == "unknown" {
			filters = append(filters, dbx.In("status", "unknown", "processing"))
		} else {
			filters = append(filters, dbx.HashExp{"status": status})
		}
	}
	var from, to time.Time
	for param, target := range map[string]*time.Time{"from": &from, "to": &to} {
		if raw := q.Get(param); raw != "" {
			parsed, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil || parsed.Nanosecond()%int(time.Millisecond) != 0 {
				return e.BadRequestError("时间须为包含时区的 RFC3339 格式，精度最多为毫秒。", nil)
			}
			*target = parsed
		}
	}
	if q.Get("from") != "" && q.Get("to") != "" && !from.Before(to) {
		return e.BadRequestError("开始时间须早于结束时间。", nil)
	}
	// PB records use canonical UTC millisecond timestamps; compare the indexed
	// text columns directly with boundaries normalized to the same format.
	if q.Get("from") != "" {
		filters = append(filters, dbx.NewExp("created >= {:from}", dbx.Params{"from": from.UTC().Format("2006-01-02 15:04:05.000Z")}))
	}
	if q.Get("to") != "" {
		filters = append(filters, dbx.NewExp("created < {:to}", dbx.Params{"to": to.UTC().Format("2006-01-02 15:04:05.000Z")}))
	}
	if text := strings.TrimSpace(q.Get("q")); text != "" {
		fields := []string{"operation_key", "request_id", "actor_id", "actor_label", "cluster_id", "cluster_name", "cluster_slug", "target_id", "job_id", "node_name", "reason"}
		matches := []dbx.Expression{}
		for _, f := range fields {
			matches = append(matches, dbx.NewExp("instr(lower([["+f+"]]),lower({:term})) > 0", dbx.Params{"term": text}))
		}
		filters = append(filters, dbx.Or(matches...))
	}
	filter := dbx.And(filters...)
	items := []auditItem{}
	var total int64
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()
	err = e.App.RunInTransaction(func(tx core.App) error {
		var err error
		err = tx.DB().Select("COUNT(*)").From("operation_audits").Where(filter).WithContext(ctx).Row(&total)
		if err != nil {
			return err
		}
		var rows []*core.Record
		sortKey := key + " " + strings.ToUpper(order)
		err = tx.RecordQuery("operation_audits").AndWhere(filter).WithContext(ctx).OrderBy(sortKey, "id "+strings.ToUpper(order)).Limit(int64(size)).Offset(int64((page - 1) * size)).All(&rows)
		if err != nil {
			return err
		}
		for _, r := range rows {
			items = append(items, auditView(r))
		}
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return e.Error(408, "审计查询超时或已取消，请缩小时间范围。", nil)
		}
		return e.InternalServerError("", err)
	}
	return response(e, map[string]any{"items": items, "page": page, "perPage": size, "totalItems": total, "fetchedAt": time.Now().UTC().Format(time.RFC3339Nano)})
}
