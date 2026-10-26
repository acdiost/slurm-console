// Package nodecontrol implements literal-node, audited Drain/Resume transitions.
package nodecontrol

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
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
)

type Prepared struct {
	Allowed     bool        `json:"allowed"`
	Message     string      `json:"message"`
	Node        models.Node `json:"node"`
	Action      string      `json:"action"`
	Proof       string      `json:"proof"`
	OperationID string      `json:"operationId"`
	ExpiresAt   string      `json:"expiresAt"`
}
type Input struct {
	Proof        string `json:"proof"`
	Confirmation string `json:"confirmation"`
	Reason       string `json:"reason"`
}
type Result struct {
	OperationID string   `json:"operationId"`
	NodeName    string   `json:"nodeName"`
	Action      string   `json:"action"`
	Status      string   `json:"status"`
	Message     string   `json:"message"`
	Reason      string   `json:"reason"`
	States      []string `json:"states"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}
type proof struct {
	Actor, Cluster, Node, Action, Operation, Fingerprint, State string
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
	if s, ok := app.Store().Get("platform.nodeControl").(*Service); ok {
		return s
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{app: app, slots: make(chan struct{}, 4), ctx: ctx}
	_, _ = rand.Read(s.key[:])
	app.Store().Set("platform.nodeControl", s)
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error { cancel(); return e.Next() })
	return s
}
func digest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func fingerprint(n models.Node) string {
	return digest(struct {
		Name, Hostname, Address, Version string
		CPUs, Memory                     int64
		Boot                             models.Number
	}{n.Name, n.Hostname, n.Address, n.Version, n.CPUs, n.MemoryMiB, n.BootTime})
}
func stateKey(n models.Node) string {
	return digest(struct {
		States                                   []string
		Reason, ReasonUser, Comment, Reservation string
		ReasonTime, ResumeAfter                  models.Number
		CPUs, Memory                             int64
	}{slices.Sorted(slices.Values(n.States)), n.Reason, n.ReasonUser, n.Comment, n.Reservation, n.ReasonChangedAt, n.ResumeAfter, n.AllocatedCPUs, n.AllocatedMemoryMiB})
}
func validAction(action string) bool { return action == "drain" || action == "resume" }
func statePolicy(n models.Node, action string) string {
	if !slices.Contains(n.States, "IDLE") && !slices.Contains(n.States, "DOWN") && !slices.Contains(n.States, "ALLOCATED") && !slices.Contains(n.States, "MIXED") {
		return "当前节点基础状态暂不支持此操作。"
	}
	for _, state := range []string{"INVALID", "INVALID_REG", "UNKNOWN", "FUTURE", "DYNAMIC_FUTURE", "REBOOT_REQUESTED", "REBOOT_ISSUED"} {
		if slices.Contains(n.States, state) {
			return "节点正在注册、重启或尚未就绪，请先由管理员核实。"
		}
	}
	if action == "drain" {
		if slices.Contains(n.States, "DRAIN") {
			return "节点已带 DRAIN 标记，无需重复排空。"
		}
		return ""
	}
	if !slices.Contains(n.States, "DRAIN") && !slices.Contains(n.States, "DOWN") {
		return "节点没有 DRAIN 或 DOWN 标记，无需恢复。"
	}
	if n.Reservation != "" {
		return "节点存在预留，请由管理员核实维护和预留安排后处理。"
	}
	for _, state := range []string{"MAINTENANCE", "RESERVED", "FAIL", "POWERED_DOWN", "POWERING_DOWN", "POWERING_UP", "POWER_DOWN", "POWER_UP", "POWER_DRAIN"} {
		if slices.Contains(n.States, state) {
			return "当前维护、故障或电源状态不支持普通 Resume。"
		}
	}
	return ""
}
func (s *Service) policy(actor, cluster, action string, n models.Node) (string, error) {
	access, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return "", err
	}
	if !access.Allows("nodes.read", cluster) || !access.Allows("nodes."+action, cluster) {
		return "未授予该集群的节点操作权限。", nil
	}
	if denied := statePolicy(n, action); denied != "" {
		return denied, nil
	}
	pending, err := s.app.CountRecords("operation_audits", dbx.And(dbx.HashExp{"cluster_id": cluster, "node_name": n.Name}, dbx.In("status", "processing", "accepted", "unknown")))
	if err != nil {
		return "", err
	}
	if pending > 0 {
		return "该节点已有操作正在处理或结果待核实，请先查询操作记录。", nil
	}
	return "", nil
}
func (s *Service) Prepare(ctx context.Context, actor, cluster, name, action string) (Prepared, error) {
	if !client.ValidNodeTarget(name) || !validAction(action) {
		return Prepared{}, client.Failure("invalid_request", "节点或动作不合法。")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return Prepared{}, client.Failure("busy", "节点校验繁忙，请稍后重试。")
	}
	connection, err := services.ConnectionFor(s.app, cluster)
	if err != nil {
		return Prepared{}, err
	}
	n, err := connection.Adapter.GetNode(ctx, name)
	if err != nil {
		return Prepared{}, err
	}
	result := Prepared{Node: n, Action: action}
	denied, err := s.policy(actor, cluster, action, n)
	if err != nil {
		return result, err
	}
	if denied != "" {
		result.Message = denied
		return result, nil
	}
	value := proof{Actor: actor, Cluster: cluster, Node: name, Action: action, Operation: rand.Text(), Fingerprint: fingerprint(n), State: stateKey(n), Revision: connection.Revision, Expires: time.Now().Add(2 * time.Minute).Unix()}
	data, _ := json.Marshal(value)
	encoded := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(encoded))
	result.Allowed = true
	result.Proof = encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	result.OperationID = value.Operation
	result.ExpiresAt = time.Unix(value.Expires, 0).UTC().Format(time.RFC3339)
	result.Message = "请核对最新节点状态和影响范围，填写原因并确认。"
	return result, nil
}
func (s *Service) parse(raw, actor, cluster, name, action string) (proof, error) {
	var value proof
	failure := client.Failure("confirmation_invalid", "节点操作确认无效或服务已重启，请重新校验。")
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
	if err != nil || json.Unmarshal(data, &value) != nil || value.Actor != actor || value.Cluster != cluster || value.Node != name || value.Action != action || value.Operation == "" {
		return proof{}, failure
	}
	return value, nil
}
func (s *Service) find(operation string) (*core.Record, error) {
	return s.app.FindFirstRecordByFilter("operation_audits", "operation_key={:key} && (action='nodes.drain' || action='nodes.resume')", dbx.Params{"key": operation})
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
		if kind, active := s.busy.Load(record.GetString("cluster_id") + ":" + record.GetString("node_name")); !active || kind != "execute" {
			status = "unknown"
			message = "上次执行可能已中断，请核对节点状态，勿重复提交。"
		}
	}
	return Result{record.GetString("operation_key"), record.GetString("node_name"), strings.TrimPrefix(record.GetString("action"), "nodes."), status, message, record.GetString("reason"), states, record.GetString("created"), record.GetString("updated")}
}
func reached(n models.Node, action, reason string) bool {
	if action == "drain" {
		return slices.Contains(n.States, "DRAIN") && n.Reason == reason
	}
	if action != "resume" {
		return false
	}
	for _, state := range []string{"DRAIN", "DOWN", "FAIL", "NOT_RESPONDING", "UNKNOWN", "INVALID", "INVALID_REG", "FUTURE", "MAINTENANCE", "REBOOT_REQUESTED", "REBOOT_ISSUED", "POWERED_DOWN", "POWERING_DOWN", "POWERING_UP"} {
		if slices.Contains(n.States, state) {
			return false
		}
	}
	return slices.Contains(n.States, "IDLE") || slices.Contains(n.States, "ALLOCATED") || slices.Contains(n.States, "MIXED")
}
func confirmedMessage(action string) string {
	if action == "drain" {
		return "已核实 DRAIN 标记，节点停止接收新作业；已有作业仍可继续运行。"
	}
	return "已核实节点已恢复调度状态。"
}
func (s *Service) Get(ctx context.Context, actor, cluster, operation string) (Result, error) {
	record, err := s.find(operation)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, client.Failure("not_found", "节点操作记录不存在。")
	}
	if err != nil {
		return Result{}, err
	}
	access, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return Result{}, err
	}
	if record.GetString("cluster_id") != cluster || !access.Allows("nodes.read", cluster) || (!access.Allows("platform.audit.read", "") && record.GetString("actor_id") != actor) {
		return Result{}, client.Failure("operation_forbidden", "无权读取该节点操作记录。")
	}
	if !slices.Contains([]string{"processing", "accepted", "unknown"}, record.GetString("status")) {
		return s.view(record), nil
	}
	key := cluster + ":" + record.GetString("node_name")
	if _, busy := s.busy.LoadOrStore(key, "read"); busy {
		return s.view(record), nil
	}
	defer s.busy.Delete(key)
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return s.view(record), nil
	}
	connection, err := services.ConnectionFor(s.app, cluster)
	if err != nil || connection.Revision != record.GetInt("cluster_revision") {
		return s.view(record), nil
	}
	readCtx, cancel := context.WithTimeout(ctx, connection.Timeout)
	defer cancel()
	n, readErr := connection.Adapter.GetNode(readCtx, record.GetString("node_name"))
	current, currentErr := services.ConnectionFor(s.app, cluster)
	if readErr == nil && currentErr == nil && current.Revision == connection.Revision && fingerprint(n) == record.GetString("fingerprint") && reached(n, strings.TrimPrefix(record.GetString("action"), "nodes."), record.GetString("reason")) {
		record.Set("status", "confirmed")
		record.Set("message", confirmedMessage(strings.TrimPrefix(record.GetString("action"), "nodes.")))
		record.Set("observed_states", n.States)
		if err := s.app.Save(record); err != nil {
			return Result{}, client.Failure("audit_unavailable", "核实结果未能持久化，请稍后查询。")
		}
		services.SlurmFor(s.app).Invalidate(cluster)
	}
	// Acquiring the reconciliation lock does not make a crashed mutation active.
	result := s.view(record)
	if result.Status == "processing" {
		result.Status = "unknown"
		result.Message = "操作结果待核实，请核对节点状态，勿重复提交。"
	}
	return result, nil
}
func (s *Service) Execute(actor, cluster, name, action, requestID string, input Input) (Result, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !client.ValidNodeTarget(name) || !validAction(action) || input.Confirmation != name || utf8.RuneCountInString(input.Reason) < 2 || utf8.RuneCountInString(input.Reason) > 300 || strings.ContainsAny(input.Reason, "\r\n\x00") {
		return Result{}, client.Failure("invalid_request", "请输入完整节点名称和 2–300 字单行操作原因。")
	}
	token, err := s.parse(input.Proof, actor, cluster, name, action)
	if err != nil {
		return Result{}, err
	}
	inputHash := digest(input)
	replay := func(record *core.Record) (Result, error) {
		if record.GetString("actor_id") != actor || record.GetString("cluster_id") != cluster || record.GetString("node_name") != name || record.GetString("action") != "nodes."+action || record.GetString("input_hash") != inputHash {
			return Result{}, client.Failure("confirmation_invalid", "同一确认请求的内容不能变更。")
		}
		return s.Get(s.ctx, actor, cluster, token.Operation)
	}
	if record, err := s.find(token.Operation); err == nil {
		return replay(record)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	if token.Expires <= time.Now().Unix() {
		return Result{}, client.Failure("confirmation_expired", "节点确认已过期，请重新校验。")
	}
	key := cluster + ":" + name
	if _, exists := s.busy.LoadOrStore(key, "execute"); exists {
		return Result{}, client.Failure("busy", "该节点已有操作正在处理。")
	}
	defer s.busy.Delete(key)
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return Result{}, client.Failure("busy", "节点操作繁忙，请稍后重试。")
	}
	if record, err := s.find(token.Operation); err == nil {
		return replay(record)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	connection, err := services.ConnectionFor(s.app, cluster)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 3*connection.Timeout+time.Second)
	defer cancel()
	n, err := connection.Adapter.GetNode(ctx, name)
	if err != nil {
		return Result{}, err
	}
	if token.Revision != connection.Revision || token.Fingerprint != fingerprint(n) || token.State != stateKey(n) {
		return Result{}, client.Failure("confirmation_changed", "节点身份、状态、原因或配置已变化，请重新校验。")
	}
	denied, err := s.policy(actor, cluster, action, n)
	if err != nil {
		return Result{}, err
	}
	if denied != "" {
		return Result{}, client.Failure("operation_forbidden", denied)
	}
	collection, err := s.app.FindCollectionByNameOrId("operation_audits")
	if err != nil {
		return Result{}, err
	}
	record := core.NewRecord(collection)
	for field, value := range map[string]any{"operation_key": token.Operation, "actor_id": actor, "cluster_id": cluster, "cluster_revision": token.Revision, "node_name": name, "action": "nodes." + action, "request_id": requestID, "status": "processing", "message": "节点操作正在处理。", "reason": input.Reason, "input_hash": inputHash, "fingerprint": token.Fingerprint, "observed_states": n.States} {
		record.Set(field, value)
	}
	if err := s.app.Save(record); err != nil {
		return Result{}, client.Failure("audit_unavailable", "无法写入审计，未发送节点控制请求。")
	}
	finish := func(status, message string, states []string) (Result, error) {
		record.Set("status", status)
		record.Set("message", message)
		record.Set("observed_states", states)
		if err := s.app.Save(record); err != nil {
			result := s.view(record)
			result.Status = "unknown"
			result.Message = "操作结果未能持久化，请核对节点状态，勿重复提交。"
			slog.Error("node audit completion unavailable", "operationId", token.Operation)
			return result, nil
		}
		slog.Info("node control", "operationId", token.Operation, "requestId", requestID, "actorId", actor, "clusterId", cluster, "nodeName", name, "action", action, "status", status)
		return s.view(record), nil
	}
	current, err := services.ConnectionFor(s.app, cluster)
	if err != nil || current.Revision != token.Revision {
		return finish("failed", "连接配置已变化，未发送节点控制请求。", n.States)
	}
	access, err := rbac.Resolve(s.app, actor)
	if err != nil || !access.Allows("nodes.read", cluster) || !access.Allows("nodes."+action, cluster) || token.Expires <= time.Now().Unix() {
		return finish("failed", "权限已变化或确认已过期，未发送节点控制请求。", n.States)
	}
	services.SlurmFor(s.app).Invalidate(cluster)
	sent, writeErr := current.Client.ControlNode(ctx, name, action, input.Reason)
	if sent {
		services.SlurmFor(s.app).Invalidate(cluster)
	}
	observed, readErr := current.Adapter.GetNode(ctx, name)
	if readErr == nil && fingerprint(observed) == token.Fingerprint && reached(observed, action, input.Reason) {
		return finish("confirmed", confirmedMessage(action), observed.States)
	}
	states := []string{}
	if readErr == nil && fingerprint(observed) == token.Fingerprint {
		states = observed.States
	}
	if writeErr == nil {
		return finish("accepted", "Slurm 已受理，请等待状态更新；Resume 在节点重新注册前不代表节点已就绪。", states)
	}
	if !sent {
		return finish("failed", writeErr.Error(), n.States)
	}
	return finish("unknown", "节点操作结果尚未核实，请查询操作记录及最新状态，勿重复提交。", states)
}
