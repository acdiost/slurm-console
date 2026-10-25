# Slurm Adapter

本阶段提供独立于 PocketBase 的只读 Slurm Client/Adapter、统一 DTO、带权限校验的集群 API，以及内存缓存和并发控制。业务查询明确支持 `data_parser/v0.0.43`；不自动猜测格式或降级。连接检测已有的 `v0.0.41`–`v0.0.44` 支持不代表业务 Adapter 已验证这些版本。

## 端点

所有端点位于 `/api/v1/clusters/{clusterId}`，要求活动平台用户及对应集群动作权限；鉴权发生在缓存查询之前，撤销权限下一次请求生效。

| GET 路径 | 权限 | 结果 |
|---|---|---|
| `/health` | `clusters.read` | RESTD/API 版本、控制器、只读能力 |
| `/jobs` | `jobs.read` | 作业列表 |
| `/jobs/{jobId}` | `jobs.read` | 数字形式的实际 Job ID 详情 |
| `/nodes` | `nodes.read` | 节点列表 |
| `/nodes/{nodeName}` | `nodes.read` | 节点详情 |
| `/partitions` | `partitions.read` | 分区列表 |

列表在 Go 服务端执行过滤、确定性排序与分页，参数 `page>=1`、`1<=perPage<=100`。所有列表支持 `q` 和 `state`，作业额外支持精确 `user/partition`，节点支持精确 `partition`。状态匹配状态数组中的任一标记（例如 `MIXED,DRAIN` 的节点可用 `state=DRAIN` 选择）；查询仅接受单个状态。作业按数字 ID 降序，节点和分区按名称升序。重复或未知参数、非法资源标识在访问上游前拒绝。数组作业按实际任务 Job ID 查询，暂不接受 `123_4` 等组合选择器。

成功响应仍为 `{data,requestId}`。列表 `data={items,page,perPage,totalItems,fetchedAt,cached}`，详情与健康为 `data={item,fetchedAt,cached}`。时间为 UTC RFC3339。`cached` 表示本次使用了已完成的缓存，`fetchedAt` 表示数据获取完成时间。

## 模型与错误

`internal/slurm/client` 管理固定 GET 路径、Unix socket/HTTPS、JWT 文件读取、证书验证与响应上限；`internal/slurm/adapter` 解析已支持的 wire schema；`internal/slurm/models` 定义平台 DTO。连接检测复用同一 Client。不会返回原始脚本、环境变量、凭据、客户端身份元数据或原始上游错误正文。

- Job ID 使用十进制字符串，避免浏览器数值精度问题；保留完整状态数组及未来未知标记。
- `Number={value: integer|null,infinite:boolean}` 区分合法零、未设置和无限制，不经过 float64 中转。
- 作业包含归属、账户、分区、原因、节点、CPU/节点数量、分钟制时限、Unix 时间戳及数组标识。
- 节点容量和已分配内存以 MiB 展示，`freeMemoryMiB` 单独表示 OS 报告空闲内存，不等同于可调度余量；保留原始 GRES 文本，不把 GPU 数量当作利用率。
- 分区保留节点表达式、CPU/节点总量、分钟制默认/最大时限。缺失必需字段、重复 ID、非法数值或超过记录上限都拒绝，不用空列表/零容量伪装成功。

业务错误包含稳定错误码及安全信息，例如 `SLURM_TIMEOUT`、`SLURM_TLS_FAILED`、`SLURM_AUTH_FAILED`、`SLURM_API_UNSUPPORTED`、`SLURM_INVALID_RESPONSE`、`SLURM_UPSTREAM_ERROR`（503）；`SLURM_DISABLED`、`SLURM_CONFIGURATION_CHANGED`（409）；`SLURM_NOT_FOUND`（404）；`SLURM_BUSY`（429）。平台身份失败仍为 401/403；Slurm 服务身份认证失败不会退出平台会话。健康成功响应可包含 `controller_down`，明确区分 REST 可达与控制器正常。

## 缓存与边界

作业/详情和健康缓存 5 秒，节点/详情 10 秒，分区 15 秒。缓存按集群 ID、配置 revision、资源类型和标识隔离，最多 128 项且总序列化数据不超过 32 MiB；失败不缓存，过期不返回旧数据。相同查询合并，最多 4 个真实上游请求。单个浏览器取消只停止其等待，不取消其他调用者的共享查询；查询仍受服务端超时约束，应用退出会取消进行中的请求。

每次读取缓存前复查集群启用状态、当前版本和已批准连接方式；请求期间编辑或停用配置会丢弃旧结果。上游每次响应上限 8 MiB、列表最多 20000 项、超时 1–30 秒；不跟随重定向、不使用环境代理、不自动重试，不接收任意 URL、请求头或 Slurm 用户代理身份。当前 slurmrestd 列表不提供本项目所需的统一分页，因此有界读取上游后在 Go 中分页；超过容量明确失败，后续按集群规模扩展。

所有查询使用部署管理员配置的固定受限 Slurm 身份，平台范围权限不提升该身份的 Slurm 权限。上游 `PrivateData` 等策略决定可见范围；平台用户/Slurm 用户身份映射、取消作业、Drain/Resume 和完整操作审计在对应业务阶段实现，本阶段没有写入或任意代理端点。记账数据仍由后续 slurmdb Adapter 扩展。

slurmrestd、LDAP、FreeIPA 等外部服务由管理员负责；本项目只连接现有服务。版本选择依据 [Slurm REST 客户端指南](https://slurm.schedmd.com/rest_clients.html)，具体字段以管理员端点提供的版本化 OpenAPI 为准。

集群总览复用以上来源与权限边界；节点 DTO 补充 `effectiveCpus` 和 `gresUsed`，用于区分预留 CPU 以及 GPU 已分配数量。聚合口径见 [集群总览](overview.md)。
