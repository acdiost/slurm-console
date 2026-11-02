package api

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"slurm-console/backend/internal/usagereport"
)

func registerUsageReportRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}/reports/usage")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("reports.read", true)).BindFunc(rbac.Require("job_history.read", true))
	r.GET("", func(e *core.RequestEvent) error {
		if err := slurmQuery(e, "start", "end", "user", "account", "partition", "groupBy"); err != nil {
			return err
		}
		v := e.Request.URL.Query()
		start, e1 := strconv.ParseInt(v.Get("start"), 10, 64)
		end, e2 := strconv.ParseInt(v.Get("end"), 10, 64)
		q := client.HistoryQuery{Cluster: "validated", Start: start, End: end, User: v.Get("user"), Account: v.Get("account"), Partition: v.Get("partition"), ExecutionWindow: true}
		by := v.Get("groupBy")
		if by == "" {
			by = "user"
		}
		if e1 != nil || e2 != nil || !q.Valid() || end > time.Now().Unix() || (by != "user" && by != "account" && by != "partition") {
			return e.BadRequestError("请选择截至当前、最多 31 天的运行时间窗口、单个精确筛选值及用户/Account/分区维度。", nil)
		}
		revision, err := diagnosticRevision(e)
		if err != nil {
			return err
		}
		q.Cluster = ""
		source, err := services.SlurmFor(e.App).Read(e.Request.Context(), e.Request.PathValue("clusterId"), "job_history", q.Key())
		if err != nil {
			return slurmFailure(e, err)
		}
		fresh := func() error {
			if err := diagnosticFresh(e, "reports.read", revision); err != nil {
				return err
			}
			return diagnosticFresh(e, "job_history.read", revision)
		}
		if err = fresh(); err != nil {
			return err
		}
		var snapshot models.HistorySnapshot
		if json.Unmarshal(source.Data, &snapshot) != nil || snapshot.Start != start || snapshot.End != end {
			return e.Error(503, "报表记账来源不完整。", nil)
		}
		observed, err := time.Parse(time.RFC3339Nano, source.FetchedAt)
		if err != nil {
			return e.Error(503, "报表来源时间不可用。", nil)
		}
		report, err := usagereport.Analyze(snapshot, by, observed.Unix())
		if err != nil {
			return e.Error(503, "报表来源超出范围或身份不明确，请核对记账数据。", nil)
		}
		if err = fresh(); err != nil {
			return err
		}
		return response(e, map[string]any{"report": report, "fetchedAt": source.FetchedAt, "cached": source.Cached, "revision": revision})
	})
}
