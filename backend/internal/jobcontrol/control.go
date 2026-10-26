// Package jobcontrol implements explicit, audited, non-retrying job cancellation.
package jobcontrol

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
)

type Prepared struct {
	Allowed     bool       `json:"allowed"`
	Message     string     `json:"message"`
	Job         models.Job `json:"job"`
	Identity    *Identity  `json:"identity"`
	Proof       string     `json:"proof"`
	OperationID string     `json:"operationId"`
	ExpiresAt   string     `json:"expiresAt"`
}
type Input struct {
	Proof        string `json:"proof"`
	Confirmation string `json:"confirmation"`
	Reason       string `json:"reason"`
}
type Result struct {
	OperationID string   `json:"operationId"`
	JobID       string   `json:"jobId"`
	Status      string   `json:"status"`
	Message     string   `json:"message"`
	Reason      string   `json:"reason"`
	States      []string `json:"states"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}
type proof struct {
	Actor, Cluster, Job, Operation, Fingerprint, State, Binding string
	Revision                                                    int
	Expires                                                     int64
}
type Service struct {
	app   core.App
	key   [32]byte
	busy  sync.Map
	slots chan struct{}
	ctx   context.Context
}

var initialize sync.Mutex

func For(app core.App) *Service {
	initialize.Lock()
	defer initialize.Unlock()
	if s, ok := app.Store().Get("platform.jobControl").(*Service); ok {
		return s
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{app: app, slots: make(chan struct{}, 4), ctx: ctx}
	_, _ = rand.Read(s.key[:])
	app.Store().Set("platform.jobControl", s)
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error { cancel(); return e.Next() })
	return s
}
func digest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func fingerprint(job models.Job) string {
	return digest(struct {
		ID, User    string
		UID, Submit *int64
	}{job.ID, job.User, job.UserID, job.SubmitTime.Value})
}
func stateKey(job models.Job) string {
	return digest(struct {
		States                   []string
		Restart                  *int64
		Name, Partition, Account string
	}{slices.Sorted(slices.Values(job.States)), job.RestartCount, job.Name, job.Partition, job.Account})
}
func bindingKey(value Identity) string {
	return digest(struct {
		ID, Username, Scope, Updated string
		UID                          int64
	}{value.ID, value.Username, value.Scope, value.Updated, value.UID})
}
func terminal(job models.Job) bool {
	for _, s := range []string{"CANCELLED", "COMPLETED", "FAILED", "TIMEOUT", "NODE_FAIL", "BOOT_FAIL", "DEADLINE", "OUT_OF_MEMORY", "PREEMPTED"} {
		if slices.Contains(job.States, s) {
			return true
		}
	}
	return false
}
func zeroMarker(value models.Number) bool {
	return !value.Infinite && value.Value != nil && *value.Value == 0
}
func ordinary(job models.Job) bool {
	return job.UserID != nil && *job.UserID > 0 && *job.UserID <= 4294967295 && job.User != "" && job.User != "root" &&
		job.SubmitTime.Value != nil && *job.SubmitTime.Value > 0 && !job.SubmitTime.Infinite &&
		job.RestartCount != nil && *job.RestartCount >= 0 && zeroMarker(job.ArrayJobID) &&
		job.ArrayTaskID.Value == nil && !job.ArrayTaskID.Infinite && job.ArrayTaskString == "" &&
		zeroMarker(job.HetJobID) && zeroMarker(job.HetJobOffset) && job.HetJobIDSet == ""
}
func (s *Service) policy(actor, cluster string, job models.Job) (Identity, string, error) {
	access, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return Identity{}, "", err
	}
	if !access.Allows("jobs.read", cluster) || !access.Allows("jobs.cancel", cluster) {
		return Identity{}, "未授予该集群的作业取消权限。", nil
	}
	linked, err := identity(s.app, actor, cluster)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, "未关联 Slurm 身份，请联系管理员完成关联。", nil
	}
	if err != nil {
		return Identity{}, "", err
	}
	if linked.UID <= 0 || linked.Username == "root" || (linked.Scope != "own" && linked.Scope != "all") {
		return linked, "Slurm 身份配置无效。", nil
	}
	if !ordinary(job) {
		return linked, "仅支持身份及提交信息完整的普通非 root 作业；数组和异构作业暂不支持取消。", nil
	}
	if linked.Scope == "own" && (linked.UID != *job.UserID || linked.Username != job.User) {
		return linked, "该作业不属于已关联的 Slurm 身份。", nil
	}
	if terminal(job) || slices.Contains(job.States, "COMPLETING") {
		return linked, "作业已结束或正在收尾，不能再次取消。", nil
	}
	if !slices.Contains(job.States, "PENDING") && !slices.Contains(job.States, "RUNNING") && !slices.Contains(job.States, "SUSPENDED") {
		return linked, "当前状态暂不支持取消，请刷新后核实。", nil
	}
	pending, err := s.app.CountRecords("operation_audits", dbx.And(dbx.HashExp{"cluster_id": cluster, "job_id": job.ID, "fingerprint": fingerprint(job)}, dbx.In("status", "processing", "accepted", "unknown")))
	if err != nil {
		return linked, "", err
	}
	if pending > 0 {
		return linked, "已有取消请求正在处理或结果待核实，请先核对作业状态，勿重复提交。", nil
	}
	return linked, "", nil
}
func (s *Service) Prepare(ctx context.Context, actor, cluster, jobID string) (Prepared, error) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return Prepared{}, client.Failure("busy", "作业校验请求繁忙，请稍后重试。")
	}
	connection, err := services.ConnectionFor(s.app, cluster)
	if err != nil {
		return Prepared{}, err
	}
	job, err := connection.Adapter.GetJob(ctx, jobID)
	if err != nil {
		return Prepared{}, err
	}
	result := Prepared{Job: job, Message: "取消暂不可用。"}
	linked, denied, err := s.policy(actor, cluster, job)
	if err != nil {
		return result, err
	}
	if linked.ID != "" {
		result.Identity = &linked
	}
	if denied != "" {
		result.Message = denied
		return result, nil
	}
	payload := proof{Actor: actor, Cluster: cluster, Job: jobID, Operation: rand.Text(), Fingerprint: fingerprint(job), State: stateKey(job), Binding: bindingKey(linked), Revision: connection.Revision, Expires: time.Now().Add(2 * time.Minute).Unix()}
	data, _ := json.Marshal(payload)
	encoded := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(encoded))
	result.Allowed = true
	result.Message = "请核对最新作业归属及状态，填写原因并输入 Job ID 确认。"
	result.OperationID = payload.Operation
	result.ExpiresAt = time.Unix(payload.Expires, 0).UTC().Format(time.RFC3339)
	result.Proof = encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return result, nil
}
func (s *Service) parse(raw, actor, cluster, job string) (proof, error) {
	var value proof
	failure := client.Failure("confirmation_invalid", "取消确认无效或服务已重启，请重新校验。")
	parts := strings.Split(raw, ".")
	if len(raw) > 4096 || len(parts) != 2 {
		return value, failure
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return value, failure
	}
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return value, failure
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(data, &value) != nil || value.Actor != actor || value.Cluster != cluster || value.Job != job || value.Operation == "" {
		return proof{}, failure
	}
	return value, nil
}
func (s *Service) find(operation string) (*core.Record, error) {
	return s.app.FindFirstRecordByFilter("operation_audits", "operation_key={:key} && action='jobs.cancel'", dbx.Params{"key": operation})
}
func (s *Service) view(record *core.Record) Result {
	states := []string{}
	data, _ := json.Marshal(record.GetRaw("observed_states"))
	_ = json.Unmarshal(data, &states)
	if states == nil {
		states = []string{}
	}
	status, message := record.GetString("status"), record.GetString("message")
	if status == "processing" {
		status = "unknown"
		message = "请求正在处理或上次执行已中断，请核对作业状态，勿重复取消。"
	}
	return Result{record.GetString("operation_key"), record.GetString("job_id"), status, message, record.GetString("reason"), states, record.GetString("created"), record.GetString("updated")}
}
func (s *Service) Get(ctx context.Context, actor, cluster, operation string) (Result, error) {
	record, err := s.find(operation)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, client.Failure("not_found", "操作记录不存在。")
	}
	if err != nil {
		return Result{}, err
	}
	access, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return Result{}, err
	}
	if record.GetString("cluster_id") != cluster || (!access.Allows("platform.audit.read", "") && record.GetString("actor_id") != actor) {
		return Result{}, client.Failure("operation_forbidden", "无权读取该操作记录。")
	}
	if !access.Allows("jobs.read", cluster) {
		return Result{}, client.Failure("operation_forbidden", "无权读取该集群的作业。")
	}
	// Reconcile only by reading. Never send another mutation, including after a
	// process restart left a durable intent without its completion record.
	if !slices.Contains([]string{"processing", "accepted", "unknown"}, record.GetString("status")) {
		return s.view(record), nil
	}
	key := cluster + ":" + record.GetString("job_id")
	if _, busy := s.busy.LoadOrStore(key, true); busy {
		return s.view(record), nil
	}
	defer s.busy.Delete(key)
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return s.view(record), nil
	}
	connection, connectionErr := services.ConnectionFor(s.app, cluster)
	if connectionErr != nil || connection.Revision != record.GetInt("cluster_revision") {
		return s.view(record), nil
	}
	readCtx, cancel := context.WithTimeout(ctx, connection.Timeout)
	defer cancel()
	job, readErr := connection.Adapter.GetJob(readCtx, record.GetString("job_id"))
	current, currentErr := services.ConnectionFor(s.app, cluster)
	if readErr == nil && currentErr == nil && current.Revision == connection.Revision && fingerprint(job) == record.GetString("fingerprint") && slices.Contains(job.States, "CANCELLED") && !slices.Contains(job.States, "COMPLETING") {
		record.Set("status", "confirmed")
		record.Set("message", "已核实作业当前为 CANCELLED。")
		record.Set("observed_states", job.States)
		if err := s.app.Save(record); err != nil {
			return Result{}, client.Failure("audit_unavailable", "核实结果未能持久化，请稍后查询操作记录。")
		}
		services.SlurmFor(s.app).Invalidate(cluster)
	}
	return s.view(record), nil
}
func (s *Service) Cancel(actor, cluster, jobID, requestID string, input Input) (Result, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Confirmation != jobID || utf8.RuneCountInString(input.Reason) < 2 || utf8.RuneCountInString(input.Reason) > 300 || strings.ContainsRune(input.Reason, 0) {
		return Result{}, client.Failure("invalid_request", "请输入完整 Job ID 和 2–300 字取消原因。")
	}
	token, err := s.parse(input.Proof, actor, cluster, jobID)
	if err != nil {
		return Result{}, err
	}
	inputHash := digest(input)
	if record, err := s.find(token.Operation); err == nil {
		if record.GetString("actor_id") != actor || record.GetString("cluster_id") != cluster || record.GetString("job_id") != jobID || record.GetString("input_hash") != inputHash {
			return Result{}, client.Failure("confirmation_invalid", "同一确认请求的内容不能变更。")
		}
		return s.Get(s.ctx, actor, cluster, token.Operation)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	if token.Expires <= time.Now().Unix() {
		return Result{}, client.Failure("confirmation_expired", "取消确认已过期，请重新校验。")
	}
	key := cluster + ":" + jobID
	if _, exists := s.busy.LoadOrStore(key, true); exists {
		return Result{}, client.Failure("busy", "该作业的取消请求正在处理。")
	}
	defer s.busy.Delete(key)
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return Result{}, client.Failure("busy", "取消请求繁忙，请稍后重试。")
	}
	// Recheck idempotency after acquiring the job slot, before any external action.
	if record, err := s.find(token.Operation); err == nil {
		if record.GetString("input_hash") != inputHash {
			return Result{}, client.Failure("confirmation_invalid", "同一确认请求的内容不能变更。")
		}
		return s.Get(s.ctx, actor, cluster, token.Operation)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	connection, err := services.ConnectionFor(s.app, cluster)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 3*connection.Timeout+time.Second)
	defer cancel()
	job, err := connection.Adapter.GetJob(ctx, jobID)
	if err != nil {
		return Result{}, err
	}
	if token.Revision != connection.Revision || token.Fingerprint != fingerprint(job) {
		return Result{}, client.Failure("confirmation_changed", "作业身份或集群配置已变化，请重新校验。")
	}
	linked, denied, err := s.policy(actor, cluster, job)
	if err != nil {
		return Result{}, err
	}
	if denied != "" {
		return Result{}, client.Failure("operation_forbidden", denied)
	}
	if token.Binding != bindingKey(linked) || token.State != stateKey(job) {
		return Result{}, client.Failure("confirmation_changed", "作业状态或身份关联已变化，请重新校验。")
	}
	// Persist a durable intent BEFORE the one and only outbound DELETE. A crash
	// leaves an unknown operation, never permission to repeat a destructive call.
	collection, err := s.app.FindCollectionByNameOrId("operation_audits")
	if err != nil {
		return Result{}, err
	}
	record := core.NewRecord(collection)
	for field, value := range map[string]any{"operation_key": token.Operation, "actor_id": actor, "cluster_id": cluster, "cluster_revision": token.Revision, "job_id": jobID, "action": "jobs.cancel", "request_id": requestID, "status": "processing", "message": "取消请求正在处理。", "reason": input.Reason, "input_hash": inputHash, "fingerprint": token.Fingerprint, "observed_states": job.States} {
		record.Set(field, value)
	}
	actorRecord, err := s.app.FindRecordById("users", actor)
	if err != nil {
		return Result{}, err
	}
	if err := audit.Stamp(s.app, record, audit.PlatformActor(actorRecord, requestID), cluster, "job", jobID); err != nil {
		return Result{}, err
	}
	if err := s.app.Save(record); err != nil {
		return Result{}, client.Failure("audit_unavailable", "无法写入操作审计，未发送取消请求。")
	}
	finish := func(status, message string, states []string) (Result, error) {
		record.Set("status", status)
		record.Set("message", message)
		record.Set("observed_states", states)
		if err := s.app.Save(record); err != nil {
			slog.Error("job cancel audit completion unavailable", "operationId", token.Operation, "clusterId", cluster, "jobId", jobID)
			result := s.view(record)
			result.Status = "unknown"
			result.Message = "操作结果未能持久化，请核对作业状态，勿重复取消。"
			return result, nil
		}
		slog.Info("job cancellation", "operationId", token.Operation, "requestId", requestID, "actorId", actor, "clusterId", cluster, "jobId", jobID, "status", status)
		return s.view(record), nil
	}
	// Grants, mapping and configuration may have changed while querying Slurm.
	current, err := services.ConnectionFor(s.app, cluster)
	if err != nil || current.Revision != token.Revision {
		return finish("failed", "连接配置已变化，未发送取消请求。", job.States)
	}
	access, err := rbac.Resolve(s.app, actor)
	freshIdentity, identityErr := identity(s.app, actor, cluster)
	if err != nil || identityErr != nil || !access.Allows("jobs.read", cluster) || !access.Allows("jobs.cancel", cluster) || bindingKey(freshIdentity) != token.Binding {
		return finish("failed", "操作权限或身份关联已变化，未发送取消请求。", job.States)
	}
	if token.Expires <= time.Now().Unix() {
		return finish("failed", "确认已过期，未发送取消请求。", job.States)
	}
	services.SlurmFor(s.app).Invalidate(cluster)
	sent, cancelErr := current.Client.Cancel(ctx, jobID)
	if sent {
		services.SlurmFor(s.app).Invalidate(cluster)
	}
	observed, readErr := current.Adapter.GetJob(ctx, jobID)
	if readErr == nil && fingerprint(observed) == token.Fingerprint && slices.Contains(observed.States, "CANCELLED") && !slices.Contains(observed.States, "COMPLETING") {
		return finish("confirmed", "已核实作业当前为 CANCELLED。", observed.States)
	}
	if cancelErr == nil {
		states := []string{}
		if readErr == nil && fingerprint(observed) == token.Fingerprint {
			states = observed.States
		}
		return finish("accepted", "Slurm 已受理取消请求，请刷新详情核对最终状态。", states)
	}
	if !sent {
		return finish("failed", cancelErr.Error(), job.States)
	}
	states := []string{}
	if readErr == nil && fingerprint(observed) == token.Fingerprint {
		states = observed.States
	}
	return finish("unknown", "取消请求结果尚未核实，请刷新作业状态，勿重复提交。", states)
}
