# 权限实施约定

已实现平台身份认证、动作级 RBAC 和集群范围隔离。设计系统中的确认弹窗仍是展示组件，不能用作安全边界。

授权集合为 `roles`、`permissions`、`role_permissions`、`user_platform_roles`、`user_cluster_roles`。`clusters` 目前只保存授权所需的非敏感标识。所有这些集合的原生 API 规则为 locked，平台用户即使有 Super Admin 角色，也不能绕过应用 API 写 Collection。

| 角色代码 | 范围 | 初始动作 |
| --- | --- | --- |
| `super_admin` | 平台 | 显式列出的平台管理与所有集群动作 |
| `cluster_admin` | 指定集群 | 该集群全部已定义动作 |
| `operator` | 指定集群 | 读取，以及 `jobs.cancel`、`nodes.drain`、`nodes.resume` |
| `viewer` | 指定集群 | 只读 |

平台动作包括 `platform.users.manage`、`platform.roles.manage`、`platform.clusters.manage`、`platform.audit.read`。集群动作包括 `clusters.read`、`jobs.read/cancel/requeue`、`nodes.read/drain/resume`、`partitions.read`、`accounts.read/update`、`qos.read/update`、`associations.read/update`、`linux_users.read` 和 `metrics.read`。这里只定义权限，尚未实现的业务动作不会因为有权限而自动开放。

`rbac.Resolve` 每次从数据库解析角色绑定和动作，按角色的 platform/cluster 范围过滤，不判断角色名字来放行。未知动作默认拒绝，集群角色不能授予平台权限。新增动作需要显式绑定到角色，Super Admin 也不使用通配符隐式获得未来动作。

新平台用户默认无授权。`/auth/me` 只返回本人角色与动作；`GET /clusters` 只分页返回有 `clusters.read` 的集群；详情接口同时要求活动平台用户和对应集群的 `clusters.read`。其他用户的授权不在当前用户响应中出现。撤销授权下一请求立即生效，前端隐藏/显示入口不是安全校验。

可信操作员使用 `access grant/revoke` 管理绑定，使用方法见 [认证与首次授权](authentication.md)。目前不提供通过浏览器写授权的接口。

平台用户、Linux/Slurm 身份和角色分别建模。作业取消已同时校验平台动作、目标资源范围及 Slurm 端权限；后续控制动作沿用此边界。Operator 的动作集合不意味着可以任意代理 Linux 用户。

真实状态变更必须验证身份、集群、资源和操作范围，记录操作人、目标、时间、请求标识及结果；高风险操作使用二次确认。普通用户不得修改审计数据。

集群配置页面及 `/api/v1/cluster-configurations`、`/cluster-connection-profiles` 全部要求平台范围 `platform.clusters.manage`，不会通过集群范围角色获得。停用连接保留现有权限绑定；配置引用只能选取可信部署管理员预设的连接方式。创建、编辑和停用与持久审计同事务；检测和请求上下文继续记录至 journald。

Adapter 的健康、作业、节点、分区端点分别要求对应集群的 `clusters.read/jobs.read/nodes.read/partitions.read`，命中缓存也先检查实时授权。所有上游查询使用同一预配置受限身份，不按浏览器提供的用户名代理。单作业取消另需显式身份关联、最新确认和持久审计，见 [作业管理](jobs.md)。

集群总览要求集群范围 `clusters.read`，并单独检查作业、节点、分区读取权限；缺少单项权限不访问该来源，响应明确返回 `forbidden/item=null`。总览不会把集群可见权限扩展为所有业务数据的读取权限。

节点 Drain/Resume 要求 `nodes.read` 和目标动作，使用明确节点名称、最新状态确认及持久审计。Slurm 仍校验固定服务身份的上游管理员权限，平台不自动扩权；节点与作业审计接口按资源类型隔离。详见 [节点管理](nodes.md)。

分区列表/详情只要求 `partitions.read`；关联节点统计及节点分页独立要求 `nodes.read`，缺权不请求节点。分区读取权限不隐含作业或节点权限，见 [分区查询](partitions.md)。

`GET /operation-audits` 及详情要求平台范围 `platform.audit.read`，可检索已停用/删除集群的历史；不依赖实时资源权限，也不授予资源操作权限。原生审计 Collection CRUD 仍锁定。详见 [操作审计](audits.md)。

Linux 身份列表及精确详情要求对应集群的 `linux_users.read`，来源访问前和返回数据前分别复查权限。该权限不包含账户创建、密码锁定/解锁、平台账户或 Slurm Association 管理；内置四类角色显式加入读取动作，自定义角色不会隐式获得。见 [Linux 系统用户查询](linux-users.md)。

本机 Linux 账户写操作独立要求 `linux_users.create/lock/unlock`，显式绑定 Super Admin/Cluster Admin，Operator/Viewer 不获得这些动作。系统管理员还须启用本机来源及 root UID 策略；集群权限不会绕过主机保护名单、账户来源或到期状态检查。写前确认、持久审计及主机幂等日志共同控制执行边界。
