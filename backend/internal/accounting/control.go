// Package accounting manages explicit, cluster-scoped SlurmDB associations and
// separately authorized global accounting identity metadata.
package accounting

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
	"slurm-console/backend/internal/slurm/adapter"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
)

type Spec struct {
	Reservation   *client.ReservationSpec `json:"reservation,omitempty"`
	Account       string                  `json:"account"`
	User          string                  `json:"user"`
	Partition     string                  `json:"partition"`
	AssociationID string                  `json:"associationId"`
	Description   string                  `json:"description"`
	Organization  string                  `json:"organization"`
	QOS           *client.QOSSpec         `json:"qos,omitempty"`
	TRES          *client.TRESSpec        `json:"tres,omitempty"`
}
type Prepared struct {
	Allowed      bool   `json:"allowed"`
	Message      string `json:"message"`
	Action       string `json:"action"`
	Spec         Spec   `json:"spec"`
	SlurmCluster string `json:"slurmCluster"`
	Global       bool   `json:"global"`
	Confirmation string `json:"confirmation"`
	Proof        string `json:"proof"`
	OperationID  string `json:"operationId"`
	ExpiresAt    string `json:"expiresAt"`
}
type Input struct {
	Proof        string `json:"proof"`
	Confirmation string `json:"confirmation"`
	Reason       string `json:"reason"`
}
type Result struct {
	OperationID  string `json:"operationId"`
	Action       string `json:"action"`
	Spec         Spec   `json:"spec"`
	SlurmCluster string `json:"slurmCluster"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	Reason       string `json:"reason"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}
type intent struct {
	Actor, Cluster, Action, Operation, State string
	Mutation                                 client.AccountingMutation
	Revision                                 int
	Expires                                  int64
}
type stored struct{ Mutation client.AccountingMutation }
type Service struct {
	app      core.App
	key      [32]byte
	ctx      context.Context
	slots    chan struct{}
	mutation sync.Mutex
	running  sync.Map
}

var initMu sync.Mutex

