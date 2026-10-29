# API 实施约定

`api/openapi.yaml` 记录已实现的认证、集群授权查询和集群连接配置契约。后续业务实现必须同步扩展契约。

- 所有业务请求统一走 Go 的 `/api/v1`，浏览器不直接访问 slurmrestd。
- 错误统一包含业务错误码、信息和请求标识。
- 作业、节点等大量记录必须使用服务端分页、筛选；设计系统中的内存表格仅用于展示。
- 各集群的凭据、API 版本、超时和能力探测留在服务端。
- 状态变更请求不自动重试，确认结果后向界面反馈。

已实现 `GET /api/v1/health`、`GET /api/v1/auth/me`、`GET /api/v1/clusters` 和 `GET /api/v1/clusters/{clusterId}`。集群列表只返回授权范围，分页 `page >= 1`、`1 <= perPage <= 100`。当前集群只返回非敏感标识，不返回连接状态或凭据。

平台自定义成功响应为 `{ data, requestId }`，错误为 `{ error: { code, message }, requestId }`，响应头包含 `X-Request-ID`；会话和权限响应使用 `Cache-Control: no-store`。未认证返回 401，无动作或集群权限返回 403，未知资源返回 404，分页非法返回 400。

认证例外：密码登录和刷新复用 PocketBase 原生 `/api/collections/users/auth-with-password`、`/auth-refresh`，保留官方 SDK 需要的 `{ token, record }` 与原生错误格式。原生接口同样附带 `X-Request-ID`。不存在服务端 logout 端点，退出通过清空 SDK 会话和缓存实现。

集群配置管理接口要求平台范围 `platform.clusters.manage`：`GET /cluster-connection-profiles`、`GET/POST /cluster-configurations`、`PATCH /cluster-configurations/{clusterId}`、`POST /cluster-configurations/{clusterId}/test` 和 `/disable`。列表采用相同分页约束；POST/PATCH 为完整配置（新建 revision=0），test/disable 只接受 revision。拒绝未知字段和任意 endpoint/凭据输入，配置冲突或检测繁忙返回 409 `CONFLICT`。Slurm 检测失败仍以 200 返回结构化 `lastProbe`；详细状态见 OpenAPI 和 [部署说明](cluster-connections.md)。

集群只读业务查询已通过 Slurm Adapter 实现：`GET /clusters/{clusterId}/health`、`/jobs`、`/jobs/{jobId}`、`/nodes`、`/nodes/{nodeName}`、`/partitions`。它们按对应集群动作鉴权，返回统一 DTO 与缓存时间，不暴露原始 Slurm 响应；接口、分页、筛选和错误码详见 [Adapter 契约](slurm-adapter.md)。

`GET /clusters/{clusterId}/overview` 返回来源级的健康、作业、节点、分区汇总；各项有 `status/item/code/message/fetchedAt/cached`，失败或缺权为 null，其他数据仍可用。接口不接受列表筛选参数，避免将过滤后的子集误当集群总量；详细口径见 [集群总览](overview.md)。

Linux 系统用户查询为 `GET /clusters/{clusterId}/linux-users` 及 `/linux-users/{username}`，独立使用 `linux_users.read`。身份来源只能来自部署管理员的配置，浏览器不能指定 SSH 目标或命令；枚举与精确查找范围不同，见 [Linux 用户契约](linux-users.md)。

记账身份读取和确认操作端点见 [Account 与 Association](accounting.md)，对接固定 slurmdbd v0.0.43。全局身份变更需独立平台权限，关联操作按实际集群隔离；已发起写操作没有自动重试路径。
