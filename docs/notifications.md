# 告警通知

`/notifications` 提供本人的站内告警和规则管理，顶部铃铛显示有权读取的未读数量。规则由用户主动创建，按现有来源读取权限运行；不自动为所有用户建规则，不调用 Slurm 控制。通知渠道为站内，邮件、Webhook 与短信外发不在本项实现范围。

## 规则与来源

| 类型 | 条件 | 读取权限 |
| --- | --- | --- |
| 集群健康异常 | 控制器均 DOWN，或健康访问发生明确的连接、超时、TLS、认证、响应异常；threshold 固定为 1 | clusters.read |
| 节点不可用 | DOWN 或 NOT_RESPONDING 节点数量达到 threshold（1–10000）；DRAIN 不单独计入 | nodes.read |
| 排队作业提交超时 | 当前 PENDING，且距提交时间达到 threshold 分钟（1–10000）；重新入队沿用提交时间 | jobs.read |
| GPU 温度过高 | 当前有效温度达到 threshold °C（30–120） | metrics.read |

每条规则绑定一个集群及当前用户；名称 1–40 字，等级为 warning/critical。持续时间 `holdMinutes` 为 0–60 分钟，界面默认 2；0 表示首次有效异常采样即触发。来源数据通过既有 Adapter 和受保护的 GPU 监控配置读取，不接受自定义查询、命令、地址、凭据或收件人。

GPU 沿用管理员已配置的 Prometheus/DCGM。未配置、没有设备、过期、缺失、歧义数据不能证明恢复；存在有效过热样本时可证明触发。排队作业缺少可靠提交时间且没有已知超时项目时也为无法判断。集群停用、配置变化、容量繁忙或监控请求取消不会被当成集群故障。

## 状态与事件

| 状态 | 含义 |
| --- | --- |
| disabled | 本人停用，不再采样 |
| unknown | 尚未采样或数据不足；原有告警可能仍未恢复 |
| ok | 有效采样未达到规则阈值 |
| pending | 达到阈值，等待连续持续时间 |
| firing | 已触发，本轮持续异常不重复提醒 |
| blocked | 读取权限撤销，暂停并隐藏来源证据 |

持续时间以有效连续异常采样计算，未知数据会清空待触发计时；超过 90 秒的采样间隔或时钟回退也重新计时。不能把服务器停机期间视为持续异常证据。已经触发的事件保留到有效采样确认条件消失，未知、缺权或连接停用都不会生成“已恢复”。超过 90 秒未检查时界面提示采样延迟。

触发和恢复各生成一条持久通知，以 `incidentId` 关联，在同一事务中更新规则状态；唯一索引防止同一事件阶段重复写入。重启后保留告警状态，持续异常不会重复发送，正常采样后仍可生成恢复通知。通知不是资源健康的实时保证，查看资源链接会重新读取当前数据。

修改规则、停用或重新启用会开始新的评估上下文，旧通知保留，不凭设置变化生成健康恢复。删除规则停止后续采样，保留历史通知；可以逐条标为已读/未读，也可将当前有权读取的通知全部标为已读。读取通知或打开页面不会自动执行资源检查。

## 调度、实时连接与保留

应用内后台任务约每 30 秒检查启用规则，无需 cron、第三方告警组件或新部署参数。手动和定时检查共用两个并发槽，同一规则不能并行检查，5 秒内重复检查复用最近状态。每轮有 25 秒预算并优先处理较久未检查的规则；拥塞时可能延迟，按 `lastChecked` 和 `stale` 判断采样是否新鲜。

每人最多 20 条规则，平台最多 100 条，包含停用规则。通知保留每人最近 200 条、全平台最多 10000 条、最长 30 天；过期通知立即从读取和未读计数中排除，后台分批清理。状态和通知随现有数据库加密备份，不新增外部存储。

`GET /api/v1/notifications/stream` 为使用 Authorization 请求头的 SSE，只发 `ready`/`changed` 和空对象，不携带资源内容或未读数。浏览器收到提示后重新请求有权限的 API；中断时自动重连，并保留 30 秒轮询。每用户最多 3 条、全平台 64 条实时连接；提示可合并，15 秒心跳复查 JWT、用户活动状态及 PAM/LDAP 资格，单连接最长 5 分钟，进程退出时关闭。SSE 没有持久重放或投递保证，数据以持久通知查询为准，令牌不进入 URL。

## 权限与接口

仅操作当前用户，平台 Super Admin 也不能访问其他人的规则或通知。规则列表在撤权后仅保留用户自己的配置，隐藏采样证据、状态及事件信息，允许本人删除以清理配额。通知按其原始来源类型与集群权限过滤后再统计、分页和标记已读；规则类型变更不会改变旧通知的授权要求。

后台在来源读取前和保存前复查用户活动状态、PAM/LDAP 账户资格及当前 RBAC，事务内再核对规则配置版本、集群修订和活动状态。原生 `notifications`、`notification_rules` Collection CRUD 全部锁定；不新增调度动作权限，不修改 `operation_audits`。

| 方法与路径 | 输入/结果 |
| --- | --- |
| `GET /api/v1/notification-rules?clusterId=…` | 本人规则，clusterId 可省略，最多 20 条 |
| `POST /api/v1/notification-rules` | `{clusterId,name,kind,severity,enabled,threshold,holdMinutes}` |
| `PATCH /api/v1/notification-rules/{id}` | 完整配置加 revision，集群范围不可变；冲突 409 |
| `DELETE /api/v1/notification-rules/{id}` | `{revision}`；保留历史通知 |
| `POST /api/v1/notification-rules/{id}/check` | `{}`；只读采样，可能生成本站内事件，不发送资源控制 |
| `GET /api/v1/notifications` | clusterId、read(all/unread/read)、phase(firing/recovered)、page、perPage；返回全范围有权读取的 unreadCount |
| `POST /api/v1/notifications/{id}/read` | `{read:true或false}` |
| `POST /api/v1/notifications/read-all` | `{}`，返回本次更新数量 |
| `GET /api/v1/notifications/stream` | 带 Authorization 头的 SSE，拒绝额外查询参数 |

统一 JSON 响应与请求标识继续适用；SSE 成功响应为 `text/event-stream`。未知字段、命令、地址、owner 及未知/重复查询参数拒绝，其他人的 ID 返回 404。完整契约见 [OpenAPI 0.23.0](../api/openapi.yaml)。

## 验证

2026-10-31 完成前端 146 项测试、类型检查、lint 和生产构建，后端完整竞态回归、静态检查及告警专项测试。隔离 REST/Prometheus 数据验证四类来源、过期与不完整指标；虚拟机私有实例验证定时触发、未知不恢复、有效恢复、重启去重、SSE 提示、未读/已读、跨用户 Super Admin 隔离及撤权边界。真实 Slurm 节点/健康/作业读取通过，未配置的 GPU 监控维持无法判断；没有发送 Slurm 控制。临时账户、数据库和只读 fixture 已清理，原有 Slurm/PAM/LDAP 配置校验一致。
