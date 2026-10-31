// Package bulkcontrol coordinates explicit targets through the existing audited
// single-resource services. It never invents a Slurm batch selector or retries writes.
package bulkcontrol

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/jobcontrol"
	"slurm-console/backend/internal/nodecontrol"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
)

const MaxTargets = 10

type Item struct {
	Action      string       `json:"action"`
	Target      string       `json:"target"`
	OperationID string       `json:"operationId"`
	Allowed     bool         `json:"allowed"`
	Status      string       `json:"status"`
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	States      []string     `json:"states"`
	Job         *models.Job  `json:"job"`
	Node        *models.Node `json:"node"`
}
type View struct {
	ID        string         `json:"id"`
	Action    string         `json:"action"`
	Status    string         `json:"status"`
	Allowed   bool           `json:"allowed"`
	ExpiresAt string         `json:"expiresAt"`
	Items     []Item         `json:"items"`
	Counts    map[string]int `json:"counts"`
}
type Input struct {
	Confirmation string `json:"confirmation"`
	Reason       string `json:"reason"`
}
type group struct {
	size                       int
	actor, cluster, action, id string
	revision                   int
	expires, retain            time.Time
	started                    bool
	input                      Input
	status                     string
	items                      []Item
	proofs                     []string
}
type Service struct {
	bytes    int
	app      core.App
	ctx      context.Context
	mu       sync.Mutex
	groups   map[string]*group
	requests chan struct{}
	workers  chan struct{}
}

var initialize sync.Mutex

