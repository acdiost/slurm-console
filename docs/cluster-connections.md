# 集群连接配置

平台设置 → **集群配置**（`/clusters`）支持服务端分页、新建、编辑、启停、连接检测和结果持久化。要求活动用户具备平台范围的 `platform.clusters.manage`；Cluster Admin、Operator、Viewer 无法读取连接详情或修改配置。普通授权查询 `/api/v1/clusters` 仍只返回 `id/name/slug/description`。

## 连接方式与凭据

可信部署管理员先创建服务端 JSON，再通过 `SLURM_CONSOLE_CLUSTER_PROFILES` 指定其绝对路径。模板见 [profiles.example.json](../deploy/cluster-profiles.example.json)。删除不用的示例条目，填写实际 endpoint、允许的 API 版本及凭据文件路径。配置文件不允许符号链接或组/其他用户写入；推荐 `root:root 0640`，父目录由 root 控制。配置错误会阻止启动；不设置环境变量则页面显示暂无连接方式。

支持两种连接：

- **本机 Unix socket + `rest_auth/local`**：`endpoint` 使用 `unix:///run/slurm-console-rest/rest.sock`，`authType` 为 `local`。root 平台通过一次性子进程以专用 `slurm-console` 普通 UID 访问，与 slurmrestd 保持同一对端身份，socket 所在目录设为该用户独占的 `0700`。不配置 JWT、远程用户名或 CA。可在可信 profile 设置 `localUser` 指定匹配 socket 的专用非 root 身份，默认 `slurm-console`；该字段不接受浏览器修改，也不用于 HTTPS/JWT。slurmrestd 由系统管理员独立安装和配置，本项目不提供其服务模板。禁止使用 root 或 Slurm 守护进程用户运行 REST 服务。
- **远程 HTTPS + JWT**：`authType` 为 `jwt`，设置固定 `username`、绝对 `tokenFile`，可选私有 `caFile`。只接受 HTTPS origin，无路径、查询、URL 用户信息或片段；验证证书和主机名，TLS 最低 1.2。JWT 文件只允许服务用户读取（推荐 `root:root 0640`），每次检测重新读取，支持原子替换轮换。JWT 的签发、续期和目标 Slurm 权限由可信管理员负责，不会自动提权或续期。

浏览器只选择已批准的连接引用，不接收任意 endpoint 或凭据。数据库保存引用、版本、超时、启停及检测结果，不保存 JWT；API 不返回 JWT、用户名、凭据路径或原始上游响应。修改连接方式、CA 后需重启平台；JWT 文件轮换无需重启。凭据缺失会得到 `credentials_unavailable`，不会回退其他认证。

将 `SLURM_CONSOLE_CLUSTER_PROFILES=/etc/slurm-console/cluster-profiles.json` 加入应用统一环境文件 `/etc/slurm-console/slurm-console.env`，重启应用即可。slurmrestd 的安装、监听、认证、证书与 JWT 签发由系统管理员维护，详见 [外部组件要求](deployment.md)。本应用不会配置、安装或重启第三方服务。

## 页面使用

1. 以 Super Admin 登录，打开 `/clusters`，选择“新建集群”。
2. 填写显示名称、不可变标识、说明，选择连接方式和 API 版本，设置 1–30 秒超时及启用状态。
3. 保存后点击“检测”。成功显示 RESTD 版本、控制器名称、主备模式、UP/DOWN、耗时和检测时间。RESTD 版本来自响应元数据，不等同于控制器二进制版本。
4. 编辑后旧结果重置；停用需确认，保留集群 ID 和原有授权。连接方式被删除后仍可停用。启用连接在每次服务启动时进行一次检查，页面不轮询 Slurm。

检测固定请求 `GET /slurm/{apiVersion}/ping`，当前支持 `v0.0.41`–`v0.0.44` 中部署配置明确允许的版本。要求返回 parser 与配置一致、有效 pings 和版本元数据；不支持的版本不会自动降级。失败区分不可达、超时、TLS、认证、API 不兼容、响应异常、控制器 DOWN、凭据/连接方式不可用。结果表示上次检查状态，不是实时健康承诺。

## 并发、升级和范围

保存、停用和检测携带 `revision`；配置变更导致旧请求返回 409，进行中的检测不能覆盖新配置。同集群检测互斥，全局最多 4 个，5 秒内复用已有检测结果；无重试、无 HTTP 重定向、无环境代理，响应上限 8 MiB。审计上下文写入 slog/journald（操作者、请求 ID、动作、集群、配置版本及结果）；完整可检索操作审计见[操作审计](audits.md)。

迁移为既有 `clusters` 扩展连接字段，旧标识默认停用且不主动连接。编辑原记录即可接入连接方式并保留授权。CLI `access register-cluster` 继续只登记标识。升级前停止平台并做一致性备份；迁移不提供破坏性的自动 down，回退应同时恢复备份数据库及匹配的旧程序。

当前已实现连接检测和 [Slurm Adapter](slurm-adapter.md) 的健康、作业、节点、分区只读 API。连接检测允许已配置的 `v0.0.41`–`v0.0.44`；业务 Adapter 明确限定已验证的 `v0.0.43`。设计系统作业仍为示例，业务查询不落地 PocketBase。[作业管理](jobs.md) 已增加受身份、确认和审计保护的单作业取消；操作证据保存在平台数据库。

依据：[Slurm REST 服务文档](https://slurm.schedmd.com/slurmrestd.html)、[REST 客户端指南](https://slurm.schedmd.com/rest_clients.html)。

连接创建、更新及停用与操作审计在同一数据库事务提交，写审计失败则回滚配置；查询审计不调用上游连接。见 [操作审计](audits.md)。

多集群工作台见[多集群管理](multi-cluster.md)。普通授权列表 `GET /api/v1/clusters` 支持 `page/perPage/q`；`q` 对名称、标识、描述做字面匹配，最多 128 个 Unicode 字符且 512 字节，不支持通配符，查询前按 `clusters.read` 隔离范围。不接受重复或未知参数。

记账功能要求连接同时启用 slurmctld、slurmdbd 内容插件，管理身份及其权限由系统管理员配置，见 [Account 与 Association](accounting.md)。
