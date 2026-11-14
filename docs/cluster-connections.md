# 集群连接配置

平台设置 → **集群配置**（`/clusters`）支持服务端分页、新建、编辑、启停、连接检测和结果持久化。要求活动用户具备平台范围的 `platform.clusters.manage`；Cluster Admin、Operator、Viewer 无法读取连接详情或修改配置。普通授权查询 `/api/v1/clusters` 仍只返回 `id/name/slug/description`。

## 连接方式与凭据

可信部署管理员先创建服务端 JSON，再通过 `SLURM_CONSOLE_CLUSTER_PROFILES` 指定其绝对路径。模板见 [profiles.example.json](../deploy/cluster-profiles.example.json)。删除不用的示例条目，填写实际 endpoint、允许的 API 版本及凭据文件路径。配置文件不允许符号链接或组/其他用户写入；推荐 `root:root 0640`，父目录由 root 控制。配置错误会阻止启动；不设置环境变量则页面显示暂无连接方式。

集群业务通过 slurmrestd 通讯，优先使用远程 HTTPS + JWT；也支持同机 Unix socket。支持两种连接：

- **本机 Unix socket + `rest_auth/local`**：`endpoint` 使用 `unix:///run/slurm-console-rest/rest.sock`，`authType` 为 `local`。root 平台直接以 root 访问 socket；终端向导保存 `localUser: "root"`。不配置 JWT、远程用户名或 CA。旧 `localUser` 字段保留读取兼容，不再选择其他请求身份；该字段不接受浏览器修改，也不用于 HTTPS/JWT。slurmrestd 的服务身份、socket 权限及 root 客户端准入由系统管理员配置，本项目不安装或修改该外部服务。
- **远程 HTTPS + JWT**：`authType` 为 `jwt`，设置固定 `username`、绝对 `tokenFile`，可选私有 `caFile`。只接受 HTTPS origin，无路径、查询、URL 用户信息或片段；验证证书和主机名，TLS 最低 1.2。JWT 文件只允许服务用户读取（推荐 `root:root 0640`），每次检测重新读取，支持原子替换轮换。JWT 的签发、续期和目标 Slurm 权限由可信管理员负责，不会自动提权或续期。

浏览器只选择已批准的连接引用，不接收任意 endpoint 或凭据。数据库保存引用、版本、超时、启停及检测结果，不保存 JWT；API 不返回 JWT、用户名、凭据路径或原始上游响应。修改连接方式、CA 后需重启平台；JWT 文件轮换无需重启。凭据缺失会得到 `credentials_unavailable`，不会回退其他认证。

将 `SLURM_CONSOLE_CLUSTER_PROFILES=/etc/slurm-console/cluster-profiles.json` 加入应用统一环境文件 `/etc/slurm-console/slurm-console.env`，重启应用即可。slurmrestd 的安装、监听、认证、证书与 JWT 签发由系统管理员维护，详见 [外部组件要求](deployment.md)。本应用不会配置、安装或重启第三方服务。

## 页面使用

1. 以 Super Admin 登录，打开 `/clusters`，选择“新建集群”。
2. 填写显示名称、不可变标识、说明，选择连接方式，点击“检测可用 API 版本”。页面从 slurmrestd 的 OpenAPI 文档中列出完整查询接口与服务端允许版本的交集；保留仍可用的当前选择，否则选择其中最高版本。也可手动选择版本，设置 1–30 秒超时及启用状态。
3. 保存后点击“检测”。成功显示 RESTD 版本、控制器名称、主备模式、UP/DOWN、耗时和检测时间。RESTD 版本来自响应元数据，不等同于控制器二进制版本。
4. 编辑后旧结果重置；停用需确认，保留集群 ID 和原有授权。连接方式被删除后仍可停用。启用连接在每次服务启动时进行一次检查，页面不轮询 Slurm。

保存后的连接检测固定请求 `GET /slurm/{apiVersion}/ping`，当前支持 `v0.0.40`–`v0.0.44`。要求返回 parser 与配置一致、有效 pings 和版本元数据；不支持的版本不会自动降级。失败区分不可达、超时、TLS、认证、API 不兼容、响应异常、控制器 DOWN、凭据/连接方式不可用。结果表示上次检查状态，不是实时健康承诺。

## 多版本接入

