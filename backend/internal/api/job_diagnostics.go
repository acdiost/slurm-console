package api

import (
	"encoding/hex"
	"encoding/json"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/diagnostics"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strconv"
)

func registerJobDiagnosticRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	r.GET("/jobs/{jobId}/diagnosis", diagnoseCurrent).BindFunc(rbac.Require("jobs.read", true))
	r.GET("/job-history/{recordKey}/diagnosis", diagnoseHistory).BindFunc(rbac.Require("job_history.read", true))
}
func diagnosticRevision(e *core.RequestEvent) (int, error) {
	r, err := e.App.FindRecordById("clusters", e.Request.PathValue("clusterId"))
	if err != nil {
		return 0, e.NotFoundError("集群不存在。", nil)
	}
	if !r.GetBool("enabled") {
		return 0, slurmFailure(e, client.Failure("disabled", "集群连接已停用。"))
	}
	return r.GetInt("revision"), nil
}
func diagnosticFresh(e *core.RequestEvent, permission string, revision int) error {
	a, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return err
	}
	if !a.Allows(permission, e.Request.PathValue("clusterId")) {
		return e.ForbiddenError("作业来源读取权限已撤销或账户已停用。", nil)
	}
	r, err := e.App.FindRecordById("clusters", e.Request.PathValue("clusterId"))
	if err != nil || !r.GetBool("enabled") || r.GetInt("revision") != revision {
		return slurmFailure(e, client.Failure("configuration_changed", "集群连接变化，请重新生成诊断。"))
	}
	return nil
}
func diagnoseCurrent(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	id := e.Request.PathValue("jobId")
	if !client.ValidJobID(id) {
		return e.BadRequestError("作业 ID 不合法。", nil)
	}
	revision, err := diagnosticRevision(e)
	if err != nil {
		return err
	}
	v, err := services.SlurmFor(e.App).Read(e.Request.Context(), e.Request.PathValue("clusterId"), "job", id)
	if err != nil {
		return slurmFailure(e, err)
	}
	if err := diagnosticFresh(e, "jobs.read", revision); err != nil {
		return err
	}
	var j models.Job
	if json.Unmarshal(v.Data, &j) != nil || j.ID != id {
		return e.Error(503, "作业来源不完整。", nil)
	}
	report := diagnostics.Current(j, v.FetchedAt)
	if err := diagnosticFresh(e, "jobs.read", revision); err != nil {
		return err
	}
	return response(e, map[string]any{"report": report, "revision": revision, "cached": v.Cached})
}
func diagnoseHistory(e *core.RequestEvent) error {
	if err := slurmQuery(e, "start", "end", "jobId"); err != nil {
		return err
	}
	raw, err := hex.DecodeString(e.Request.PathValue("recordKey"))
	if err != nil || len(raw) != 32 {
		return e.BadRequestError("历史记录标识不合法。", nil)
	}
	values := e.Request.URL.Query()
	start, e1 := strconv.ParseInt(values.Get("start"), 10, 64)
	end, e2 := strconv.ParseInt(values.Get("end"), 10, 64)
	id := values.Get("jobId")
	q := client.HistoryQuery{Cluster: "validated", Start: start, End: end, JobID: id}
	if e1 != nil || e2 != nil || !client.ValidJobID(id) || !q.Valid() {
		return e.BadRequestError("历史诊断必须指定单个 Job ID 和最多 31 天的提交时间范围。", nil)
	}
	revision, err := diagnosticRevision(e)
	if err != nil {
		return err
	}
	q.Cluster = ""
	v, err := services.SlurmFor(e.App).Read(e.Request.Context(), e.Request.PathValue("clusterId"), "job_history", q.Key())
	if err != nil {
		return slurmFailure(e, err)
	}
	if err := diagnosticFresh(e, "job_history.read", revision); err != nil {
		return err
	}
	var snapshot models.HistorySnapshot
	if json.Unmarshal(v.Data, &snapshot) != nil {
		return e.Error(503, "历史来源不完整。", nil)
	}
	for _, j := range snapshot.Items {
		if j.Key == e.Request.PathValue("recordKey") && j.ID == id {
			report := diagnostics.Accounting(j, v.FetchedAt)
			if err := diagnosticFresh(e, "job_history.read", revision); err != nil {
				return err
			}
			return response(e, map[string]any{"report": report, "revision": revision, "cached": v.Cached, "start": snapshot.Start, "end": snapshot.End})
		}
	}
	return e.NotFoundError("所选记录不在此 Job ID 和提交窗口内，无法生成诊断。", nil)
}
