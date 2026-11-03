# 可选 AI 运维辅助

当前作业和历史作业的规则诊断下方提供 AI 解释入口。首版支持管理员批准的 **Chat Completions 兼容服务**，先预览完整请求，再逐次确认发送，结果为未经人工核实的只读建议。不会执行命令、调用工具、控制 Slurm、追加读取其他来源或自动重试；原有规则诊断独立可用。

本次完成配置接入、合成数据隔离验证与默认关闭部署。真实模型服务、凭据及其输出质量尚未验证，待管理员后续配置。项目不安装或托管模型服务。

## 管理员配置

正式环境默认关闭：未设置 `SLURM_CONSOLE_AI_CONFIG` 或 JSON 中 `enabled:false` 时，不调用模型。复制 [配置示例](../deploy/ai.example.json) 到 `/etc/slurm-console/ai.json`，按实际服务填写以下字段。

| 字段 | 要求 |
|---|---|
| enabled | 明确设为 true 才启用 |
| label / model | 服务显示名称及管理员批准的模型名；无默认模型 |
| endpoint | 完整且固定的 HTTPS `/chat/completions` 地址；无用户名、密码、查询或 fragment |
| clusterIds | 明确允许发送证据的集群 ID 列表，1–100 个，不接受通配符 |
| apiKeyFile | 可选密钥文件的绝对路径；只读 ASCII token，文件 0600，禁止符号链接 |
| caFile | 可选受信任 CA PEM 文件绝对路径；始终校验证书和主机名 |
| timeoutSeconds | 默认 30，5–60 秒 |
| maxOutputTokens | 默认 2048，512–4096，包含服务定义的输出预算 |
| tokenParameter | 默认 max_completion_tokens；旧兼容服务可显式选择 max_tokens |

JSON/CA 文件须是绝对路径的常规文件，不能被 group/other 写入；建议配置目录 root:root 0700、配置 0600。密钥直接在服务器创建，不放入命令参数、仓库或浏览器；每次发送重新读取密钥，修改密钥文件无需重启。其他配置启动时读取，修改后须重启应用。非法启用配置使应用启动失败，阻止带病发送。

```sh
# 在服务器修改配置和密钥文件后：
chown root:root /etc/slurm-console/ai.json /etc/slurm-console/secrets/ai.key
chmod 0600 /etc/slurm-console/ai.json /etc/slurm-console/secrets/ai.key
# /etc/slurm-console/slurm-console.env 中加入：
# SLURM_CONSOLE_AI_CONFIG=/etc/slurm-console/ai.json
systemctl restart slurm-console
```

只允许 HTTPS，唯一例外是无凭据的字面回环 IP HTTP（如 `http://127.0.0.1:18093/v1/chat/completions`），用于同机服务或隔离验证；不允许 HTTP 域名、HTTP 密钥或 HTTP 自定义 CA。不跟随重定向，不读取系统 HTTP 代理环境变量。模型、地址与密钥不能由前端请求覆盖。

请求使用 system/user 两条消息、JSON object 输出模式、`stream:false`、`store:false` 和配置的 token 上限，协议参考 [OpenAI Chat Completions](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)。兼容服务必须实现这些字段及 choices/message/finish_reason 结构；拒绝不支持的参数时不自动降级或重试。`store:false` 是请求要求，不能保证第三方服务不留存请求，管理员仍须核对其政策。

## 权限与数据范围

新增集群权限 `ai.explain`，默认绑定 Super Admin、Cluster Admin、Operator；Viewer 无权发送。每次预览和发送还要求对应的 `jobs.read` 或 `job_history.read`、活动账户和启用的集群。配置中的集群允许列表与平台角色授权必须同时满足。返回前复查权限和配置修订；请求已经发送后发生撤权时丢弃结果并保留审计，不能撤回已发送内容。

服务端按原诊断路径重新读取或复用现有有界缓存，不接收浏览器上传的任意提示或报告。当前来源仅为精确作业 ID；历史来源必须同时指定精确 recordKey、Job ID 和最多 31 天提交窗口，不合并重复 Job ID，不使用当前记录兜底。