终端 `slurm-console configure` 的 API 版本可输入 `auto`，按上述规则选择最高可用版本；回车保留已有值，新连接默认 `v0.0.43`。也接受 `0.0.40` 等省略 `v` 的输入，保存为规范版本名。每个集群独立选择版本，所有业务读写固定使用该版本，不因失败而切换版本或重发写请求。

受保护连接配置的 `apiVersions` 可省略，表示允许平台支持的全部五个版本；显式数组则为白名单。升级不会扩大既有显式白名单；如果旧配置只允许 `v0.0.43`，需修改配置并重启后才能选择其他版本。

| 能力 | v0.0.40–41 | v0.0.42 | v0.0.43–44 |
|---|---|---|---|
| 控制器、作业、节点、分区及记账查询 | 支持 | 支持 | 支持 |
| 作业取消、节点控制及账户/QOS 基础管理 | 支持 | 支持 | 支持 |
| QOS 累计 TRES 分钟限额（GrpTRESMins） | 上游未提供 | 支持 | 支持 |
| Reservation 查询 | 支持 | 支持 | 支持 |
| Reservation 新建、调整、删除 | 平台仅开放查询 | 平台仅开放查询 | 支持 |

版本受限的能力在界面标识，服务端也会在写入前拒绝。旧版 Reservation 不具备当前完整写入契约，平台统一仅开放查询；即使新 Slurm 为旧 parser 补充个别删除端点，也不单独开放。记账功能仍要求启用 `openapi/slurmdbd` 并具备相应 Slurm 权限。API 版本不是 Slurm 发布版本；目标 slurmrestd 必须加载对应 `data_parser`，平台不能为服务端补装旧版本插件。

版本发现接口为 `GET /api/v1/cluster-connection-profiles/{ref}/versions`，要求平台集群管理权限；只读取批准连接的 `/openapi/v3`，仅在 404 时尝试 `/openapi.json`。文档上限 16 MiB、10000 个路径，整个发现操作最多 5 秒；缺少 OpenAPI 文档时可手动选择版本并做连接检测。发现仅确认核心 GET 路径存在，实际业务仍验证响应版本、字段、权限和错误，不能替代完整业务验收。

## 并发、升级和范围

保存、停用和检测携带 `revision`；配置变更导致旧请求返回 409，进行中的检测不能覆盖新配置。同集群检测互斥，全局最多 4 个，5 秒内复用已有检测结果；无重试、无 HTTP 重定向、无环境代理，响应上限 8 MiB。审计上下文写入 slog/journald（操作者、请求 ID、动作、集群、配置版本及结果）；完整可检索操作审计见[操作审计](audits.md)。

迁移为既有 `clusters` 扩展连接字段，旧标识默认停用且不主动连接。编辑原记录即可接入连接方式并保留授权。CLI `access register-cluster` 继续只登记标识。升级前停止平台并做一致性备份；迁移不提供破坏性的自动 down，回退应同时恢复备份数据库及匹配的旧程序。

连接检测与 [Slurm Adapter](slurm-adapter.md) 业务接口统一支持 `v0.0.40`–`v0.0.44`。设计系统作业仍为示例，业务查询不落地 PocketBase。[作业管理](jobs.md) 提供受身份、确认和审计保护的单作业取消；操作证据保存在平台数据库。

依据：[Slurm REST 服务文档](https://slurm.schedmd.com/slurmrestd.html)、[REST 客户端指南](https://slurm.schedmd.com/rest_clients.html)。

连接创建、更新及停用与操作审计在同一数据库事务提交，写审计失败则回滚配置；查询审计不调用上游连接。见 [操作审计](audits.md)。

多集群工作台见[多集群管理](multi-cluster.md)。普通授权列表 `GET /api/v1/clusters` 支持 `page/perPage/q`；`q` 对名称、标识、描述做字面匹配，最多 128 个 Unicode 字符且 512 字节，不支持通配符，查询前按 `clusters.read` 隔离范围。不接受重复或未知参数。

记账功能要求连接同时启用 slurmctld、slurmdbd 内容插件，管理身份及其权限由系统管理员配置，见 [Account 与 Association](accounting.md)。

新建、编辑和停用前可预览服务端生成的七字段差异，确认后保存；实际写入仍独立复查权限和 revision。列表的“版本对比”进入上线基线与历史快照，只读比较，不恢复配置或记录凭据文件，见 [配置变更对比](configuration-history.md)。
