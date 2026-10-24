# 权限实施约定

已实现平台身份认证、动作级 RBAC 和集群范围隔离。设计系统中的确认弹窗仍是展示组件，不能用作安全边界。

授权集合为 `roles`、`permissions`、`role_permissions`、`user_platform_roles`、`user_cluster_roles`。`clusters` 目前只保存授权所需的非敏感标识。所有这些集合的原生 API 规则为 locked，平台用户即使有 Super Admin 角色，也不能绕过应用 API 写 Collection。

| 角色代码 | 范围 | 初始动作 |
| --- | --- | --- |
| `super_admin` | 平台 | 显式列出的平台管理与所有集群动作 |
| `cluster_admin` | 指定集群 | 该集群全部已定义动作 |
| `operator` | 指定集群 | 读取，以及 `jobs.cancel`、`nodes.drain`、`nodes.resume` |
| `viewer` | 指定集群 | 只读 |

平台动作包括 `platform.users.manage`、`platform.roles.manage`、`platform.clusters.manage`、`platform.audit.read`。集群动作包括 `clusters.read`、`jobs.read/cancel/requeue`、`nodes.read/drain/resume`、`partitions.read`、`accounts.read/update`、`qos.read/update`、`associations.read/update` 和 `metrics.read`。这里只定义权限，尚未实现的业务动作不会因为有权限而自动开放。

`rbac.Resolve` 每次从数据库解析角色绑定和动作，按角色的 platform/cluster 范围过滤，不判断角色名字来放行。未知动作默认拒绝，集群角色不能授予平台权限。新增动作需要显式绑定到角色，Super Admin 也不使用通配符隐式获得未来动作。

新平台用户默认无授权。`/auth/me` 只返回本人角色与动作；`GET /clusters` 只分页返回有 `clusters.read` 的集群；详情接口同时要求活动平台用户和对应集群的 `clusters.read`。其他用户的授权不在当前用户响应中出现。撤销授权下一请求立即生效，前端隐藏/显示入口不是安全校验。

可信操作员使用 `access grant/revoke` 管理绑定，使用方法见 [认证与首次授权](authentication.md)。目前不提供通过浏览器写授权的接口。

平台用户、Linux/Slurm 身份和角色分别建模。后续实际控制 Slurm 时，必须同时校验平台动作、目标资源范围及 Slurm 端权限；Operator 的动作集合不意味着可以任意代理 Linux 用户。

真实状态变更必须验证身份、集群、资源和操作范围，记录操作人、目标、时间、请求标识及结果；高风险操作使用二次确认。普通用户不得修改审计数据。

集群配置页面及 `/api/v1/cluster-configurations`、`/cluster-connection-profiles` 全部要求平台范围 `platform.clusters.manage`，不会通过集群范围角色获得。停用连接保留现有权限绑定；配置引用只能选取可信部署管理员预设的连接方式。创建、编辑、检测和停用的操作上下文记录至 journald，完整操作审计仍待实现。