func For(app core.App) *Service {
	initialize.Lock()
	defer initialize.Unlock()
	if s, ok := app.Store().Get("platform.bulkControl").(*Service); ok {
		return s
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{app: app, ctx: ctx, groups: map[string]*group{}, requests: make(chan struct{}, 2), workers: make(chan struct{}, 3)}
	app.Store().Set("platform.bulkControl", s)
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error { cancel(); return e.Next() })
	return s
}
func ValidAction(action string) bool {
	return action == "jobs.cancel" || action == "nodes.drain" || action == "nodes.resume"
}
func ValidID(id string) bool {
	if len(id) < 20 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func ValidTargets(action string, targets []string) bool {
	if !ValidAction(action) || len(targets) < 1 || len(targets) > MaxTargets {
		return false
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if seen[target] {
			return false
		}
		seen[target] = true
		if action == "jobs.cancel" {
			if !client.ValidJobID(target) {
				return false
			}
		} else if !client.ValidNodeTarget(target) {
			return false
		}
	}
	return true
}
func Confirmation(action string, n int) string {
	prefix := strings.ToUpper(strings.Split(action, ".")[1])
	return prefix + " " + strconv.Itoa(n)
}

func (s *Service) allowed(actor, cluster, action string, write bool) error {
	user, err := s.app.FindRecordById("users", actor)
	if err != nil || !user.GetBool("active") {
		return client.Failure("operation_forbidden", "平台用户已停用或不存在。")
	}
	access, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return err
	}
	read := strings.Split(action, ".")[0] + ".read"
	if !access.Allows(read, cluster) || write && !access.Allows(action, cluster) {
		return client.Failure("operation_forbidden", "未授予该集群的读取与控制权限。")
	}
	return nil
}
func failure(target, id string, err error) Item {
	message := "请求未完成，请核对操作记录，勿自动重发。"
	code := "unavailable"
	var public *client.Error
	if errors.As(err, &public) {
		message = public.Message
		code = public.Code
	}
	return Item{Target: target, OperationID: id, Status: "failed", Code: code, Message: message, States: []string{}}
}
func (s *Service) enter() error {
	select {
	case s.requests <- struct{}{}:
		return nil
	default:
		return client.Failure("busy", "批量请求繁忙，请稍后重试。")
	}
}
func (s *Service) pool(ctx context.Context, n int, run func(int)) {
	var next int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range min(3, n) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= n {
					return
				}
				select {
				case s.workers <- struct{}{}:
				case <-ctx.Done():
					return
				}
				if ctx.Err() != nil {
					<-s.workers
					return
				}
				run(i)
				<-s.workers
			}
		}()
	}
	wg.Wait()
}
func (s *Service) Prepare(ctx context.Context, actor, cluster, action string, targets []string) (View, error) {
	if !ValidTargets(action, targets) {
		return View{}, client.Failure("invalid_request", "每批须提供 1–10 个不重复的明确目标；不支持范围或通配选择。")
	}
	if err := s.allowed(actor, cluster, action, true); err != nil {
		return View{}, err
	}
	if err := s.enter(); err != nil {
		return View{}, err
	}
	defer func() { <-s.requests }()
	connection, err := services.ConnectionFor(s.app, cluster)
	if err != nil {
		return View{}, err
	}
	g := &group{actor: actor, cluster: cluster, action: action, revision: connection.Revision, status: "ready", expires: time.Now().Add(2 * time.Minute), items: make([]Item, len(targets)), proofs: make([]string, len(targets))}
	s.pool(ctx, len(targets), func(i int) {
		target := targets[i]
		var item Item
		var proof string
		if action == "jobs.cancel" {
			p, err := jobcontrol.For(s.app).Prepare(ctx, actor, cluster, target)
			if err != nil {
				g.items[i] = failure(target, "", err)
				return
			}
			item = Item{Target: target, OperationID: p.OperationID, Allowed: p.Allowed, Status: "ready", Message: p.Message, States: p.Job.States, Job: &p.Job}
			proof = p.Proof
		} else {
			p, err := nodecontrol.For(s.app).Prepare(ctx, actor, cluster, target, strings.TrimPrefix(action, "nodes."))
			if err != nil {
				g.items[i] = failure(target, "", err)
				return
			}
			item = Item{Target: target, OperationID: p.OperationID, Allowed: p.Allowed, Status: "ready", Message: p.Message, States: p.Node.States, Node: &p.Node}
			proof = p.Proof
		}
		if !item.Allowed {
			item.Status = "failed"
			item.Code = "operation_forbidden"
		}
		b, _ := json.Marshal(item)
		if len(b) > 65536 || len(proof) > 8192 {
			g.items[i] = failure(target, "", client.Failure("invalid_response", "目标确认信息超过容量限制。"))
			return
		}
		g.items[i] = item
		g.proofs[i] = proof
	})
	if ctx.Err() != nil {
		return View{}, ctx.Err()
	}
	if err := s.allowed(actor, cluster, action, true); err != nil {
		return View{}, err
	}
	current, err := services.ConnectionFor(s.app, cluster)
	if err != nil {
		return View{}, err
	}
	if current.Revision != g.revision {
		return View{}, client.Failure("configuration_changed", "连接配置已变化，请重新校验全部目标。")
	}
	for _, item := range g.items {
		if !item.Allowed {
			g.status = "blocked"
		}
	}
	if g.status == "blocked" {
		return view(g), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, old := range s.groups {
		if !old.started && now.After(old.expires) || old.status == "completed" && now.After(old.retain) {
			s.bytes -= old.size
			delete(s.groups, id)
		}
	}
	raw, _ := json.Marshal(view(g))
	g.size = len(raw)
	for _, proof := range g.proofs {
		g.size += len(proof)
	}
	if len(s.groups) >= 64 || s.bytes+g.size > 8<<20 {
		return View{}, client.Failure("busy", "批量确认容量已满，请稍后重试。")
	}
	g.id = rand.Text()
	s.groups[g.id] = g
	s.bytes += g.size
	return view(g), nil
}
func view(g *group) View {
	v := View{ID: g.id, Action: g.action, Status: g.status, Allowed: g.status == "ready" && time.Now().Before(g.expires), ExpiresAt: g.expires.UTC().Format(time.RFC3339), Items: make([]Item, len(g.items)), Counts: map[string]int{"ready": 0, "processing": 0, "confirmed": 0, "accepted": 0, "failed": 0, "unknown": 0}}
	// States are copied; preparation models are immutable after creation.
	for i, item := range g.items {
		if item.Action == "" {
			item.Action = g.action
		}
		item.States = append([]string{}, item.States...)
		v.Items[i] = item
		v.Counts[item.Status]++
	}
	return v
}
func validInput(action string, n int, in Input) bool {
	return in.Confirmation == Confirmation(action, n) && utf8.RuneCountInString(in.Reason) >= 2 && utf8.RuneCountInString(in.Reason) <= 300 && !strings.ContainsAny(in.Reason, "\r\n\x00")
}
func (s *Service) lookup(id string) *group {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[id]
	if g != nil && ((!g.started && time.Now().After(g.expires)) || (g.status == "completed" && time.Now().After(g.retain))) {
		delete(s.groups, id)
		s.bytes -= g.size
		return nil
	}
	return g
}
func (s *Service) Execute(actor, cluster, id, requestID string, input Input) (View, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	g := s.lookup(id)
	if g == nil {
		return View{}, client.Failure("confirmation_invalid", "批量确认不存在、已过期或服务已重启；先读取已保存的逐项操作记录。")
	}
	if g.actor != actor || g.cluster != cluster {
		return View{}, client.Failure("operation_forbidden", "无权执行该批量确认。")
	}
	if err := s.allowed(actor, cluster, g.action, true); err != nil {
		return View{}, err
	}
	if !validInput(g.action, len(g.items), input) {
		return View{}, client.Failure("invalid_request", "请输入完整批量确认短语和 2–300 字单行原因。")
	}
	s.mu.Lock()
	if g.started {
		if g.input != input {
			s.mu.Unlock()
			return View{}, client.Failure("confirmation_invalid", "同一批量确认的原因与内容不能改变。")
		}
		v := view(g)
		s.mu.Unlock()
		return v, nil
	}
	if !time.Now().Before(g.expires) {
		s.mu.Unlock()
		return View{}, client.Failure("confirmation_expired", "批量确认已过期，请重新校验。")
	}
	s.mu.Unlock()
	if err := s.enter(); err != nil {
		return View{}, err
	}
	current, err := services.ConnectionFor(s.app, cluster)
	if err != nil {
		<-s.requests
		return View{}, err
	}
	if current.Revision != g.revision {
		<-s.requests
		return View{}, client.Failure("configuration_changed", "连接配置已变化，未开始批量执行。")
	}
	s.mu.Lock()
	if s.groups[id] != g || !g.started && !time.Now().Before(g.expires) {
		s.mu.Unlock()
		<-s.requests
		return View{}, client.Failure("confirmation_expired", "批量确认已过期，请重新校验。")
	}
	if g.started {
		v := view(g)
		same := g.input == input
		s.mu.Unlock()
		<-s.requests
		if !same {
			return View{}, client.Failure("confirmation_invalid", "批量确认内容不能变更。")
		}
		return v, nil
	}
	g.started = true
	g.input = input
	g.status = "processing"
	for i := range g.items {
		g.items[i].Status = "processing"
		g.items[i].Message = "已接受批量执行，等待逐项复查与审计。"
	}
	v := view(g)
	s.mu.Unlock()
	go s.run(g, requestID)
	return v, nil
}
func (s *Service) run(g *group, requestID string) {
	defer func() { <-s.requests }()
	s.pool(s.ctx, len(g.items), func(i int) {
		s.mu.Lock()
		item := g.items[i]
		s.mu.Unlock()
		// Queueing never freezes permission, account activity or confirmation validity.
		err := s.allowed(g.actor, g.cluster, g.action, true)
		var result Item
		if err != nil {
			result = failure(item.Target, item.OperationID, err)
		} else if g.action == "jobs.cancel" {
			r, e := jobcontrol.For(s.app).Cancel(g.actor, g.cluster, item.Target, requestID, jobcontrol.Input{Proof: g.proofs[i], Confirmation: item.Target, Reason: g.input.Reason})
			if e != nil {
				result = executionFailure(item.Target, item.OperationID, e)
			} else {
				result = Item{Target: item.Target, OperationID: r.OperationID, Status: r.Status, Message: r.Message, States: r.States}
			}
		} else {
			r, e := nodecontrol.For(s.app).Execute(g.actor, g.cluster, item.Target, strings.TrimPrefix(g.action, "nodes."), requestID, nodecontrol.Input{Proof: g.proofs[i], Confirmation: item.Target, Reason: g.input.Reason})
			if e != nil {
				result = executionFailure(item.Target, item.OperationID, e)
			} else {
				result = Item{Target: item.Target, OperationID: r.OperationID, Status: r.Status, Message: r.Message, States: r.States}
			}
		}
		s.mu.Lock()
		g.items[i] = result
		s.mu.Unlock()
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range g.items {
		if g.items[i].Status == "processing" {
			g.items[i].Status = "unknown"
			g.items[i].Message = "应用停止，无法证明此项是否已发送；请核对操作记录。"
		}
	}
	clear(g.proofs)
	g.status = "completed"
	g.retain = time.Now().Add(30 * time.Minute)
}
func (s *Service) Get(actor, cluster, id string) (View, error) {
	g := s.lookup(id)
	if g == nil {
		return View{}, client.Failure("not_found", "批量上下文不在内存中，请使用逐项操作编号只读核实。")
	}
	if g.actor != actor || g.cluster != cluster {
		return View{}, client.Failure("operation_forbidden", "无权读取该批量上下文。")
	}
	if err := s.allowed(actor, cluster, g.action, false); err != nil {
		return View{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return view(g), nil
}
func (s *Service) Operations(ctx context.Context, actor, cluster, resource string, ids []string) (View, error) {
	action := "jobs.cancel"
	if resource == "node" {
		action = "nodes.drain"
	} else if resource != "job" {
		return View{}, client.Failure("invalid_request", "资源类型不合法。")
	}
	if len(ids) < 1 || len(ids) > MaxTargets {
		return View{}, client.Failure("invalid_request", "操作编号须为 1–10 个。")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !ValidID(id) || seen[id] {
			return View{}, client.Failure("invalid_request", "操作编号不合法或重复。")
		}
		seen[id] = true
	}
	if err := s.allowed(actor, cluster, action, false); err != nil {
		return View{}, err
	}
	if err := s.enter(); err != nil {
		return View{}, err
	}
	defer func() { <-s.requests }()
	g := &group{action: action, status: "completed", items: make([]Item, len(ids))}
	s.pool(ctx, len(ids), func(i int) {
		var item Item
		if resource == "job" {
			r, e := jobcontrol.For(s.app).Get(ctx, actor, cluster, ids[i])
			if e != nil {
				item = failure("", ids[i], e)
				item.Status = "unknown"
			} else {
				item = Item{Target: r.JobID, OperationID: r.OperationID, Status: r.Status, Message: r.Message, States: r.States}
			}
		} else {
			r, e := nodecontrol.For(s.app).Get(ctx, actor, cluster, ids[i])
			if e != nil {
				item = failure("", ids[i], e)
				item.Status = "unknown"
			} else {
				item = Item{Action: "nodes." + r.Action, Target: r.NodeName, OperationID: r.OperationID, Status: r.Status, Message: r.Message, States: r.States}
			}
		}
		if item.Code != "" {
			item.Message = "无法读取该操作记录；可能未送达、被拒绝或结果待核实。核对资源状态，勿自动重发。"
		}
		g.items[i] = item
	})
	if ctx.Err() != nil {
		return View{}, ctx.Err()
	}
	if err := s.allowed(actor, cluster, action, false); err != nil {
		return View{}, err
	}
	if resource == "node" {
		g.action = "nodes.control"
	}
	return view(g), nil
}

func executionFailure(target, id string, err error) Item {
	item := failure(target, id, err)
	if !slices.Contains([]string{"invalid_request", "confirmation_invalid", "confirmation_expired", "confirmation_changed", "operation_forbidden", "configuration_changed", "disabled", "busy", "not_found", "auth_failed"}, item.Code) {
		item.Status = "unknown"
	}
	return item
}
