package api

import (
	"encoding/hex"
	"encoding/json"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slices"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"sort"
	"strconv"
	"time"
)

func registerHistoryRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}/job-history")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("job_history.read", true))
	r.GET("", jobHistory)
	r.GET("/{recordKey}", jobHistory)
}
func jobHistory(e *core.RequestEvent) error {
	if err := slurmQuery(e, "start", "end", "user", "account", "partition", "qos", "state", "jobId", "q", "page", "perPage", "sort", "order"); err != nil {
		return err
	}
	values := e.Request.URL.Query()
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	recordKey := e.Request.PathValue("recordKey")
	if recordKey != "" {
		raw, err := hex.DecodeString(recordKey)
		if err != nil || len(raw) != 32 {
			return e.BadRequestError("历史记录标识不合法。", nil)
		}
	}
	start, end := time.Now().Add(-24*time.Hour).Unix(), time.Now().Unix()
	if values.Has("start") != values.Has("end") {
		return e.BadRequestError("请同时提供提交时间起止。", nil)
	}
	if values.Has("start") {
		var err error
		start, err = strconv.ParseInt(values.Get("start"), 10, 64)
		if err != nil {
			return e.BadRequestError("提交时间不合法。", nil)
		}
		end, err = strconv.ParseInt(values.Get("end"), 10, 64)
		if err != nil {
			return e.BadRequestError("提交时间不合法。", nil)
		}
	}
	q := client.HistoryQuery{Cluster: "validated", Start: start, End: end, User: values.Get("user"), Account: values.Get("account"), Partition: values.Get("partition"), QOS: values.Get("qos"), State: values.Get("state"), JobID: values.Get("jobId")}
	if !q.Valid() {
		return e.BadRequestError("历史查询须使用最多 31 天的提交时间范围与单个精确筛选值。", nil)
	}
	q.Cluster = "" // Resolved from the connected controller, never the browser.
	sortKey, order := values.Get("sort"), values.Get("order")
	if sortKey == "" {
		sortKey = "submitTime"
	}
	if order == "" {
		order = "desc"
	}
	if !slices.Contains([]string{"submitTime", "startTime", "endTime", "elapsedSeconds", "id"}, sortKey) || !slices.Contains([]string{"asc", "desc"}, order) {
		return e.BadRequestError("历史排序不合法。", nil)
	}
	cid := e.Request.PathValue("clusterId")
	record, err := e.App.FindRecordById("clusters", cid)
	if err != nil {
		return e.NotFoundError("集群不存在。", nil)
	}
	revision := record.GetInt("revision")
	result, err := services.SlurmFor(e.App).Read(e.Request.Context(), cid, "job_history", q.Key())
	if err != nil {
		return slurmFailure(e, err)
	}
	// Re-check authorization even after a cache hit or an in-flight DB query.
	access, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return err
	}
	if !access.Allows("job_history.read", cid) {
		return e.ForbiddenError("历史作业读取权限已撤销。", nil)
	}
	current, err := e.App.FindRecordById("clusters", cid)
	if err != nil || !current.GetBool("enabled") || current.GetInt("revision") != revision {
		return slurmFailure(e, client.Failure("configuration_changed", "集群连接变化，请重新查询。"))
	}
	person, err := e.App.FindRecordById("users", e.Auth.Id)
	if err != nil || !person.GetBool("active") {
		return e.ForbiddenError("平台账户已停用。", nil)
	}
	var snapshot models.HistorySnapshot
	if json.Unmarshal(result.Data, &snapshot) != nil {
		return e.InternalServerError("", nil)
	}
	items := []models.HistoryJob{}
	for _, j := range snapshot.Items {
		if recordKey != "" && j.Key == recordKey {
			return response(e, map[string]any{"item": j, "fetchedAt": result.FetchedAt, "cached": result.Cached, "start": snapshot.Start, "end": snapshot.End})
		}
		if contains(values.Get("q"), j.ID+" "+j.Name+" "+j.User+" "+j.Account+" "+j.Nodes) {
			items = append(items, j)
		}
	}
	if recordKey != "" {
		return e.NotFoundError("记录不在当前提交时间范围内，或已不对连接身份可见。", nil)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		var x, y *int64
		switch sortKey {
		case "submitTime":
			x, y = a.SubmitTime.Value, b.SubmitTime.Value
		case "startTime":
			x, y = a.StartTime.Value, b.StartTime.Value
		case "endTime":
			x, y = a.EndTime.Value, b.EndTime.Value
		case "elapsedSeconds":
			x, y = a.ElapsedSeconds.Value, b.ElapsedSeconds.Value
		case "id":
			u, _ := strconv.ParseInt(a.ID, 10, 64)
			v, _ := strconv.ParseInt(b.ID, 10, 64)
			x, y = &u, &v
		}
		if x == nil && y != nil {
			return false
		}
		if x != nil && y == nil {
			return true
		}
		if x != nil && y != nil && *x != *y {
			if order == "asc" {
				return *x < *y
			}
			return *x > *y
		}
		return a.Key < b.Key
	})
	total := len(items)
	offset := min((page-1)*size, total)
	// Steps are detail-only and must not be counted again as job rows.
	rows := append([]models.HistoryJob{}, items[offset:min(offset+size, total)]...)
	for i := range rows {
		rows[i].Steps = []models.HistoryStep{}
	}
	return response(e, map[string]any{"items": rows, "page": page, "perPage": size, "totalItems": total, "cluster": snapshot.Cluster, "start": snapshot.Start, "end": snapshot.End, "fetchedAt": result.FetchedAt, "cached": result.Cached})
}