发送正文只含：白名单 Slurm 状态、归一化等待原因组、非负有限的时间/CPU 数值、步骤数量、静态规则提示与固定证据边界。未知字符串映射为 UNKNOWN/unknown。**不发送** Job ID、记录标识、集群标识、用户/账户/分区/QOS/节点名、作业名、原始原因文本、自由描述、退出细节、日志、脚本、环境、凭据或来源时间戳。服务端请求观测时间仅用于本地预览与结果标记。数值本身仍可能反映负载情况，必须逐次确认发送。

页面展示完整请求正文、接收地址、模型、证据观测时间、到期时间与 SHA-256 摘要。勾选确认并输入 `SEND AI` 后才发送；取消、只看预览或刷新均不调用模型。发送精确预览正文，不声称这是发送瞬间的最新实时状态。

## 预算、结果与审计

- 请求正文最多 16 KiB；响应最多 128 KiB；内容最多 24 KiB。输出须包含 summary、observations、checks、limitations，每组 1–4 条，文本最多 300 字；解释和核查引用 1–5 个已发送的事实 ID。
- 拒绝未知字段、重复 JSON 键、未知/重复引用、工具调用、拒答、截断、不完整和超预算响应。不解析 Markdown/HTML，页面以纯文本显示；结构校验不保证语义正确，用户须人工核实。
- 内存预览有效 2 分钟，全局最多 64 条、每用户 8 条；绑定用户、集群、来源、目标和配置修订。预览消费一次后不能重放；应用重启即失效。
- 全局最多 2 个模型请求并发，每用户 1 个且两次消费至少间隔 30 秒。无后台队列、自动轮询、自动重试或共享答案缓存。失败后须重新预览确认，不提供原请求重试按钮。
- 发送前必须持久化 `ai.explain` 意图；审计不可写时不发送。审计保存操作者、目标、请求标识、配置修订、服务/模型名称、正文摘要和结果，**不保存提示、证据正文、答案或密钥**。
- `succeeded` 只表示收到通过结构校验的建议，内容未核实；发送前失败为 `failed`；网络/模型异常、发出后撤权为 `unknown`。终态审计写入失败时丢弃答案，保留 processing 意图（审计页显示待核实），绝不自动重发。错误返回操作编号和请求标识供管理员查询。
- 答案只在当前页面内存中显示；收起、刷新或切换用户/集群/诊断会清除。审计访问仍需要 `platform.audit.read`。

## API 与验证

OpenAPI 0.30.0 定义四条接口：

```http
GET  /api/v1/clusters/{clusterId}/ai-assistance?source=controller|accounting
POST /api/v1/clusters/{clusterId}/jobs/{jobId}/diagnosis/ai-preview
POST /api/v1/clusters/{clusterId}/job-history/{recordKey}/diagnosis/ai-preview?jobId=...&start=...&end=...
POST /api/v1/clusters/{clusterId}/ai-assistance/{previewId}/explain
```

预览请求体为 `{}`；发送请求体为 `{ "confirmation":"SEND AI", "digest":"<预览摘要>" }`。不接受扩展字段或重复/未知查询参数；默认关闭返回 enabled:false，预览拒绝发送。没有原生模型配置 CRUD、新的答案 Collection 或客户端密钥接口。

单元及接口回归覆盖隐私投影、任意来源文本注入、TLS/密钥轮换、无重定向、预算/到期/并发、一次性消费、来源权限独立、历史精确记录、审计故障和请求后撤权；前端覆盖关闭状态、完整预览、确认门槛、取消、到期、失败不重试、纯文本展示和身份切换。虚拟机验证仅使用独立数据库、合成作业和回环模拟服务，正式 AI 保持关闭，真实服务和质量验收待配置。

2026-11-03 验收：前端 197 项测试、类型检查、lint、生产构建及后端全量竞态回归、go vet 通过；历史来源和权限用例另通过针对性竞态检查。OpenAPI 101 个操作编号唯一且引用完整。虚拟机浏览器确认预览零调用、确认后仅一次模型调用，实际正文 SHA-256 与审计一致，隐私标记未进入请求，建议以纯文本显示并保留人工核查提示。

平台设置现支持保存和热更新非敏感 AI 配置。数据库覆盖优先于服务器 JSON 中的非敏感字段，密钥/CA 文件引用继续由服务器提供；保存使旧预览失效并中止旧请求，不调用模型。优先级、权限和操作流程见 [平台设置](platform-settings.md)。
