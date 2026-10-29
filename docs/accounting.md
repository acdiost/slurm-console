# Account、Slurm 用户与 Association

`/accounts` 提供三个视图：Account、Slurm 记账用户及 Association，支持所选集群内的筛选、排序、分页、详情和关联导航。Linux 用户、平台登录账户和 Slurm 记账身份分别管理。

## 来源与集群边界

固定对接 v0.0.43 的 `slurmdbd` OpenAPI 插件。连接需同时开放控制器 `slurmctld` 接口，以取得实际 Slurm 集群名；平台集群记录 ID/slug 不能代替记账库的 cluster 名称。服务端按该名称查询并再次过滤 Association，再关联全局 Account/User。其他集群记录不返回；不在所选集群的默认 Account 名称隐藏为 `outside_cluster`。

所有读取均经过动作授权，并在返回数据前复查活动身份及权限。缓存按平台集群与连接版本隔离，TTL 60 秒；成功写入会使所有配置集群的记账缓存及进行中的旧查询失效。列表最多接受上游 20000 条记录、8 MiB 响应，超限明确失败。

用户默认 Account 依赖 `users/?with_assocs=true`，无该参数时部分实际版本返回空默认值。后端内部只保留确认与保护所需的全局元数据，不把原始关联、Coordinators、TRES 或 Slurm 响应直接交给浏览器。

## 已支持的操作

| 操作 | 范围和要求 |
| --- | --- |
| 创建 Account | 全局新名称、描述、组织；仅在所选集群 root 下建立 Account 层级 |
| 编辑 Account | 仅描述、组织；全局生效，不改名称、父级、额度或身份权限 |
| 创建 Slurm 用户 | 新全局用户名，普通 `None` 管理级别；在所选 Account 创建通用默认关联，不创建 Linux 或平台用户 |
| 创建 Association | 已存在的 Slurm 用户、当前集群 Account，以及可选的单个真实分区 |
| 移除 Association | 精确 ID 和完整目标元组；仅移除用户关联，保留 Account 层级及全局用户 |

新建 Account/User 使用小写名称。root、服务身份和保留名称受保护；不提供 Account/User 删除、管理员授予、默认账户修改或任意 Slurm JSON/CSV 代理。默认关联、默认 Account 下的关联、当前集群最后一个关联，以及有未结束作业的关联不可移除。未知或仍在清理的作业视作未结束。移除通用用户关联前，必须先逐一移除同 Account/User 的分区子关联，防止 Slurm 的层级删除影响多个目标。单目标 REST 若以 `ESLURM_DATA_AMBIGUOUS_MODIFY (9210)` 明确拒绝提交，记录为失败，不自动重试。

Account 描述/组织和 Slurm 用户默认账户属于共享 SlurmDBD 全局信息，因此创建 Account、编辑 Account、创建 Slurm 用户另需平台范围 `platform.accounting.manage`，默认仅绑定 Super Admin。Cluster Admin 只有关联操作权限，不会因管理一个集群获得全局身份修改权。

| 动作 | 平台权限 |
| --- | --- |
| Account 列表/详情 | `accounts.read` |
| Slurm 用户列表/详情 | `slurm_users.read`（内置四角色显式授予） |
| Association 列表/详情 | `associations.read` |
| Account 创建/编辑 | `accounts.read/update` + `platform.accounting.manage` |
| Slurm 用户创建 | `slurm_users.read` + `accounts.read` + `associations.read/update` + `platform.accounting.manage` |
| Association 创建 | `associations.read/update` + `accounts.read` + `slurm_users.read` |
| Association 移除 | `associations.read/update` + `jobs.read` |

平台授权和 Slurm 上游授权都必须满足。应用以 root 运行不会自动授予 SlurmDBD 权限。

## 确认、审计与恢复

先校验最新身份、关联、分区/作业和连接版本，再签发两分钟确认。执行时重新读取并比对目标快照、复查权限、保护策略与配置，先持久化意图审计，再发送一次固定写请求。确认绑定操作人、集群、完整字段、原因和操作编号，不能换人、换集群或更改内容重放。应用内记账变更串行执行；同一目标的未决操作阻止新写入。

