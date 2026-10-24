# API 实施约定

`api/openapi.yaml` 记录已实现的认证和集群授权查询契约。后续业务实现必须同步扩展契约。

- 所有业务请求统一走 Go 的 `/api/v1`，浏览器不直接访问 slurmrestd。
- 错误统一包含业务错误码、信息和请求标识。
- 作业、节点等大量记录必须使用服务端分页、筛选；设计系统中的内存表格仅用于展示。
- 各集群的凭据、API 版本、超时和能力探测留在服务端。
- 状态变更请求不自动重试，确认结果后向界面反馈。

已实现 `GET /api/v1/health`、`GET /api/v1/auth/me`、`GET /api/v1/clusters` 和 `GET /api/v1/clusters/{clusterId}`。集群列表只返回授权范围，分页 `page >= 1`、`1 <= perPage <= 100`。当前集群只返回非敏感标识，不返回连接状态或凭据。

平台自定义成功响应为 `{ data, requestId }`，错误为 `{ error: { code, message }, requestId }`，响应头包含 `X-Request-ID`；会话和权限响应使用 `Cache-Control: no-store`。未认证返回 401，无动作或集群权限返回 403，未知资源返回 404，分页非法返回 400。

认证例外：密码登录和刷新复用 PocketBase 原生 `/api/collections/users/auth-with-password`、`/auth-refresh`，保留官方 SDK 需要的 `{ token, record }` 与原生错误格式。原生接口同样附带 `X-Request-ID`。不存在服务端 logout 端点，退出通过清空 SDK 会话和缓存实现。
