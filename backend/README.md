# 后端工程边界

当前完成目录和 Go module 初始化，尚未提供 HTTP 服务。下一项“PocketBase Go 框架与认证”将在这里实现应用入口、固定 PocketBase 版本和认证迁移。

- `internal/api`：平台 `/api/v1` 路由。
- `internal/auth`、`internal/rbac`：平台身份和集群范围授权。
- `internal/slurm/{client,adapter,models}`：独立的 Slurm 接口适配。
- `internal/services`：应用服务。
- `internal/monitoring`、`internal/audit`、`internal/cache`：监控、审计、缓存。
- `migrations`：PocketBase 数据迁移。

不在浏览器保存集群凭据；不将作业、节点持久化为平台业务主表。平台用户、Slurm 身份和集群权限分别建模。
