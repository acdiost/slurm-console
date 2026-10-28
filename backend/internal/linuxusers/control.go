package linuxusers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/rbac"
)

type confirmation struct {
	BeforeExpiry                                *int64
	Actor, Cluster, Action, Operation, Expected string
	Spec                                        CreateSpec
	Revision                                    int
	Expires                                     time.Time
}
type Prepared struct {
	Allowed     bool       `json:"allowed"`
	Message     string     `json:"message"`
	Proof       string     `json:"proof"`
	OperationID string     `json:"operationId"`
	ExpiresAt   string     `json:"expiresAt"`
	Action      string     `json:"action"`
	Spec        CreateSpec `json:"spec"`
	Account     *User      `json:"account"`
	Expiry      *int64     `json:"expiry"`
	MinUID      uint32     `json:"minUid"`
	MaxUID      uint32     `json:"maxUid"`
}
type ControlInput struct {
	Proof        string `json:"proof"`
	Confirmation string `json:"confirmation"`
	Reason       string `json:"reason"`
}
type OperationResult struct {
	OperationID string `json:"operationId"`
	Name        string `json:"name"`
	Action      string `json:"action"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}
type Capabilities struct {
	RecentOperations []OperationResult `json:"recentOperations"`
	Enabled          bool              `json:"enabled"`
	Actions          []string          `json:"actions"`
	MinUID           uint32            `json:"minUid"`
	MaxUID           uint32            `json:"maxUid"`
	Message          string            `json:"message"`
}

func (s *Service) SetAdminProvider(p AdminProvider) { s.admin = p }
func (s *Service) manageable(cluster string) (int, error) {
	r, err := s.app.FindRecordById("clusters", cluster)
	if err != nil {
		return 0, fail("not_found")
	}
	if !r.GetBool("enabled") {
		return 0, fail("disabled")
	}
	p, ok := s.registry.Clusters[cluster]
	if !ok || !p.ManageLocal || p.Transport != "local" {
		return 0, fail("management_disabled")
	}
	return r.GetInt("revision"), nil
}
func (s *Service) authorize(actor, cluster, action string) error {
	a, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return err
	}
	if !a.Allows("linux_users.read", cluster) || !a.Allows("linux_users."+action, cluster) {
		return fail("forbidden")
	}
	return nil
}
func (s *Service) Capabilities(ctx context.Context, actor, cluster string) (Capabilities, error) {
	result := Capabilities{Actions: []string{}, RecentOperations: []OperationResult{}, Message: "此来源未启用本地账户写入，或当前用户没有写权限。"}
	rows, err := s.app.FindRecordsByFilter("operation_audits", "cluster_id={:cluster} && target_type='linux_user' && actor_id={:actor}", "-created,-id", 5, 0, dbx.Params{"cluster": cluster, "actor": actor})
	if err != nil {
		return result, err
	}
	for _, row := range rows {
		result.RecentOperations = append(result.RecentOperations, operationView(row))
	}
	if _, err := s.manageable(cluster); err != nil {
		return result, nil
	}
	for _, action := range []string{"create", "lock", "unlock"} {
		if s.authorize(actor, cluster, action) == nil {
			result.Actions = append(result.Actions, action)
		}
	}
	if len(result.Actions) == 0 {
		return result, nil
	}
	v, err := s.admin.Run(ctx, AdminRequest{Mode: "capabilities"})
	if err != nil {
		return result, err
	}
	if !v.Allowed {
		result.Actions = []string{}
		result.Message = v.Message
		return result, nil
	}
	a, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return result, err
	}
	if !a.Allows("linux_users.read", cluster) {
		return result, fail("forbidden")
	}
	result.Actions = slices.DeleteFunc(result.Actions, func(action string) bool { return !a.Allows("linux_users."+action, cluster) })
	result.Enabled = len(result.Actions) > 0
	result.MinUID = v.MinUID
	result.MaxUID = v.MaxUID
	result.Message = "操作作用于此身份主机的本地账户；共享该身份的系统也可能受到影响。"
	return result, nil
}
func (s *Service) Prepare(ctx context.Context, actor, cluster, action string, spec CreateSpec) (Prepared, error) {
	result := Prepared{Action: action, Spec: spec}
	if !validAdminAction(action) || !localName.MatchString(spec.Name) || action == "create" && !ValidCreate(spec) || action != "create" && (spec.UID != 0 || spec.Comment != "" || spec.Shell != "") {
		return result, fail("invalid_request")
	}
	if err := s.authorize(actor, cluster, action); err != nil {
		return result, err
	}
	revision, err := s.manageable(cluster)
	if err != nil {
		return result, err
	}
	if pending, err := s.app.CountRecords("operation_audits", dbx.And(dbx.HashExp{"cluster_id": cluster, "target_type": "linux_user", "target_id": spec.Name}, dbx.In("status", "processing", "unknown"))); err != nil {
		return result, err
	} else if pending > 0 {
		result.Message = "该账户存在待核实操作，请先查询原操作记录。"
		return result, nil
	}
	v, err := s.admin.Run(ctx, AdminRequest{Mode: "prepare", Action: action, Spec: spec})
	if err != nil {
		return result, err
	}
	if err = s.authorize(actor, cluster, action); err != nil {
		return result, err
	}
	latest, err := s.manageable(cluster)
	if err != nil || latest != revision {
		return result, fail("changed")
	}
	result.Message = v.Message
	result.MinUID = v.MinUID
	result.MaxUID = v.MaxUID
	if v.State != nil {
		u := v.State.User
		result.Account = &u
		x := v.State.Expiry
		result.Expiry = &x
	}
	if !v.Allowed {
		return result, nil
	}
	if len(v.Fingerprint) != 64 {
		return result, fail("invalid_response")
	}
	token := rand.Text()
	value := confirmation{Actor: actor, Cluster: cluster, Action: action, Operation: rand.Text(), Expected: v.Fingerprint, Spec: spec, Revision: revision, Expires: time.Now().Add(2 * time.Minute)}
	s.confirmMu.Lock()
	defer s.confirmMu.Unlock()
	for key, p := range s.confirmations {
		if time.Now().After(p.Expires) {
			delete(s.confirmations, key)
		}
	}
	if len(s.confirmations) >= 256 {
		return result, fail("busy")
	}
	if v.State != nil {
		x := v.State.Expiry
		value.BeforeExpiry = &x
	}
	s.confirmations[token] = value
	result.Allowed = true
	result.Proof = token
	result.OperationID = value.Operation
	result.ExpiresAt = value.Expires.UTC().Format(time.RFC3339)
	return result, nil
}
func (s *Service) findOperation(id string) (*core.Record, error) {
	return s.app.FindFirstRecordByFilter("operation_audits", "operation_key={:id} && target_type='linux_user'", dbx.Params{"id": id})
}
func operationView(r *core.Record) OperationResult {
	status := r.GetString("status")
	message := r.GetString("message")
	if status == "processing" {
		status = "unknown"
		message = "操作正在处理或执行曾中断，请查询核实，勿重复提交。"
	}
	return OperationResult{r.GetString("operation_key"), r.GetString("target_id"), strings.TrimPrefix(r.GetString("action"), "linux_users."), status, message, r.GetString("created"), r.GetString("updated")}
}
func (s *Service) GetOperation(ctx context.Context, actor, cluster, id string) (OperationResult, error) {
	r, err := s.findOperation(id)
	if errors.Is(err, sql.ErrNoRows) {
		return OperationResult{}, fail("not_found")
	}
	if err != nil {
		return OperationResult{}, err
	}
	a, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return OperationResult{}, err
	}
	if r.GetString("cluster_id") != cluster || !a.Allows("linux_users.read", cluster) || r.GetString("actor_id") != actor && !a.Allows("platform.audit.read", "") {
		return OperationResult{}, fail("forbidden")
	}
	value := operationView(r)
	if value.Status != "unknown" {
		return value, nil
	}
	if _, active := s.busy.Load(cluster + ":" + r.GetString("target_id")); active {
		return value, nil
	}
	if _, err := s.manageable(cluster); err != nil {
		return value, nil
	}
	result, err := s.admin.Run(ctx, AdminRequest{Mode: "status", OperationID: id})
	if err != nil || result.Status != "confirmed" && result.Status != "failed" {
		return value, nil
	}
	// Reconciliation updates evidence only; it never sends another OS mutation.
	r.Set("status", result.Status)
	r.Set("message", result.Message)
	if err = s.app.Save(r); err != nil {
		return value, nil
	}
	return operationView(r), nil
}
func (s *Service) Execute(actor, cluster, requestID string, input ControlInput) (OperationResult, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Proof) > 128 || utf8.RuneCountInString(input.Reason) < 2 || utf8.RuneCountInString(input.Reason) > 300 || strings.ContainsFunc(input.Reason, unicode.IsControl) {
		return OperationResult{}, fail("invalid_request")
	}
	s.confirmMu.Lock()
	p, ok := s.confirmations[input.Proof]
	s.confirmMu.Unlock()
	if !ok || p.Actor != actor || p.Cluster != cluster || input.Confirmation != p.Spec.Name {
		return OperationResult{}, fail("confirmation_invalid")
	}
	if err := s.authorize(actor, cluster, p.Action); err != nil {
		return OperationResult{}, err
	}
	inputHash := Hash(input)
	if r, err := s.findOperation(p.Operation); err == nil {
		if r.GetString("input_hash") != inputHash {
			return OperationResult{}, fail("confirmation_invalid")
		}
		return s.GetOperation(s.ctx, actor, cluster, p.Operation)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return OperationResult{}, err
	}
	if time.Now().After(p.Expires) {
		return OperationResult{}, fail("confirmation_expired")
	}
	key := cluster + ":" + p.Spec.Name
	if _, exists := s.busy.LoadOrStore(key, true); exists {
		return OperationResult{}, fail("busy")
	}
	defer s.busy.Delete(key)
	// The unique operation key prevents replay across concurrent requests.
	if r, err := s.findOperation(p.Operation); err == nil {
		if r.GetString("input_hash") != inputHash {
			return OperationResult{}, fail("confirmation_invalid")
		}
		return operationView(r), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return OperationResult{}, err
	}
	revision, err := s.manageable(cluster)
	if err != nil || revision != p.Revision {
		return OperationResult{}, fail("changed")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 25*time.Second)
	defer cancel()
	collection, err := s.app.FindCollectionByNameOrId("operation_audits")
	if err != nil {
		return OperationResult{}, err
	}
	r := core.NewRecord(collection)
	for field, v := range map[string]any{"operation_key": p.Operation, "actor_id": actor, "cluster_id": cluster, "cluster_revision": revision, "action": "linux_users." + p.Action, "target_type": "linux_user", "target_id": p.Spec.Name, "request_id": requestID, "status": "processing", "message": "本地账户操作正在执行。", "reason": input.Reason, "input_hash": inputHash} {
		r.Set(field, v)
	}
	user, err := s.app.FindRecordById("users", actor)
	if err != nil {
		return OperationResult{}, err
	}
	if err = audit.Stamp(s.app, r, audit.PlatformActor(user, requestID), cluster, "linux_user", p.Spec.Name); err != nil {
		return OperationResult{}, err
	}
	if err = s.app.Save(r); err != nil {
		return OperationResult{}, fail("audit_unavailable")
	}
	var observed *AccountState
	finish := func(status, message string) (OperationResult, error) {
		r.Set("status", status)
		r.Set("message", message)
		if status == "confirmed" && observed != nil {
			changes := []audit.Change{}
			if p.Action == "create" {
				changes = append(changes, audit.Change{Field: "uid", After: strconv.FormatUint(uint64(observed.User.UID), 10)}, audit.Change{Field: "home", After: observed.User.Home}, audit.Change{Field: "shell", After: observed.User.Shell})
			}
			before := ""
			if p.BeforeExpiry != nil {
				before = strconv.FormatInt(*p.BeforeExpiry, 10)
			}
			changes = append(changes, audit.Change{Field: "accountExpiry", Before: before, After: strconv.FormatInt(observed.Expiry, 10)})
			r.Set("changes", changes)
		}
		if err := s.app.Save(r); err != nil {
			value := operationView(r)
			value.Status = "unknown"
			value.Message = "结果未能持久化，请查询原操作核实。"
			return value, nil
		}
		return operationView(r), nil
	}
	latest, e := s.manageable(cluster)
	if e != nil || latest != revision || s.authorize(actor, cluster, p.Action) != nil || time.Now().After(p.Expires) {
		return finish("failed", "权限、配置或确认已变化，未发送主机写请求。")
	}
	v, e := s.admin.Run(ctx, AdminRequest{Mode: "execute", Action: p.Action, Spec: p.Spec, OperationID: p.Operation, Expected: p.Expected})
	if e != nil {
		return finish("unknown", "主机请求结果不确定，请查询原操作，勿重复提交。")
	}
	if v.Status == "confirmed" {
		observed = v.State
		return finish("confirmed", v.Message)
	}
	if v.Status == "unknown" {
		return finish("unknown", v.Message)
	}
	return finish("failed", v.Message)
}