显式上游拒绝记为 `failed`；成功且只读观察到目标状态为 `confirmed`；受理未核实为 `accepted`；断线/超时及写后审计失败为 `unknown`。未知结果绝不自动重发，通过 URL 的 `operation` 或本人最近记录只读核实。服务重启后，未决操作按审计内的固定 Spec 查询，不依赖旧确认密钥；结果表示观察到目标状态，不推断外部管理员并发操作的归因。

Slurm REST 不提供跨外部管理员的乐观并发事务；平台在发送前重新校验，管理员在维护期间仍应避免同时修改相同身份/关联。控制器或记账库读权限受限时，保护检查不足会拒绝执行，不把不可见数据解释为不存在。

## 部署前提

Slurm、SlurmDBD、slurmrestd、数据库和相应认证均由系统管理员安装、配置。仓库不提供这些组件或服务模板。管理员为现有 REST 服务启用 `slurmctld,slurmdbd` 内容插件以及 `v0.0.43` parser，并确保连接身份可以读取所需记账数据；写操作还须具有目标 SlurmDBD 的相应权限。仅控制器接口可用时，页面明确返回 `SLURM_ACCOUNTING_UNAVAILABLE`。

Unix socket 默认继续使用非 root `slurm-console` 对端身份。管理员也可在受保护的集群 profile 中设置 `localUser`，指定匹配该 socket 服务的专用非 root 本地用户；root 主服务以该 UID/GID 的一次性子进程请求，不继承 root 附加组。不允许浏览器指定该身份，HTTPS/JWT profile 禁止使用 `localUser`。这样可以将测试或记账管理连接与现有只读控制器连接分开。HTTPS/JWT 使用原有受保护令牌与 TLS 配置。

正式 Slurm 管理权限应由系统管理员根据本站策略显式配置，不由安装器、迁移或平台角色自动授予。

接口位于 `/api/v1/clusters/{clusterId}`：

- `GET /accounts`、`/accounts/{name}`
- `GET /slurm-users`、`/slurm-users/{name}`
- `GET /associations`、`/associations/{id}`
- `POST /accounting-controls/prepare`、`/accounting-controls/execute`
- `GET /accounting-operations`、`/accounting-operations/{operationId}`

分页沿用 `page/perPage`；列表支持 `q/sort/order`，Association 另支持精确 `account/user/partition` 和层级 `kind=account|user`。请求、响应及错误详见 [OpenAPI](../api/openapi.yaml)。官方记账语义参见 [Slurm Accounting](https://slurm.schedmd.com/accounting.html)，具体 wire 结构以目标实例生成的 v0.0.43 OpenAPI 为准。

## 实机验收

2026-10-29 在 Rocky Linux 9.7 / Slurm 25.11.8 / REST v0.0.43 上，通过独立应用数据库和独立临时 Slurm Administrator 连接完成写入验收：两个测试 Account 的创建、描述/组织修改、普通 Slurm 用户创建、通用及 normal 分区关联创建、按 ID 先移除分区再移除通用关联、默认关联与分区子关联保护、确认重放和持久审计均通过。测试用户、Account 和临时管理员均已清理。实测发现单目标删除通用关联可能涉及分区子关联，已补充应用层保护及 9210 明确拒绝分类。

按管理员明确授权，现有正式 `slurm-console` 连接身份已授予 Slurm Administrator，REST 已启用 slurmctld/slurmdbd 插件。该站点授权由管理员维护，不由应用安装器或迁移自动执行。原有控制器、SlurmDBD 进程及 Slurm、LDAP、PAM 配置保持一致。

正式连接另以独立应用数据库实测一次 Account 创建并核实成功，测试 Account 随后清理。服务器恢复时校正了 NTP 约 20 小时的偏差，PAM account 检查及定时备份补跑均通过。

QOS 全局策略管理复用本项确认及持久操作引擎，权限、字段保留及 Fairshare 查询见 [QOS / Fairshare](qos-fairshare.md)。本人操作列表按业务分类展示。

TRES 容量及分钟额度在 [TRES 限额](tres.md) 页面管理，Association 保留实际集群和完整身份边界。
