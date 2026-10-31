// Package diagnostics derives read-only, evidence-linked explanations from one job record.
package diagnostics

import (
	"fmt"
	"math"
	"slices"
	"slurm-console/backend/internal/slurm/models"
	"strconv"
	"strings"
	"time"
)

type Evidence struct {
	ID    string `json:"id"`
	Field string `json:"field"`
	Label string `json:"label"`
	Value string `json:"value"`
}
type Check struct {
	Audience string `json:"audience"`
	Text     string `json:"text"`
}
type Finding struct {
	Code        string   `json:"code"`
	Category    string   `json:"category"`
	Severity    string   `json:"severity"`
	Confidence  string   `json:"confidence"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Explanation string   `json:"explanation"`
	EvidenceIDs []string `json:"evidenceIds"`
	Checks      []Check  `json:"checks"`
}
type Identity struct {
	JobID        string        `json:"jobId"`
	RecordKey    string        `json:"recordKey"`
	Name         string        `json:"name"`
	User         string        `json:"user"`
	Account      string        `json:"account"`
	Partition    string        `json:"partition"`
	QOS          string        `json:"qos"`
	Nodes        string        `json:"nodes"`
	States       []string      `json:"states"`
	ArrayJobID   models.Number `json:"arrayJobId"`
	ArrayTaskID  models.Number `json:"arrayTaskId"`
	HetJobID     models.Number `json:"hetJobId"`
	HetJobOffset models.Number `json:"hetJobOffset"`
}
type Timeline struct {
	SinceSubmitSeconds     *int64 `json:"sinceSubmitSeconds"`
	SubmitToStartSeconds   *int64 `json:"submitToStartSeconds"`
	ObservedRunSeconds     *int64 `json:"observedRunSeconds"`
	ReportedElapsedSeconds *int64 `json:"reportedElapsedSeconds"`
}
type Metrics struct {
	AllocatedCPUs        *float64 `json:"allocatedCpus"`
	TotalCPUSeconds      *float64 `json:"totalCpuSeconds"`
	CPUAccountingPercent *float64 `json:"cpuAccountingPercent"`
}
type Coverage struct {
	TotalSteps     int `json:"totalSteps"`
	AbnormalSteps  int `json:"abnormalSteps"`
	DisplayedSteps int `json:"displayedSteps"`
}
type Report struct {
	Version     int        `json:"version"`
	Source      string     `json:"source"`
	ObservedAt  string     `json:"observedAt"`
	GeneratedAt string     `json:"generatedAt"`
	Identity    Identity   `json:"identity"`
	Category    string     `json:"category"`
	Headline    string     `json:"headline"`
	Timeline    Timeline   `json:"timeline"`
	Metrics     Metrics    `json:"metrics"`
	Coverage    Coverage   `json:"coverage"`
	Evidence    []Evidence `json:"evidence"`
	Findings    []Finding  `json:"findings"`
	Limitations []string   `json:"limitations"`
}

func number(n models.Number) string {
	if n.Infinite {
		return "不限"
	}
	if n.Value == nil {
		return "未知"
	}
	return strconv.FormatInt(*n.Value, 10)
}
func known(n models.Number) *int64 {
	if n.Infinite || n.Value == nil || *n.Value < 0 {
		return nil
	}
	return n.Value
}
func stamp(n models.Number) string {
	v := known(n)
	if v == nil || *v == 0 || *v > 253402300799 {
		return "未知/未发生"
	}
	return time.Unix(*v, 0).UTC().Format(time.RFC3339)
}
func delta(a, b models.Number) *int64 {
	x, y := known(a), known(b)
	if x == nil || y == nil || *x <= 0 || *y < *x || *y > 253402300799 {
		return nil
	}
	v := *y - *x
	return &v
}
func (r *Report) fact(id, field, label, value string) string {
	r.Evidence = append(r.Evidence, Evidence{id, field, label, value})
	return id
}
func (r *Report) finding(code, category, severity, confidence, kind, title, explanation string, ids []string, checks ...Check) {
	if checks == nil {
		checks = []Check{}
	}
	r.Findings = append(r.Findings, Finding{code, category, severity, confidence, kind, title, explanation, ids, checks})
}
func user(s string) Check  { return Check{"user", s} }
func admin(s string) Check { return Check{"admin", s} }
func base(j models.Job, source, key, observed string) Report {
	r := Report{Version: 1, Source: source, ObservedAt: observed, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Identity: Identity{JobID: j.ID, RecordKey: key, Name: j.Name, User: j.User, Account: j.Account, Partition: j.Partition, QOS: j.QOS, Nodes: j.Nodes, States: j.States, ArrayJobID: j.ArrayJobID, ArrayTaskID: j.ArrayTaskID, HetJobID: j.HetJobID, HetJobOffset: j.HetJobOffset}, Evidence: []Evidence{}, Findings: []Finding{}, Limitations: []string{"仅解释这一条来源记录，不将当前控制器和历史记录、重复 Job ID、数组元素或不同步骤的时间线合并。", "未读取脚本、环境变量、stdout/stderr、节点日志或进程/CPU/GPU 时间序列；不能由状态码定位具体代码故障或证明性能退化。"}}
	r.fact("states", "states", "记录状态", strings.Join(j.States, ", "))
	r.fact("reason", "reason", "来源原因码", j.Reason)
	r.fact("submit", "submitTime", "提交时间（UTC）", stamp(j.SubmitTime))
	r.fact("start", "startTime", "开始/预计开始（UTC）", stamp(j.StartTime))
	r.fact("end", "endTime", "结束/预计结束（UTC）", stamp(j.EndTime))
	r.fact("timeLimit", "timeLimitMinutes", "时限（分钟）", number(j.TimeLimitMinutes))
	r.fact("placement", "partition/qos/nodes", "分区 / QOS / 节点", j.Partition+" / "+j.QOS+" / "+j.Nodes)
	now, e := time.Parse(time.RFC3339Nano, observed)
	if e == nil {
		n := now.Unix()
		r.Timeline.SinceSubmitSeconds = delta(j.SubmitTime, models.Number{Value: &n})
		if slices.Contains(j.States, "RUNNING") && !slices.Contains(j.States, "COMPLETING") {
			r.Timeline.ObservedRunSeconds = delta(j.StartTime, models.Number{Value: &n})
		}
	}
	if e == nil && !slices.Contains(j.States, "PENDING") && !slices.Contains(j.States, "CONFIGURING") && !slices.Contains(j.States, "POWER_UP_NODE") && known(j.StartTime) != nil && *j.StartTime.Value <= now.Unix() {
		r.Timeline.SubmitToStartSeconds = delta(j.SubmitTime, j.StartTime)
	}
	stateRules(&r, j.States, j.Reason)
	return r
}
func stateRules(r *Report, states []string, reason string) {
	has := func(s string) bool { return slices.Contains(states, s) }
	bases := 0
	for _, s := range []string{"PENDING", "RUNNING", "SUSPENDED", "COMPLETED", "CANCELLED", "FAILED", "TIMEOUT", "NODE_FAIL", "BOOT_FAIL", "DEADLINE", "OUT_OF_MEMORY", "PREEMPTED"} {
		if has(s) {
			bases++
		}
	}
	r.Category, r.Headline = "unknown", "现有状态不足以确定作业阶段"
	if bases > 1 {
		r.Timeline = Timeline{}
		r.finding("state_conflict", "unknown", "warning", "high", "fact", "记录包含多个基础状态", "来源状态相互冲突，暂停阶段与时间间隔推断，请核对相同记录的最新来源。", []string{"states"}, admin("核对 REST 原始状态及控制器/记账更新，避免把重排队前后记录混用。"))
		return
	}
	switch {
	case has("COMPLETING"):
		r.Category, r.Headline = "ending", "作业处于结束清理阶段"
	case has("CONFIGURING") || has("POWER_UP_NODE"):
		r.Category, r.Headline = "starting", "作业处于节点启动准备阶段"
		r.Timeline.ObservedRunSeconds = nil
	case has("PENDING"):
		r.Category, r.Headline = "waiting", "作业正在等待调度"
	case has("RUNNING"):
		r.Category, r.Headline = "running", "作业记录为运行中"
	case has("SUSPENDED"):
		r.Category, r.Headline = "suspended", "作业执行已挂起"
	case has("COMPLETED"):
		r.Category, r.Headline = "completed", "Slurm 记录作业已完成"
	case has("CANCELLED") || has("PREEMPTED"):
		r.Category, r.Headline = "interrupted", "作业被取消或抢占"
	case has("FAILED") || has("TIMEOUT") || has("OUT_OF_MEMORY") || has("NODE_FAIL") || has("BOOT_FAIL") || has("DEADLINE"):
		r.Category, r.Headline = "failure", "Slurm 记录异常终止"
	}
	if has("PENDING") {
		pending(r, reason)
	}
	for _, rule := range []struct {
		state, title, explanation string
		check                     Check
	}{
		{"OUT_OF_MEMORY", "Slurm 报告内存不足", "记录存在 OUT_OF_MEMORY；具体超限进程和内存峰值尚未验证。MaxRSS 通常是某步骤中单任务的峰值，不能当作整作业总内存。", user("在此记录的申请/分配 TRES、各步骤及 stderr 中核对 OOM；需要时由管理员检查 cgroup 事件和采样配置。")},
		{"TIMEOUT", "Slurm 报告达到时限", "TIMEOUT 表示触及时限；尚不能区分工作量超时与 I/O、锁或通信停滞。", user("对照时限、记账已运行时长和应用阶段日志，检查进度或检查点，再评估后续提交的资源/时间。")},
		{"FAILED", "Slurm 报告作业失败", "FAILED 本身不说明哪条命令失败；需结合主退出码、派生退出码、各步骤和应用日志。", user("核对失败步骤及 stderr 的首个错误，检查输入、可执行文件和运行环境。")},
		{"NODE_FAIL", "Slurm 报告节点故障终止", "记录为 NODE_FAIL；不能仅凭此状态判定具体硬件或守护进程故障。", admin("按此记录的节点和时间窗核对节点原因、slurmd 日志及同节点事件。")},
		{"BOOT_FAIL", "Slurm 报告启动节点失败", "BOOT_FAIL 指向节点启动阶段，需要节点/启动路径证据。", admin("核对所选节点的启动、prolog 和守护进程日志。")},
		{"DEADLINE", "Slurm 报告错过截止条件", "DEADLINE 记录不等同应用崩溃；需核对 deadline、时限和可调度时间。", user("核对原始 deadline 与时限及提交策略；当前 DTO 未提供 deadline 字段。")},
		{"CANCELLED", "作业记录为取消", "取消不等同应用崩溃；当前证据未确认取消者和具体操作原因。", admin("如需追溯，按作业和时间窗核对操作审计与控制器事件。")},
		{"PREEMPTED", "作业记录为抢占", "抢占与策略有关，后续可能重排队；本次只解释当前记录。", admin("核对 QOS/分区抢占策略、PreemptMode 和此作业的后续独立记录。")},
		{"SUSPENDED", "作业执行已挂起", "SUSPENDED 不等同应用卡死；仅凭当前记录不能确定挂起原因或持续时间。", admin("核对授权操作、抢占模式和挂起事件时间。")},
		{"COMPLETING", "观察到结束清理标志", "没有进入 COMPLETING 的准确时间，不据此断言长时间卡住。", admin("若持续不结束，按时间窗检查 epilog、不可终止进程和文件系统事件。")},
		{"LAUNCH_FAILED", "观察到启动失败标志", "LAUNCH_FAILED 可能涉及 prolog 或其他启动条件，具体原因需要日志。", admin("核对节点启动与 prolog 日志、可执行文件/工作目录及对应时间窗。")},
		{"CONFIGURING", "观察到配置节点标志", "已分配节点但仍在等待启动或重启，不把这个阶段视为应用已经正常推进。", admin("核对配置/启动节点状态与相同时间窗的节点事件。")},
		{"POWER_UP_NODE", "观察到节点上电标志", "作业正在等待已分配的节能节点启动，尚不能据此判断启动故障。", admin("核对节点上电流程、启动时间及电源管理事件。")},
		{"RECONFIG_FAIL", "观察到节点配置失败标志", "配置路径发生异常，具体失败环节需要管理员日志。", admin("核对相关节点配置和 prolog/守护进程事件。")},
		{"REQUEUE_HOLD", "观察到重排队并保持标志", "作业在重排队后被保持，不会仅凭当前标志推断应自动释放。", admin("核对保持原因与重排队记录，由有权限的人决定后续处理。")},
		{"REVOKED", "观察到联邦撤销标志", "该标志与联邦 sibling 作业条件相关，不能当作应用崩溃。", admin("核对同一联邦作业的 sibling 记录和站点策略。")},
		{"REQUEUED", "观察到重排队标志", "重排队会改变执行过程，当前记录不足以还原各次尝试的完整时间线。", admin("按提交时间、开始时间和记录标识分别核对各次尝试。")},
	} {
		if has(rule.state) {
			r.finding(strings.ToLower(rule.state), r.Category, "warning", "high", "fact", rule.title, rule.explanation, []string{"states"}, rule.check)
		}
	}
	if has("RUNNING") && r.Category == "running" {
		r.finding("running_evidence", "running", "info", "high", "fact", "记录显示已分配并运行", "RUNNING 和时间经过不能证明应用在推进，也不能说明变慢的根因。", []string{"states", "start"}, user("核对应用进度与同输入、版本和资源的基线；必要时采集此作业 sstat 与应用 profile，分开比较排队和运行阶段。"))
	}
	if has("COMPLETED") {
		r.finding("completed_state", "completed", "info", "high", "fact", "Slurm 记录为 COMPLETED", "该状态不替代业务结果校验；还应核对派生退出码、步骤和预期输出。", []string{"states"}, user("检查预期产物和步骤退出结果；不将 Slurm 完成状态等同业务计算正确。"))
	}
	if len(r.Findings) == 0 {
		r.finding("unknown_state", "unknown", "info", "low", "inference", "需要补充阶段证据", "当前规则没有匹配到可确认的阶段或异常类型，未知不等同正常。", []string{"states", "reason"}, admin("核对来源状态/标志、采样时间和当前 Slurm 版本文档。"))
	}
}
func pending(r *Report, reason string) {
	title, text, check := "等待原因尚未分类", "来源未给出已识别原因；不据此推断资源空闲或预计启动时间。", user("核对此作业的当前 Reason、依赖、最早开始、请求资源和站点策略。")
	switch {
	case reason == "Priority":
		title, text = "原因码指向调度优先级", "当前调度尝试报告优先级等待；不等于只按提交先后排序，也不是启动时间承诺。"
		check = admin("核对同分区/QOS 的优先级与 fairshare、预留策略；不为此自动改优先级。")
	case reason == "Resources":
		title, text = "原因码指向所请求资源不可用", "不能由 Resources 单独判断是 CPU、内存、GPU 或拓扑限制，也不能由空闲节点数证明能调度。"
		check = admin("对照此作业的资源/约束、分区限制与候选节点的配置、分配和状态；FreeMem 不能单独作为可调度内存。")
	case reason == "Dependency" || reason == "DependencyNeverSatisfied":
		title, text = "原因码指向作业依赖", "当前 DTO 不包含完整依赖表达式和前序结果，不能判断修改依赖是否正确。"
		check = user("在相同集群核对原始 Dependency 与前序作业结果，确认预期执行顺序。")
	case reason == "BeginTime":
		title, text = "原因码指向最早开始时间", "最早开始条件尚未满足；控制器的预计开始时间不等于实际开始。"
		check = user("核对原始 BeginTime 和集群时钟，无需把等待时间算作程序运行时间。")
	case reason == "JobHeldUser" || reason == "JobHeldAdmin" || reason == "JobHeld":
		title, text = "原因码指向作业保持", "作业被保持，不把它归为程序运行失败；释放会改变调度状态，需要相应授权。"
		check = admin("核对保持者和保持原因，确认是否应由拥有权限的人处理。")
	case strings.HasPrefix(reason, "AssocGrp") || strings.HasPrefix(reason, "AssocMax") || strings.HasPrefix(reason, "AssocMin") || strings.HasPrefix(reason, "QOSGrp") || strings.HasPrefix(reason, "QOSMax") || strings.HasPrefix(reason, "QOSMin"):
		title, text = "原因码指向 Association / QOS 限额", "需结合实际策略与使用量；本接口不读取或修改记账策略，不能仅凭码名确定应提高哪项额度。"
		check = admin("按此作业的 Account/QOS/资源请求核对对应 Association 或 QOS 限额及当前使用量。")
	case reason == "PartitionDown" || reason == "PartitionInactive" || reason == "PartitionNodeLimit" || reason == "PartitionTimeLimit" || reason == "PartitionMemoryLimit":
		title, text = "原因码指向分区状态或限制", "分区状态、节点数、时限或内存限制需要按完整原因码核对，不能直接建议换分区。"
		check = admin("核对当前原因码对应的分区状态/边界与作业请求，再决定是否需要调整后续提交。")
	case reason == "ReqNodeNotAvail" || reason == "NodeDown" || reason == "BadConstraints" || reason == "PartitionConfig":
		title, text = "原因码指向节点可用性或约束", "需要核对必选/排除节点、特性及资源组合，当前 DTO 没有全部约束；不将其直接认定为硬件故障。"
		check = admin("核对原始 ReqNodeList/ExcNodeList、特性及分区候选节点，不自动解除节点约束。")
	case reason == "InvalidAccount" || reason == "AccountNotAllowed" || reason == "InvalidQOS" || reason == "QOSNotAllowed":
		title, text = "原因码指向账户或 QOS 可用性", "当前调度身份/分区策略可能不满足要求，需核对实际关联和允许项。"
		check = admin("核对用户关联、Account/QOS 和分区允许项；本诊断不会授予或扩大权限。")
	}
	r.finding("pending_reason", "waiting", "info", "medium", "inference", title, text+" 原因码通常只反映当前调度尝试的一项原因，不保证唯一根因。", []string{"states", "reason", "placement"}, check)
}
func Current(j models.Job, observed string) Report {
	r := base(j, "controller", "", observed)
	r.fact("cpus", "cpus", "请求/分配 CPU（控制器口径）", number(j.CPUs))
	r.fact("memoryNode", "memoryPerNodeMiB", "申请每节点内存（MiB）", number(j.MemoryPerNodeMiB))
	r.fact("memoryCPU", "memoryPerCpuMiB", "申请每 CPU 内存（MiB）", number(j.MemoryPerCPUMiB))
	r.Limitations = append(r.Limitations, "当前控制器诊断不读取历史记账。PENDING 的开始/结束可能是预计时间，不用于计算实际运行或提交至开始间隔。")
	return r
}
func exitText(e models.HistoryExit) string {
	return strings.Join(e.Status, ",") + " · 退出码 " + number(e.Code) + " · 信号 " + number(e.Signal)
}
func exitBad(e models.HistoryExit) bool {
	c, s := known(e.Code), known(e.Signal)
	return c != nil && *c != 0 || s != nil && *s != 0
}
func resources(a []models.HistoryTRES) string {
	v := []string{}
	for _, t := range a {
		c := "未知"
		if t.Count != nil {
			c = *t.Count
		}
		v = append(v, t.Type+"/"+t.Name+"="+c)
	}
	return strings.Join(v, " · ")
}
func Accounting(j models.HistoryJob, observed string) Report {
	current := models.Job{ID: j.ID, Name: j.Name, User: j.User, Account: j.Account, Partition: j.Partition, QOS: j.QOS, Nodes: j.Nodes, States: j.States, Reason: j.Reason, SubmitTime: j.SubmitTime, StartTime: j.StartTime, EndTime: j.EndTime, TimeLimitMinutes: j.TimeLimitMinutes, ArrayJobID: j.ArrayJobID, ArrayTaskID: j.ArrayTaskID, HetJobID: j.HetJobID, HetJobOffset: j.HetJobOffset}
	r := base(current, "accounting", j.Key, observed)
	r.Timeline.SinceSubmitSeconds = nil
	r.Timeline.ObservedRunSeconds = nil // Do not extrapolate a live runtime from historical accounting snapshots.
	r.Timeline.ReportedElapsedSeconds = known(j.ElapsedSeconds)
	r.fact("elapsed", "elapsedSeconds", "记账已运行时长（秒）", number(j.ElapsedSeconds))
	r.fact("exit", "exit", "作业主退出记录", exitText(j.Exit))
	r.fact("derivedExit", "derivedExit", "步骤派生退出记录", exitText(j.DerivedExit))
	r.fact("requested", "requestedTres", "申请 TRES", resources(j.RequestedTRES))
	r.fact("allocated", "allocatedTres", "分配 TRES", resources(j.AllocatedTRES))
	if exitBad(j.Exit) {
		r.finding("job_exit", "failure", "warning", "high", "fact", "主退出记录为非零码或信号", "已记录退出码/信号，具体失败指令与发信号来源需要应用日志或系统事件。", []string{"exit"}, user("核对此次执行的 stderr、脚本返回值和对应步骤；非零码不直接等同某一种程序错误。"))
	}
	if exitBad(j.DerivedExit) {
		r.finding("derived_exit", "failure", "warning", "high", "fact", "派生退出记录存在异常", "即使作业主记录完成，也应检查各步骤。派生退出码可能被授权用户事后注释或调整，不能代替步骤原始记录。", []string{"states", "derivedExit"}, user("核对下方异常步骤与历史详情中的原始步骤退出码。"))
	}
	r.Coverage.TotalSteps = len(j.Steps)
	stepEvidence := []string{}
	for i, s := range j.Steps {
		bad := exitBad(s.Exit)
		for _, v := range s.States {
			bad = bad || slices.Contains([]string{"FAILED", "OUT_OF_MEMORY", "TIMEOUT", "NODE_FAIL", "BOOT_FAIL", "CANCELLED", "PREEMPTED", "LAUNCH_FAILED"}, v)
		}
		if !bad {
			continue
		}
		r.Coverage.AbnormalSteps++
		if len(stepEvidence) < 20 {
			id := fmt.Sprintf("step_%d", i)
			r.fact(id, fmt.Sprintf("steps[%d]", i), "步骤 "+s.ID+" / "+s.Name, strings.Join(s.States, ",")+" · "+exitText(s.Exit))
			stepEvidence = append(stepEvidence, id)
		}
	}
	r.Coverage.DisplayedSteps = len(stepEvidence)
	if r.Coverage.AbnormalSteps > 0 {
		r.finding("step_failures", "failure", "warning", "high", "fact", "存在异常步骤记录", fmt.Sprintf("分析了 %d 个步骤，发现 %d 个异常步骤，列出前 %d 个证据。步骤不重复加算为作业或累计 CPU 时间。", len(j.Steps), r.Coverage.AbnormalSteps, len(stepEvidence)), stepEvidence, user("在此条历史详情中核对具体步骤状态、退出码、TRES 和相应应用日志。"))
	}
	r.Limitations = append(r.Limitations, "记账可能延迟或采样不完整，缺失或零用量不必然表示没有使用。未将步骤 CPU 时间累加到作业总 CPU 时间，也未把 TRES 消耗字段解释为 MaxRSS 或整作业内存峰值。")
	cpuMetrics(&r, j)
	if r.Category == "completed" && (exitBad(j.Exit) || exitBad(j.DerivedExit) || r.Coverage.AbnormalSteps > 0) {
		r.Headline = "主记录已完成，但退出或步骤存在异常"
	}
	slices.SortStableFunc(r.Findings, func(a, b Finding) int {
		if a.Severity == b.Severity {
			return 0
		}
		if a.Severity == "warning" {
			return -1
		}
		return 1
	})
	return r
}
func cpuMetrics(r *Report, j models.HistoryJob) {
	var cpus *float64
	cpuEntries := 0
	for _, v := range j.AllocatedTRES {
		if v.Type == "cpu" && v.Name == "" {
			cpuEntries++
			if cpuEntries > 1 {
				cpus = nil
				break
			}
			if v.Count == nil {
				continue
			}
			n, e := strconv.ParseFloat(*v.Count, 64)
			if e == nil && n > 0 && n <= 9007199254740991 && math.Trunc(n) == n {
				if cpus != nil {
					cpus = nil
					break
				}
				cpus = &n
			}
		}
	}
	r.Metrics.AllocatedCPUs = cpus
	total := j.TotalCPUSeconds
	if total != nil && !math.IsNaN(*total) && !math.IsInf(*total, 0) && *total >= 0 {
		r.Metrics.TotalCPUSeconds = total
		r.fact("totalCPU", "totalCpuSeconds", "作业总 CPU 记账时间（秒）", strconv.FormatFloat(*total, 'f', 6, 64))
	}
	elapsed := known(j.ElapsedSeconds)
	// Keep the approximation narrow: completed, fixed-size, unsuspended records only.
	eligible := r.Category == "completed" && !slices.Contains(j.States, "RESIZING") && !positive(j.ArrayJobID) && !positive(j.HetJobID) && !slices.Contains(j.States, "REQUEUED") && !slices.Contains(j.States, "REQUEUE_FED") && !slices.Contains(j.States, "REQUEUE_HOLD") && known(j.SuspendedSeconds) != nil && *j.SuspendedSeconds.Value == 0
	if !eligible || cpus == nil || total == nil || r.Metrics.TotalCPUSeconds == nil || elapsed == nil || *elapsed <= 0 || *elapsed > 9007199254740991 {
		r.Limitations = append(r.Limitations, "粗略 CPU 记账占比不可计算：需要已完成、非数组/异构、无挂起记录及有效分配 CPU、总 CPU 时间和已运行时长。")
		return
	}
	ratio := 100 * *total / (*cpus * float64(*elapsed))
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 100 {
		r.Limitations = append(r.Limitations, "CPU 时间与分配 CPU × 已运行时长不一致，占比保持未知，不截断为 100%。")
		return
	}
	r.Metrics.CPUAccountingPercent = &ratio
	r.Limitations = append(r.Limitations, "CPU 记账占比 = 作业 TotalCPU / (分配 CPU × 记账 Elapsed)，仅为粗略指标；不是瞬时利用率或性能基线，零值可能来自采样缺失。")
	if ratio > 0 && ratio < 20 && *elapsed >= 60 && !exitBad(j.Exit) && !exitBad(j.DerivedExit) && r.Coverage.AbnormalSteps == 0 {
		r.finding("low_cpu_accounting", "performance", "info", "low", "inference", "粗略 CPU 记账占比较低", "可能涉及 I/O/锁等待、串行阶段、线程数或 GPU 主导任务；这些方向尚未验证，不能据此认定资源浪费或降低 CPU 配额。", []string{"totalCPU", "allocated", "elapsed"}, user("结合相同输入/版本/资源的基线、应用 profile 和同一时间轴 CPU/GPU/I/O 指标区分候选方向。"))
	}
}

func positive(n models.Number) bool { v := known(n); return v != nil && *v > 0 }
