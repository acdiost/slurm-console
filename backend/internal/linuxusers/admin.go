package linuxusers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const AdminCommand = "linux-users-admin"
const AdminPolicyPath = "/etc/slurm-console/linux-users-admin.json"
const AdminStatePath = "/var/lib/slurm-console-linux-users"

var localName = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
var operationName = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type AdminPolicy struct {
	Enabled        bool     `json:"enabled"`
	CallerUID      uint32   `json:"callerUid"`
	MinUID         uint32   `json:"minUid"`
	MaxUID         uint32   `json:"maxUid"`
	ProtectedUsers []string `json:"protectedUsers"`
}
type CreateSpec struct {
	Name    string `json:"name"`
	UID     uint32 `json:"uid"`
	Comment string `json:"comment"`
	Shell   string `json:"shell"`
}
type AdminRequest struct {
	Mode        string     `json:"mode"`
	Action      string     `json:"action,omitempty"`
	Spec        CreateSpec `json:"spec"`
	OperationID string     `json:"operationId,omitempty"`
	Expected    string     `json:"expected,omitempty"`
}
type AccountState struct {
	User         User   `json:"user"`
	Expiry       int64  `json:"expiry"`
	Identity     string `json:"identity"` // Digest only; never a password or shadow row.
	Privileged   bool   `json:"privileged"`
	PrivateGroup bool   `json:"privateGroup"`
	HomeReady    bool   `json:"homeReady"`
}
type AdminResult struct {
	Allowed          bool          `json:"allowed"`
	Code             string        `json:"code,omitempty"`
	Message          string        `json:"message"`
	Fingerprint      string        `json:"fingerprint,omitempty"`
	State            *AccountState `json:"state,omitempty"`
	LockedByPlatform bool          `json:"lockedByPlatform"`
	MinUID           uint32        `json:"minUid"`
	MaxUID           uint32        `json:"maxUid"`
	Status           string        `json:"status,omitempty"`
	OperationID      string        `json:"operationId,omitempty"`
}
type LockState struct {
	Identity       string `json:"identity"`
	OriginalExpiry int64  `json:"originalExpiry"`
}
type Host interface {
	Inspect(context.Context, string) (AccountState, error)
	CheckCreate(context.Context, CreateSpec) error
	Create(context.Context, CreateSpec) error
	SetExpiry(context.Context, string, int64) error
}
type HostOperation struct {
	Request AdminRequest  `json:"request"`
	Hash    string        `json:"hash"`
	Before  *AccountState `json:"before,omitempty"`
	Result  AdminResult   `json:"result"`
}
type AdminStore interface {
	Operation(string) (HostOperation, error)
	SaveOperation(string, HostOperation) error
	Lock(string) (LockState, error)
	SaveLock(string, LockState) error
	Target(string) (string, error)
	SaveTarget(string, string) error
}
type Engine struct {
	Policy AdminPolicy
	Host   Host
	Store  AdminStore
}

func Hash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func ValidCreate(v CreateSpec) bool {
	return localName.MatchString(v.Name) && v.Name != "root" && v.UID >= 1000 && v.UID <= 60000 && utf8.RuneCountInString(v.Comment) <= 100 && len(v.Comment) <= 400 && !strings.ContainsAny(v.Comment, ":") && !strings.ContainsFunc(v.Comment, unicode.IsControl) && (v.Shell == "/bin/bash" || v.Shell == "/sbin/nologin")
}
func validAdminAction(action string) bool {
	return slices.Contains([]string{"create", "lock", "unlock"}, action)
}
func adminDenied(code, message string) AdminResult { return AdminResult{Code: code, Message: message} }
func (e *Engine) targetAllowed(s AccountState) bool {
	return s.User.Source == "local" && localName.MatchString(s.User.Name) && s.User.UID >= e.Policy.MinUID && s.User.UID <= e.Policy.MaxUID && s.User.GID != 0 && s.User.UID != e.Policy.CallerUID && !s.Privileged && !slices.Contains(e.Policy.ProtectedUsers, s.User.Name) && s.User.Name != "root"
}
func (e *Engine) pending(name string) bool {
	op, err := e.Store.Target(name)
	if errors.Is(err, ErrAbsent) {
		return false
	}
	if err != nil {
		return true
	}
	value, err := e.Store.Operation(op)
	return err != nil || value.Result.Status != "confirmed" && value.Result.Status != "failed"
}

var ErrAbsent = errors.New("host record absent")

