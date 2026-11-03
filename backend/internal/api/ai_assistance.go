package api

import (
	"crypto/rand"
	"errors"
	"regexp"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/aiassist"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
)

func registerAIRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("ai.explain", true))
	r.GET("/ai-assistance", aiStatus)
	r.POST("/jobs/{jobId}/diagnosis/ai-preview", func(e *core.RequestEvent) error { return aiPreview(e, "controller") }).BindFunc(rbac.Require("jobs.read", true))
	r.POST("/job-history/{recordKey}/diagnosis/ai-preview", func(e *core.RequestEvent) error { return aiPreview(e, "accounting") }).BindFunc(rbac.Require("job_history.read", true))
	r.POST("/ai-assistance/{previewId}/explain", aiExplain)
}
func aiPermission(source string) string {
	if source == "controller" {
		return "jobs.read"
	}
	return "job_history.read"
}
func aiFresh(e *core.RequestEvent, source string, revision int) error {
	if err := diagnosticFresh(e, "ai.explain", revision); err != nil {
		return err
	}
	return diagnosticFresh(e, aiPermission(source), revision)
}
func aiStatus(e *core.RequestEvent) error {
	if err := slurmQuery(e, "source"); err != nil {
		return err
	}
	source := e.Request.URL.Query().Get("source")
	if source != "controller" && source != "accounting" {
		return e.BadRequestError("须指定诊断来源。", nil)
	}
	revision, err := diagnosticRevision(e)
	if err != nil {
		return err
	}
	if err = aiFresh(e, source, revision); err != nil {
		return err
	}
	s := aiassist.For(e.App)
	if !s.Enabled(e.Request.PathValue("clusterId")) {
		return response(e, map[string]any{"enabled": false})
	}
	return response(e, map[string]any{"enabled": true, "destination": s.Destination()})
}
func aiError(e *core.RequestEvent, err error) error {
	if errors.Is(err, aiassist.ErrDisabled) {
		return e.Error(409, "此集群尚未启用 AI 服务。", nil)
	}
	if errors.Is(err, aiassist.ErrBusy) {
		e.Response.Header().Set("Retry-After", "30")
		return e.Error(429, "AI 请求预算已用完，请等待后重新预览。", nil)
	}
	return e.Error(409, "预览已过期、已使用或与当前上下文不符，请重新预览。", nil)
}
func aiPreview(e *core.RequestEvent, source string) error {
	var body struct{}
	if err := readConfigurationBody(e, &body); err != nil {
		return err
	}
	s := aiassist.For(e.App)
	cid := e.Request.PathValue("clusterId")
	if !s.Enabled(cid) {
		return aiError(e, aiassist.ErrDisabled)
	}
	var v diagnosticSnapshot
	var err error
	target := e.Request.PathValue("jobId")
	if source == "controller" {
		v, err = loadCurrentDiagnosis(e)
	} else {
		v, err = loadHistoryDiagnosis(e)
		target = e.Request.PathValue("recordKey")
	}
	if err != nil {
		return err
	}
	if err = aiFresh(e, source, v.Revision); err != nil {
		return err
	}
	preview, err := s.Prepare(aiassist.Scope{Actor: e.Auth.Id, Cluster: cid, Source: source, Target: target, Revision: v.Revision}, aiassist.Project(v.Report), v.Report.ObservedAt)
	if err != nil {
		return aiError(e, err)
	}
	return response(e, preview)
}

var aiPreviewID = regexp.MustCompile(`^[A-Z2-7]{26,64}$`)
var aiDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func aiExplain(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	var body struct {
		Confirmation string `json:"confirmation"`
		Digest       string `json:"digest"`
	}
	if err := readConfigurationBody(e, &body); err != nil {
		return err
	}
	id := e.Request.PathValue("previewId")
	if !aiPreviewID.MatchString(id) || !aiDigest.MatchString(body.Digest) || body.Confirmation != "SEND AI" {
		return e.BadRequestError("须确认本次预览并输入 SEND AI。", nil)
	}
	invocation, err := aiassist.For(e.App).Take(e.Auth.Id, e.Request.PathValue("clusterId"), id, body.Digest)
	if err != nil {
		return aiError(e, err)
	}
	defer invocation.Release()
	scope := invocation.Scope
	if err = aiFresh(e, scope.Source, scope.Revision); err != nil {
		return err
	}
	collection, err := e.App.FindCollectionByNameOrId("operation_audits")
	if err != nil {
		return e.Error(503, "审计不可用。", nil)
	}
	record := core.NewRecord(collection)
	if err = audit.Stamp(e.App, record, audit.PlatformActor(e.Auth, e.Get("requestId").(string)), scope.Cluster, "ai_explanation", scope.Source+":"+scope.Target); err != nil {
		return e.Error(503, "审计不可用。", nil)
	}
	operation := rand.Text()
	record.Set("operation_key", operation)
	record.Set("action", "ai.explain")
	record.Set("status", "processing")
	record.Set("cluster_revision", scope.Revision)
	record.Set("observed_states", []string{})
	record.Set("message", "已记录发送意图；请求可能尚未完成，请勿重复提交。")
	d := invocation.Preview.Destination
	record.Set("changes", []audit.Change{{Field: "aiProvider", After: d.Label}, {Field: "aiModel", After: d.Model}, {Field: "aiPayloadDigest", After: body.Digest}})
	if err = e.App.Save(record); err != nil {
		return e.Error(503, "审计不可用，未发送 AI 请求。", nil)
	}
	finish := func(status, message string) error {
		record.Set("status", status)
		record.Set("message", message)
		return e.App.Save(record)
	}
	failure := func(message string) error {
		e.Set("publicErrorMessage", message+" 操作编号："+operation+"。不会自动重试，请核对审计记录。")
		return e.Error(503, "AI 请求未完成。", nil)
	}
	if err = aiFresh(e, scope.Source, scope.Revision); err != nil {
		if finish("failed", "权限或集群配置变化，未发送 AI 请求。") != nil {
			return failure("审计结果写入失败，未发送 AI 请求。")
		}
		return err
	}
	answer, callErr := invocation.Call(e.Request.Context())
	if callErr != nil {
		status, message := "unknown", "AI 请求可能已经发送，但未获得通过校验的结果。"
		var f *aiassist.Failure
		if errors.As(callErr, &f) && !f.Sent {
			status = "failed"
			message = "发送前校验失败，未发送 AI 请求。"
		}
		if finish(status, message) != nil {
			return failure("审计结果写入失败，请求可能已发送。")
		}
		return failure(message)
	}
	if err = aiFresh(e, scope.Source, scope.Revision); err != nil {
		if finish("unknown", "请求已发送；返回时权限或集群配置变化，结果已丢弃。") != nil {
			return failure("请求已发送，审计结果写入失败，结果已丢弃。")
		}
		return failure("请求已发送；权限或配置已变化，结果已丢弃。")
	}
	if finish("succeeded", "已收到通过结构校验的 AI 建议，内容未经人工核实。") != nil {
		return failure("请求已发送，审计结果写入失败，结果已丢弃。")
	}
	return response(e, map[string]any{"answer": answer, "operationId": operation, "observedAt": invocation.Preview.ObservedAt, "destination": d})
}
