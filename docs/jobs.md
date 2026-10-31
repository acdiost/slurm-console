# 作业列表、详情与取消

`/jobs` 按授权集群查询真实作业，支持名称/Job ID 搜索、用户、账户、分区、QOS 和单状态筛选，服务端分页及 Job ID、提交时间、CPU 排序。筛选、分页、详情和操作编号保存在 URL；可暂停每 10 秒刷新。详情展示完整状态标记、状态原因、身份、资源、QOS、时限与时间。数据仅覆盖控制器当前保留且固定 REST 身份可见的记录，不替代历史记账查询。

## 管理员关联 Slurm 身份

平台角色与 Slurm 身份分别校验。拥有 `jobs.read` 和 `jobs.cancel`（Operator/Cluster Admin/Super Admin）仍需管理员显式关联目标集群的规范用户名和数字 UID；PAM/LDAP 登录名不会自动成为 Slurm 操作身份。Viewer 仅可查询。身份表原生 API 不开放，关联不会修改平台角色或 Slurm 权限。

管理员先在目标集群通过 `id <用户名>` 核实用户名和 UID，再在应用主机通过受信任的本地 CLI 配置。CLI 与应用使用相同数据库；以数据库所有者运行，避免生成 root 所有的数据库文件：

```sh
sudo /opt/slurm-console/slurm-console slurm-identity link \
  --dir=/var/lib/slurm-console/pb_data \
  --email alice@example.com --cluster <集群ID或slug> \
  --username alice --uid 1001

# 撤销该关联
sudo /opt/slurm-console/slurm-console slurm-identity unlink \
  --dir=/var/lib/slurm-console/pb_data \
  --email alice@example.com --cluster <集群ID或slug>
```

默认 `--scope own`，只允许取消 UID 和规范用户名同时匹配的本人作业。同集群 UID 只能关联一个平台账户；相同关联重复执行不更新确认版本。如需委派跨用户取消范围，管理员必须显式使用 `--scope all`。该选项扩大平台允许范围，应仅授予可信管理员；它不会为固定 REST 服务身份增加任何 Slurm 权限。首次关联建议使用 `own` 验证。

所有查询与取消使用连接配置中固定的 REST 身份；本项目不把浏览器用户名写入 Slurm 请求头、不按请求切换 Unix 用户、不模拟其他用户。若固定身份无权取消某作业，上游仍会拒绝。管理员应独立配置符合实际管理范围的服务身份和 Slurm 权限；应用安装器不修改这些服务。

## 取消流程与边界

1. 打开确认框后，直接读取最新详情，检查角色、关联、归属、状态及配置版本；这些检查不使用查询缓存。
2. 确认作业信息，填写 2–300 字原因并输入完整 Job ID。确认令牌有效 2 分钟，绑定用户、集群、作业提交身份、状态、重启次数、身份关联及配置版本。过期、状态变化或权限撤销需要重新校验。
3. 执行前再次复查权限和关联，先持久化操作意图，再发送一次固定 `DELETE /slurm/v0.0.43/job/{jobId}?signal=KILL`；不允许自定义信号或批量选择器。审计不可写则不发送取消请求。
4. 重新读取作业，只有同一次提交已为 `CANCELLED` 且不再 `COMPLETING` 才显示“已核实取消”。受理未结束显示“已受理，待核实”；断网、超时、响应不明或进程中断显示“结果待核实”。不会自动重试写请求，刷新操作记录只做读取核实。

首版支持 PENDING、RUNNING、SUSPENDED 的普通非 root 单作业；身份/提交时间/数组及异构标记缺失、已结束、正在收尾、数组和异构作业均拒绝取消。数组实际数字 Job ID 可以查询，但不能取消整组或子任务。

同一确认请求重复提交返回同一审计结果，不重复发送 DELETE；同一次作业已有 processing/accepted/unknown 记录时禁止新取消。应用重启会使全部确认令牌失效（包括 POST 重放），已持久化的操作记录仍可凭操作编号查询。进程崩溃后无法证明是否已发送的请求会保守标为待核实；先核对 Slurm 状态，必要时由管理员在 Slurm 端处理，不应通过重新发送平台请求尝试恢复。

查询缓存在取消前后失效，取消前正在读取的旧快照也会被丢弃；新的读取仍遵循 Adapter 的缓存期限。取消校验、写入和核实共用最多 4 个并发槽，执行总时限为连接超时的 3 倍加 1 秒；浏览器断开后已授权操作仍会在该边界内完成并记录结果。

## 接口与证据

所有接口前缀 `/api/v1/clusters/{clusterId}`，成功统一 `{data,requestId}`，禁止未知/重复查询参数。

| 方法与路径 | 权限 | 结果 |
|---|---|---|
| `GET /jobs`、`GET /jobs/{jobId}` | `jobs.read` | 列表/详情和来源时间 |
| `GET /jobs/{jobId}/cancel-context` | `jobs.read` | 最新详情、allowed/message、关联身份及短期确认；无取消权限时 allowed=false |
| `POST /jobs/{jobId}/cancel` | `jobs.read` + `jobs.cancel` | proof/confirmation/reason 换取持久操作结果 |
| `GET /job-operations/{operationId}` | `jobs.read`，且操作发起人或 `platform.audit.read` | 当前审计及只读状态核实 |

`operation_audits` 在 SQLite 中记录 operationId、请求编号、平台用户、集群及配置版本、Job ID、提交身份指纹、动作、原因、已观察状态、结果和时间；浏览器只获得安全 DTO，不获得内部指纹、确认令牌或上游凭据。Collection 的原生读写规则关闭。未通过权限/参数检查的请求记入结构化请求日志；实际取消必须有持久审计。备份应同时保留数据库及应用版本，迁移不支持丢弃审计证据的向下回滚。

审计确认的是“当前作业已为 CANCELLED”，不能证明该状态仅由本次操作造成。作业退出控制器保留范围、配置改指其他集群或读取失败时不会凭空推断成功。完整平台审计检索、保留策略和历史 SlurmDB 记账在后续阶段扩展。

平台审计查询可通过 `platform.audit.read` 检索跨集群历史，不改变原有本人操作查询边界，详见 [操作审计](audits.md)。

作业页已支持本页最多十个明确作业的批量取消，共用相同身份、状态和逐项审计规则，不开放 Slurm 批量选择器。见 [批量操作](bulk-operations.md)。

详情新增“诊断作业”，按需解释当前状态和原因，保留证据与下一步核查；见 [作业异常诊断](job-diagnostics.md)。