func For(app core.App) *Service {
	initMu.Lock()
	defer initMu.Unlock()
	if s, ok := app.Store().Get("platform.accounting").(*Service); ok {
		return s
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{app: app, ctx: ctx, slots: make(chan struct{}, 4)}
	_, _ = rand.Read(s.key[:])
	app.Store().Set("platform.accounting", s)
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error { cancel(); return e.Next() })
	return s
}
func digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func mutation(action, cluster string, v Spec) client.AccountingMutation {
	return client.AccountingMutation{Action: action, Cluster: cluster, Account: v.Account, User: v.User, Partition: v.Partition, AssociationID: v.AssociationID, Description: v.Description, Organization: v.Organization, QOS: v.QOS, TRES: v.TRES, Reservation: v.Reservation}
}
func spec(m client.AccountingMutation) Spec {
	return Spec{Account: m.Account, User: m.User, Partition: m.Partition, AssociationID: m.AssociationID, Description: m.Description, Organization: m.Organization, QOS: m.QOS, TRES: m.TRES, Reservation: m.Reservation}
}
func Action(action string) string {
	return map[string]string{"reservation_create": "reservations.create", "reservation_update": "reservations.update", "reservation_delete": "reservations.delete", "account_create": "accounts.create", "account_update": "accounts.update", "user_create": "slurm_users.create", "association_create": "associations.create", "association_remove": "associations.remove", "qos_create": "qos.create", "qos_update": "qos.update", "qos_tres_update": "qos.tres.update", "association_tres_update": "associations.tres.update"}[action]
}
func global(action string) bool {
	return slices.Contains([]string{"account_create", "account_update", "user_create", "qos_create", "qos_update", "qos_tres_update"}, action)
}
func confirmation(m client.AccountingMutation) string {
	if client.IsReservation(m.Action) && m.Reservation != nil {
		return m.Reservation.Name
	}
	if isTRES(m.Action) && m.TRES != nil {
		if m.TRES.Kind == "qos" {
			return m.TRES.Name
		}
		return m.TRES.TargetID
	}
	if isQOS(m.Action) && m.QOS != nil {
		return m.QOS.Name
	}
	if m.Action == "account_create" || m.Action == "account_update" {
		return m.Account
	}
	if m.Action == "user_create" {
		return m.User
	}
	if m.Action == "association_remove" {
		return m.AssociationID
	}
	return m.User + "@" + m.Account
}
func target(m client.AccountingMutation) (string, string) {
	if client.IsReservation(m.Action) && m.Reservation != nil {
		return "reservation", m.Cluster + "/" + m.Reservation.Name
	}
	if isTRES(m.Action) && m.TRES != nil {
		v := m.TRES
		if v.Kind == "qos" {
			return "slurm_qos", v.Name
		}
		return "association", m.Cluster + "/" + v.Account + "/" + v.User + "/" + v.Partition
	}
	if isQOS(m.Action) && m.QOS != nil {
		return "slurm_qos", m.QOS.Name
	}
	switch m.Action {
	case "account_create", "account_update":
		return "slurm_account", m.Account
	case "user_create":
		return "slurm_user", m.User
	default:
		return "association", m.Cluster + "/" + m.Account + "/" + m.User + "/" + m.Partition
	}
}
func required(action string) []string {
	switch action {
	case "reservation_create":
		return []string{"reservations.read", "reservations.create", "nodes.read"}
	case "reservation_update":
		return []string{"reservations.read", "reservations.update", "jobs.read"}
	case "reservation_delete":
		return []string{"reservations.read", "reservations.delete", "jobs.read"}
	case "qos_tres_update":
		return []string{"tres.read", "tres.update", "qos.read", "qos.update"}
	case "association_tres_update":
		return []string{"tres.read", "tres.update", "associations.read", "associations.update"}
	case "qos_create", "qos_update":
		return []string{"qos.read", "qos.update"}
	case "account_create", "account_update":
		return []string{"accounts.read", "accounts.update"}
	case "user_create":
		return []string{"slurm_users.read", "accounts.read", "associations.read", "associations.update"}
	case "association_create":
		return []string{"associations.read", "associations.update", "accounts.read", "slurm_users.read"}
	case "association_remove":
		return []string{"associations.read", "associations.update", "jobs.read"}
	}
	return nil
}
func (s *Service) authorized(actor, cluster, action string) error {
	actorRecord, err := s.app.FindRecordById("users", actor)
	if err != nil || !actorRecord.GetBool("active") {
		return client.Failure("operation_forbidden", "平台身份已停用或不可用。")
	}
	access, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return err
	}
	if Action(action) == "" {
		return client.Failure("invalid_request", "不支持的操作。")
	}
	for _, p := range required(action) {
		if !access.Allows(p, cluster) {
			return client.Failure("operation_forbidden", "未授予此集群的操作权限。")
		}
	}
	if (isQOS(action) || action == "qos_tres_update") && (!access.Allows("platform.qos.manage", "") || !access.Allows("platform.qos.read", "")) {
		return client.Failure("operation_forbidden", "共享 QOS 变更需要平台 QOS 读取及管理权限。")
	}
	if global(action) && !isQOS(action) && action != "qos_tres_update" && !access.Allows("platform.accounting.manage", "") {
		return client.Failure("operation_forbidden", "全局记账身份变更需要独立的平台记账管理权限。")
	}
	return nil
}
func findAccount(s adapter.AccountingSnapshot, name string) *models.Account {
	for _, v := range s.Accounts {
		if v.Name == name {
			return &v
		}
	}
	return nil
}
func findUser(s adapter.AccountingSnapshot, name string) *models.SlurmUser {
	for _, v := range s.Users {
		if v.Name == name {
			return &v
		}
	}
	return nil
}
func matching(s adapter.AccountingSnapshot, m client.AccountingMutation) *models.Association {
	for _, v := range s.Associations {
		if m.Action == "association_remove" {
			if v.ID == m.AssociationID {
				return &v
			}
		} else if v.Account == m.Account && v.User == m.User && v.Partition == m.Partition {
			return &v
		}
	}
	return nil
}
func parent(s adapter.AccountingSnapshot, account string) *models.Association {
	for _, v := range s.Associations {
		if v.Account == account && v.User == "" && v.Partition == "" {
			return &v
		}
	}
	return nil
}
func state(s adapter.AccountingSnapshot, m client.AccountingMutation) string {
	if client.IsReservation(m.Action) {
		return reservationState(s, m)
	}
	if isTRES(m.Action) {
		t := findTRESTarget(s, m)
		if t == nil {
			return digest([]any{s.Cluster, nil, s.TRES.Resources})
		}
		return digest([]any{s.Cluster, t.Fingerprint, s.TRES.Resources})
	}
	if isQOS(m.Action) {
		q := findQOS(s, m.QOS.Name)
		fingerprint := ""
		if q != nil {
			fingerprint = q.Fingerprint
		}
		return digest([]any{s.Cluster, q, fingerprint})
	}
	a, u, assoc := findAccount(s, m.Account), findUser(s, m.User), matching(s, m)
	if m.Action == "account_create" {
		return digest([]any{s.Cluster, a, parent(s, "root")})
	}
	if m.Action == "account_update" {
		return digest([]any{s.Cluster, a, parent(s, m.Account)})
	}
	return digest([]any{s.Cluster, a, u, assoc, parent(s, m.Account)})
}
func protected(name string) bool {
	return slices.Contains([]string{"root", "slurm-console", "slurm", "munge", "nobody"}, strings.ToLower(name))
}
func policy(s adapter.AccountingSnapshot, m client.AccountingMutation) string {
	if client.IsReservation(m.Action) {
		return reservationPolicy(s, m)
	}
	if isTRES(m.Action) {
		return tresPolicy(s, m)
	}
	if isQOS(m.Action) {
		return qosPolicy(s, m)
	}
	if !m.Valid() || s.Cluster != m.Cluster || protected(m.Account) || m.User != "" && protected(m.User) {
		return "目标名称、字段或集群不符合操作策略。"
	}
	a, u := findAccount(s, m.Account), findUser(s, m.User)
	switch m.Action {
	case "account_create":
		if a != nil {
			return "Account 名称已存在于记账库，请选用新名称；不会接管现有账户。"
		}
		if parent(s, "root") == nil {
			return "当前集群缺少 root Account 关联。"
		}
	case "account_update":
		if a == nil || parent(s, m.Account) == nil {
			return "Account 未关联当前集群。"
		}
		if a.Description == m.Description && a.Organization == m.Organization {
			return "描述和组织均未变化。"
		}
	case "user_create", "association_create":
		if a == nil || parent(s, m.Account) == nil {
			return "目标 Account 尚未关联当前集群。"
		}
		if m.Action == "user_create" && u != nil {
			return "Slurm 用户已存在，请使用创建 Association。"
		}
		if m.Action == "association_create" && u == nil {
			return "Slurm 用户不存在，请先由平台记账管理员创建 Slurm 用户。"
		}
		if matching(s, m) != nil {
			return "该集群、Account、用户和分区的 Association 已存在。"
		}
	case "association_remove":
		v := matching(s, m)
		if v == nil || v.Account != m.Account || v.User != m.User || v.Partition != m.Partition {
			return "Association 身份已变化或不属于所选集群。"
		}
		if v.User == "" || v.IsDefault || u == nil || u.DefaultAccount == v.Account {
			return "不能移除 Account 层级、默认账户或默认 Association。"
		}
		if v.Partition == "" {
			for _, other := range s.Associations {
				if other.Account == v.Account && other.User == v.User && other.Partition != "" {
					return "请先逐一移除此用户在该 Account 的分区关联；通用关联可能包含子关联。"
				}
			}
		}
		count := 0
		for _, assoc := range s.Associations {
			if assoc.User == m.User {
				count++
			}
		}
		if count < 2 {
			return "不能移除用户在当前集群的最后一个 Association。"
		}
	}
	return ""
}
func (s *Service) pending(m client.AccountingMutation) error {
	kind, id := target(m)
	count, err := s.app.CountRecords("operation_audits", dbx.And(dbx.HashExp{"target_type": kind, "target_id": id}, dbx.In("status", "processing", "accepted", "unknown")))
	if err != nil {
		return err
	}
	if count > 0 {
		return client.Failure("operation_forbidden", "此目标已有操作待核实，请先查询已有操作记录。")
	}
	return nil
}
func (s *Service) checkLive(ctx context.Context, c services.Connection, snapshot adapter.AccountingSnapshot, m client.AccountingMutation) error {
	if client.IsReservation(m.Action) {
		return checkReservationJobs(ctx, c, m)
	}
	if m.Partition != "" && m.Action == "association_create" {
		values, err := c.Adapter.GetPartitions(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, p := range values {
			if p.Name == m.Partition {
				found = true
			}
		}
		if !found {
			return client.Failure("operation_forbidden", "目标分区不存在于当前集群。")
		}
	}
	if m.Action != "association_remove" {
		return nil
	}
	jobs, err := c.Adapter.GetJobs(ctx)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		terminal := len(j.States) == 1 && slices.ContainsFunc(j.States, func(v string) bool {
			return slices.Contains([]string{"COMPLETED", "CANCELLED", "FAILED", "TIMEOUT", "NODE_FAIL", "OUT_OF_MEMORY", "PREEMPTED", "BOOT_FAIL", "DEADLINE", "REVOKED"}, v)
		})
		if j.User == m.User && j.Account == m.Account && !terminal {
			return client.Failure("operation_forbidden", "用户在该 Account 仍有未结束作业，不能移除关联。")
		}
	}
	return nil
}
func (s *Service) Prepare(ctx context.Context, actor, cluster, action string, v Spec) (Prepared, error) {
	result := Prepared{Action: action, Spec: v, Global: global(action)}
	if err := s.authorized(actor, cluster, action); err != nil {
		return result, err
	}
	if !mutation(action, "validation", v).Valid() {
		return result, client.Failure("invalid_request", "操作参数不合法。")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return result, client.Failure("busy", "记账校验繁忙，请稍后重试。")
	}
	c, err := services.ConnectionFor(s.app, cluster)
	if err != nil {
		return result, err
	}
	snapshot, err := readSnapshot(ctx, c.Adapter, action)
	if err != nil {
		return result, err
	}
	if err := s.authorized(actor, cluster, action); err != nil {
		return result, err
	}
	m := preserveQOS(snapshot, mutation(action, snapshot.Cluster, v))
	if client.IsReservation(action) {
		if r := findReservation(snapshot, m); r != nil {
			m.ReservationUnmanagedFingerprint = adapter.ReservationDigest(r.Raw, true)
		}
	}
	if isTRES(action) {
		m = materializeTRES(snapshot, m)
	}
	result.SlurmCluster = m.Cluster
	result.Confirmation = confirmation(m)
	if denied := policy(snapshot, m); denied != "" {
		result.Message = denied
		return result, nil
	}
	if err := s.pending(m); err != nil {
		result.Message = err.Error()
		return result, nil
	}
	if err := s.checkLive(ctx, c, snapshot, m); err != nil {
		return result, err
	}
	if err := s.authorized(actor, cluster, action); err != nil {
		return result, err
	}
	current, err := services.ConnectionFor(s.app, cluster)
	if err != nil || current.Revision != c.Revision {
		return result, client.Failure("configuration_changed", "连接配置已变化，请重新校验。")
	}
	p := intent{Actor: actor, Cluster: cluster, Action: action, Operation: rand.Text(), State: state(snapshot, m), Mutation: m, Revision: c.Revision, Expires: time.Now().Add(2 * time.Minute).Unix()}
	raw, _ := json.Marshal(p)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write([]byte(encoded))
	result.Proof = encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(result.Proof) > 12000 {
		return result, client.Failure("invalid_request", "本次确认保留数据超过安全上限，请管理员维护。")
	}
	result.Allowed = true
	result.OperationID = p.Operation
	result.ExpiresAt = time.Unix(p.Expires, 0).UTC().Format(time.RFC3339)
	result.Message = "已读取最新目标配置，请核对影响范围、填写原因并确认。"
	if isQOS(action) {
		result.Message = "已读取共享 QOS 最新定义，请核对全部可编辑字段及跨集群影响，填写原因并确认。"
	}
	if isTRES(action) {
		result.Message = "已校验资源目录、完整对象及未编辑字段；请核对本层限额、影响范围、填写原因并确认。"
	}
	return result, nil
}
func (s *Service) parse(raw, actor, cluster string) (intent, error) {
	p := intent{}
	failure := client.Failure("confirmation_invalid", "记账确认无效或服务已重启，请重新校验。")
	parts := strings.Split(raw, ".")
	if len(raw) > 12288 || len(parts) != 2 {
		return p, failure
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return p, failure
	}
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return p, failure
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(data, &p) != nil || p.Actor != actor || p.Cluster != cluster || !p.Mutation.Valid() || p.Action != p.Mutation.Action || len(p.Operation) != 26 {
		return intent{}, failure
	}
	return p, nil
}
func (s *Service) find(id string) (*core.Record, error) {
	return s.app.FindFirstRecordByFilter("operation_audits", "operation_key={:id} && accounting_spec != null", dbx.Params{"id": id})
}
func storedMutation(r *core.Record) client.AccountingMutation {
	v := stored{}
	raw, _ := json.Marshal(r.GetRaw("accounting_spec"))
	_ = json.Unmarshal(raw, &v)
	return v.Mutation
}
func (s *Service) view(r *core.Record) Result {
	m := storedMutation(r)
	status, message := r.GetString("status"), r.GetString("message")
	if status == "processing" {
		if _, active := s.running.Load(r.GetString("operation_key")); !active {
			status = "unknown"
			message = "执行可能已中断，请核实结果，勿重复提交。"
		}
	}
	return Result{r.GetString("operation_key"), m.Action, spec(m), m.Cluster, status, message, r.GetString("reason"), r.GetString("created"), r.GetString("updated")}
}
func reached(snapshot adapter.AccountingSnapshot, m client.AccountingMutation) bool {
	if client.IsReservation(m.Action) {
		return reservationReached(snapshot, m)
	}
	if isTRES(m.Action) {
		return tresReached(snapshot, m)
	}
	if isQOS(m.Action) {
		return qosReached(snapshot, m)
	}
	if snapshot.Cluster != m.Cluster {
		return false
	}
	switch m.Action {
	case "account_create", "account_update":
		a := findAccount(snapshot, m.Account)
		return a != nil && a.Description == m.Description && a.Organization == m.Organization && parent(snapshot, m.Account) != nil
	case "user_create":
		u := findUser(snapshot, m.User)
		return u != nil && u.DefaultAccount == m.Account && slices.Equal(u.AdministratorLevel, []string{"None"}) && matching(snapshot, m) != nil
	case "association_create":
		return matching(snapshot, m) != nil
	case "association_remove":
		return matching(snapshot, m) == nil
	}
	return false
}
func (s *Service) Get(ctx context.Context, actor, cluster, id string) (Result, error) {
	r, err := s.find(id)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, client.Failure("not_found", "操作记录不存在。")
	}
	if err != nil {
		return Result{}, err
	}
	m := storedMutation(r)
	access, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return Result{}, err
	}
	read := readPermission(m.Action)
	if isTRES(m.Action) && !tresAllowed(access, cluster, m.Action) {
		return Result{}, client.Failure("operation_forbidden", "TRES 限额读取权限已撤销。")
	}
	if isQOS(m.Action) && !access.Allows("platform.qos.read", "") {
		return Result{}, client.Failure("operation_forbidden", "共享 QOS 操作记录需要平台目录读取权限。")
	}
	if r.GetString("cluster_id") != cluster || !access.Allows(read, cluster) || (r.GetString("actor_id") != actor && !access.Allows("platform.audit.read", "")) {
		return Result{}, client.Failure("operation_forbidden", "无权读取该操作。")
	}
	v := s.view(r)
	if !slices.Contains([]string{"processing", "accepted", "unknown"}, v.Status) {
		return v, nil
	}
	if _, active := s.running.Load(id); active {
		return v, nil
	}
	if !s.mutation.TryLock() {
		return v, nil
	}
	defer s.mutation.Unlock()
	c, err := services.ConnectionFor(s.app, cluster)
	if err != nil || c.Revision != r.GetInt("cluster_revision") {
		return v, nil
	}
	snapshot, err := readSnapshot(ctx, c.Adapter, m.Action)
	latestAccess, accessErr := rbac.Resolve(s.app, actor)
	if accessErr != nil {
		return Result{}, accessErr
	}
	if !latestAccess.Allows(read, cluster) || isQOS(m.Action) && !latestAccess.Allows("platform.qos.read", "") || isTRES(m.Action) && !tresAllowed(latestAccess, cluster, m.Action) {
		return Result{}, client.Failure("operation_forbidden", "记账读取权限已撤销。")
	}
	if err != nil {
		return v, nil
	}
	current, err := services.ConnectionFor(s.app, cluster)
	if err == nil && current.Revision == c.Revision && reached(snapshot, m) {
		r.Set("status", "confirmed")
		r.Set("message", "已只读核实目标状态；未重新发送写请求。")
		if err := s.app.Save(r); err != nil {
			return Result{}, client.Failure("audit_unavailable", "核实结果未能持久化，请稍后重查。")
		}
		services.SlurmFor(s.app).Invalidate(cluster)
		services.SlurmFor(s.app).InvalidateAccounting()
		return s.view(r), nil
	}
	return v, nil
}
func (s *Service) Execute(actor, cluster, requestID string, input Input) (Result, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if utf8.RuneCountInString(input.Reason) < 2 || utf8.RuneCountInString(input.Reason) > 300 || strings.ContainsAny(input.Reason, "\r\n\x00") {
		return Result{}, client.Failure("invalid_request", "请填写 2–300 字的单行操作原因。")
	}
	p, err := s.parse(input.Proof, actor, cluster)
	if err != nil {
		return Result{}, err
	}
	if input.Confirmation != confirmation(p.Mutation) {
		return Result{}, client.Failure("invalid_request", "确认文本与目标不一致。")
	}
	if err := s.authorized(actor, cluster, p.Action); err != nil {
		return Result{}, err
	}
	hash := digest(input)
	replay := func(r *core.Record) (Result, error) {
		if r.GetString("actor_id") != actor || r.GetString("cluster_id") != cluster || r.GetString("input_hash") != hash {
			return Result{}, client.Failure("confirmation_invalid", "同一确认请求的内容不能变更。")
		}
		return s.Get(s.ctx, actor, cluster, p.Operation)
	}
	if r, err := s.find(p.Operation); err == nil {
		return replay(r)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	if p.Expires <= time.Now().Unix() {
		return Result{}, client.Failure("confirmation_expired", "记账确认已过期，请重新校验。")
	}
	if !s.mutation.TryLock() {
		return Result{}, client.Failure("busy", "已有记账变更正在执行，请稍后重试。")
	}
	defer s.mutation.Unlock()
	if r, err := s.find(p.Operation); err == nil {
		return replay(r)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	c, err := services.ConnectionFor(s.app, cluster)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, min(60*time.Second, 10*c.Timeout+time.Second))
	defer cancel()
	snapshot, err := readSnapshot(ctx, c.Adapter, p.Action)
	if err != nil {
		return Result{}, err
	}
	if c.Revision != p.Revision || state(snapshot, p.Mutation) != p.State {
		return Result{}, client.Failure("confirmation_changed", "目标配置或连接已变化，请重新校验。")
	}
	if denied := policy(snapshot, p.Mutation); denied != "" {
		return Result{}, client.Failure("operation_forbidden", denied)
	}
	if err := s.pending(p.Mutation); err != nil {
		return Result{}, err
	}
	if err := s.checkLive(ctx, c, snapshot, p.Mutation); err != nil {
		return Result{}, err
	}
	collection, err := s.app.FindCollectionByNameOrId("operation_audits")
	if err != nil {
		return Result{}, err
	}
	r := core.NewRecord(collection)
	kind, id := target(p.Mutation)
	for field, value := range map[string]any{"operation_key": p.Operation, "cluster_revision": p.Revision, "action": Action(p.Action), "status": "processing", "message": "操作正在处理。", "reason": input.Reason, "input_hash": hash, "fingerprint": p.State, "accounting_spec": stored{p.Mutation}, "observed_states": []string{}, "changes": changes(snapshot, p.Mutation)} {
		r.Set(field, value)
	}
	actorRecord, err := s.app.FindRecordById("users", actor)
	if err != nil {
		return Result{}, err
	}
	if err := audit.Stamp(s.app, r, audit.PlatformActor(actorRecord, requestID), cluster, kind, id); err != nil {
		return Result{}, err
	}
	s.running.Store(p.Operation, true)
	defer s.running.Delete(p.Operation)
	if err := s.app.Save(r); err != nil {
		return Result{}, client.Failure("audit_unavailable", "无法持久化审计，未发送记账写请求。")
	}
	finish := func(status, message string) (Result, error) {
		r.Set("status", status)
		r.Set("message", message)
		if err := s.app.Save(r); err != nil {
			v := s.view(r)
			v.Status = "unknown"
			v.Message = "结果未能持久化，请查询操作记录，勿重复提交。"
			return v, nil
		}
		return s.view(r), nil
	}
	current, err := services.ConnectionFor(s.app, cluster)
	if err != nil || current.Revision != p.Revision {
		return finish("failed", "连接配置已变化，未发送写请求。")
	}
	if err := s.authorized(actor, cluster, p.Action); err != nil || p.Expires <= time.Now().Unix() {
		return finish("failed", "权限已变化或确认已过期，未发送写请求。")
	}
	services.SlurmFor(s.app).Invalidate(cluster)
	sent, writeErr := current.Client.MutateAccounting(ctx, p.Mutation)
	// Global labels/defaults can appear through other configured clusters too.
	services.SlurmFor(s.app).InvalidateAccounting()
	if writeErr != nil && !sent {
		return finish("failed", writeErr.Error())
	}
	observed, readErr := readSnapshot(ctx, current.Adapter, p.Action)
	latest, configErr := services.ConnectionFor(s.app, cluster)
	if readErr == nil && configErr == nil && latest.Revision == p.Revision && reached(observed, p.Mutation) {
		return finish("confirmed", "已核实目标状态。")
	}
	if writeErr == nil {
		return finish("accepted", "Slurm 已受理，结果待只读核实；请勿重复提交。")
	}
	return finish("unknown", "写请求结果未知，请查询操作记录及 Slurm 状态，勿重复提交。")
}
func changes(s adapter.AccountingSnapshot, m client.AccountingMutation) []audit.Change {
	if client.IsReservation(m.Action) {
		return reservationChanges(s, m)
	}
	if isTRES(m.Action) {
		return tresChanges(s, m)
	}
	if isQOS(m.Action) {
		return qosChanges(s, m)
	}
	out := []audit.Change{{Field: "account", After: m.Account}, {Field: "slurmCluster", After: m.Cluster}}
	if m.User != "" {
		out = append(out, audit.Change{Field: "username", After: m.User})
	}
	if m.Partition != "" {
		out = append(out, audit.Change{Field: "partition", After: m.Partition})
	}
	if m.AssociationID != "" {
		out = append(out, audit.Change{Field: "associationId", Before: m.AssociationID})
	}
	if strings.HasPrefix(m.Action, "account_") {
		beforeDescription, beforeOrganization := "", ""
		if a := findAccount(s, m.Account); a != nil {
			beforeDescription = a.Description
			beforeOrganization = a.Organization
		}
		out = append(out, audit.Change{Field: "description", Before: beforeDescription, After: m.Description}, audit.Change{Field: "organization", Before: beforeOrganization, After: m.Organization})
	}
	return out
}

func (s *Service) Recent(actor, cluster string) ([]Result, error) {
	return s.recent(actor, cluster, "accounting")
}
func (s *Service) RecentQOS(actor, cluster string) ([]Result, error) {
	return s.recent(actor, cluster, "qos")
}
func (s *Service) RecentReservations(actor, cluster string) ([]Result, error) {
	return s.recent(actor, cluster, "reservation")
}
func (s *Service) RecentTRES(actor, cluster string) ([]Result, error) {
	return s.recent(actor, cluster, "tres")
}
func (s *Service) recent(actor, cluster string, family string) ([]Result, error) {
	access, err := rbac.Resolve(s.app, actor)
	if err != nil {
		return nil, err
	}
	if family == "reservation" && !access.Allows("reservations.read", cluster) {
		return nil, client.Failure("operation_forbidden", "预留读取权限已撤销。")
	}
	if family == "qos" && !access.Allows("platform.qos.read", "") {
		return []Result{}, nil
	}
	if family == "qos" && !access.Allows("qos.read", cluster) || family == "accounting" && !access.Allows("accounts.read", cluster) && !access.Allows("slurm_users.read", cluster) && !access.Allows("associations.read", cluster) {
		return nil, client.Failure("operation_forbidden", "未授予该集群的记账读取权限。")
	}
	filter := "cluster_id={:cluster} && actor_id={:actor} && accounting_spec != null"
	if family == "qos" {
		filter += " && (action='qos.create' || action='qos.update')"
	} else if family == "reservation" {
		filter += " && (action='reservations.create' || action='reservations.update' || action='reservations.delete')"
	} else if family == "tres" {
		filter += " && (action='qos.tres.update' || action='associations.tres.update')"
	} else {
		filter += " && action!='qos.create' && action!='qos.update' && action!='qos.tres.update' && action!='associations.tres.update' && action!='reservations.create' && action!='reservations.update' && action!='reservations.delete'"
	}
	rows, err := s.app.FindRecordsByFilter("operation_audits", filter, "-created", 10, 0, dbx.Params{"cluster": cluster, "actor": actor})
	if err != nil {
		return nil, err
	}
	out := []Result{}
	for _, r := range rows {
		m := storedMutation(r)
		read := readPermission(m.Action)
		if access.Allows(read, cluster) && (!isTRES(m.Action) || tresAllowed(access, cluster, m.Action)) {
			out = append(out, s.view(r))
		}
	}
	return out, nil
}