func (e *Engine) Prepare(ctx context.Context, r AdminRequest) AdminResult {
	result := AdminResult{MinUID: e.Policy.MinUID, MaxUID: e.Policy.MaxUID}
	if !e.Policy.Enabled {
		return adminDenied("disabled", "系统管理员尚未启用本地账户管理。")
	}
	if r.Mode == "capabilities" {
		result.Allowed = true
		return result
	}
	if !validAdminAction(r.Action) || !localName.MatchString(r.Spec.Name) {
		return adminDenied("invalid_request", "本地账户或动作不合法。")
	}
	if e.pending(r.Spec.Name) {
		return adminDenied("pending", "该账户存在未核实操作，请先查询操作结果。")
	}
	if r.Action == "create" {
		if !ValidCreate(r.Spec) || r.Spec.UID < e.Policy.MinUID || r.Spec.UID > e.Policy.MaxUID || slices.Contains(e.Policy.ProtectedUsers, r.Spec.Name) {
			return adminDenied("forbidden", "新账户参数不符合主机管理策略。")
		}
		if err := e.Host.CheckCreate(ctx, r.Spec); err != nil {
			return adminDenied("conflict", "用户名、UID、同名组或主目录已存在，或无法完成唯一性检查。")
		}
		result.Allowed = true
		result.Fingerprint = Hash(r.Spec)
		result.Message = "创建同名主组与 0700 主目录；账户初始到期锁定且未设置可用密码。"
		return result
	}
	state, err := e.Host.Inspect(ctx, r.Spec.Name)
	if err != nil {
		return adminDenied("unavailable", "无法核实本地账户；不修改目录或未知来源账户。")
	}
	result.State = &state
	if !e.targetAllowed(state) {
		return adminDenied("forbidden", "仅允许管理策略范围内的普通本地账户，受保护账户不可操作。")
	}
	lock, lockErr := e.Store.Lock(r.Spec.Name)
	if lockErr != nil && !errors.Is(lockErr, ErrAbsent) {
		return adminDenied("unavailable", "无法读取主机锁定记录。")
	}
	owned := lockErr == nil && lock.Identity == state.Identity && state.Expiry == 1
	result.LockedByPlatform = owned
	if r.Action == "lock" && state.Expiry >= 0 && state.Expiry <= time.Now().UTC().Unix()/86400 {
		return adminDenied("conflict", "账户已经到期；不能覆盖现有到期策略。")
	}
	if r.Action == "unlock" && (!owned || lock.OriginalExpiry >= 0 && lock.OriginalExpiry <= time.Now().UTC().Unix()/86400) {
		return adminDenied("forbidden", "只能恢复平台锁定且原到期设置仍有效的账户；请由系统管理员核对。")
	}
	result.Allowed = true
	result.Fingerprint = Hash(state)
	result.Message = "账户到期锁定影响后续执行账户校验的登录；现有会话与作业保持原状。"
	return result
}
func (e *Engine) Execute(ctx context.Context, r AdminRequest) AdminResult {
	if !operationName.MatchString(r.OperationID) || r.Mode != "execute" {
		return adminDenied("invalid_request", "操作编号不合法。")
	}
	hash := Hash(r)
	if old, err := e.Store.Operation(r.OperationID); err == nil {
		if old.Hash != hash {
			return adminDenied("conflict", "操作编号不能复用为不同请求。")
		}
		return e.Status(ctx, r.OperationID)
	} else if !errors.Is(err, ErrAbsent) {
		return adminDenied("unavailable", "无法读取主机操作记录。")
	}
	prepared := e.Prepare(ctx, r)
	if !prepared.Allowed {
		return prepared
	}
	if prepared.Fingerprint != r.Expected {
		return adminDenied("changed", "账户身份、到期设置或创建条件已变化。")
	}
	op := HostOperation{Request: r, Hash: hash, Before: prepared.State, Result: AdminResult{Status: "unknown", OperationID: r.OperationID, Message: "主机执行结果待核实，请勿重复提交。"}}
	if e.Store.SaveOperation(r.OperationID, op) != nil || e.Store.SaveTarget(r.Spec.Name, r.OperationID) != nil {
		return adminDenied("unavailable", "无法保存主机执行意图，未修改账户。")
	}
	var err error
	switch r.Action {
	case "create":
		err = e.Host.Create(ctx, r.Spec)
	case "lock":
		if err = e.Store.SaveLock(r.Spec.Name, LockState{Identity: prepared.State.Identity, OriginalExpiry: prepared.State.Expiry}); err == nil {
			err = e.Host.SetExpiry(ctx, r.Spec.Name, 1)
		}
	case "unlock":
		var lock LockState
		lock, err = e.Store.Lock(r.Spec.Name)
		if err == nil {
			err = e.Host.SetExpiry(ctx, r.Spec.Name, lock.OriginalExpiry)
		}
	}
	// A failed command may already have changed passwd/group/shadow or the home.
	// Persist uncertainty and reconcile only by reading; never retry a command.
	if err != nil {
		return op.Result
	}
	return e.Status(ctx, r.OperationID)
}
func (e *Engine) Status(ctx context.Context, id string) AdminResult {
	if !operationName.MatchString(id) {
		return adminDenied("invalid_request", "操作编号不合法。")
	}
	op, err := e.Store.Operation(id)
	if err != nil {
		return adminDenied("not_found", "主机操作记录不存在。")
	}
	if op.Result.Status == "confirmed" || op.Result.Status == "failed" {
		return op.Result
	}
	current, err := e.Host.Inspect(ctx, op.Request.Spec.Name)
	if err != nil {
		return op.Result
	}
	reached := false
	switch op.Request.Action {
	case "create":
		v := op.Request.Spec
		reached = current.User.Name == v.Name && current.User.UID == v.UID && current.User.Home == "/home/"+v.Name && current.User.Shell == v.Shell && current.User.Comment == v.Comment && current.Expiry == 1 && current.PrivateGroup && current.HomeReady && e.targetAllowed(current)
		if reached {
			if err = e.Store.SaveLock(v.Name, LockState{Identity: current.Identity, OriginalExpiry: -1}); err != nil {
				return op.Result
			}
		}
	case "lock":
		reached = op.Before != nil && current.Identity == op.Before.Identity && current.Expiry == 1
	case "unlock":
		lock, err := e.Store.Lock(current.User.Name)
		reached = err == nil && op.Before != nil && current.Identity == op.Before.Identity && current.Expiry == lock.OriginalExpiry
	}
	if !reached {
		return op.Result
	}
	op.Result = AdminResult{Status: "confirmed", OperationID: id, Message: "已读取主机账户并核实操作结果。", State: &current}
	if e.Store.SaveOperation(id, op) != nil {
		return AdminResult{Status: "unknown", OperationID: id, Message: "主机结果尚未持久化，请继续核实。"}
	}
	return op.Result
}
