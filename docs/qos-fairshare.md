# QOS 与 Fairshare

`/policies` 提供 QOS 和 Fairshare 两个视图，保留跨业务页的集群选择，支持服务端筛选、排序、分页及详情。QOS 使用现有 `slurmdbd` REST 插件，Fairshare 使用控制器 `/slurm/v0.0.43/shares`，固定支持已验证的 v0.0.43。

## 查询与权限

QOS 定义属于共享记账库，可能供多个集群使用。普通集群角色只看到当前实际 Slurm 集群 Association 的 QOS 列表及默认 QOS 引用；不会展示其他集群引用或未分配的策略。平台目录权限允许查看该连接记账库的全部活动 QOS。响应 `catalogScope=referenced|global` 明确区分两种范围。

| 功能 | 权限 |
| --- | --- |
| QOS 列表、详情 | `qos.read` + `associations.read` |
| 完整 QOS 目录 | 上述权限 + `platform.qos.read` |
| QOS 新增、编辑 | `qos.read/update` + `platform.qos.read/manage` |
| Fairshare 列表、详情 | `fairshare.read` + `associations.read` |
| 本人 QOS 操作及结果核实 | `qos.read` + `platform.qos.read` |

新增的 `fairshare.read` 显式绑定四个内置角色。`platform.qos.read`、`platform.qos.manage` 默认仅绑定 Super Admin；Cluster Admin 原有的 `qos.update` 不会赋予全局写权限。API 在返回数据前复查权限，目录缓存不携带用户权限，不允许借缓存读取完整目录。

Fairshare 按控制器实际集群名称及当前集群 Association ID、用户名/账户和分区交叉校验。其他集群或不存在于当前关联集合的记录不会返回。每条记录提供权重、规范份额、原始用量、规范/有效用量、Fairshare 因子和层级因子。零值、未提供和无限值分别保留；原始累计用量使用十进制字符串，避免 uint64 经 JavaScript 数字转换丢失精度。很小的非零份额使用科学计数法显示。

因子来自 Slurm 控制器，实际优先级由站点的 priority 插件、权重及其他因子共同决定。原始用量使用 Slurm 记账单位，可能受 TRESBillingWeights 和衰减配置影响。参见 [SchedMD sshare](https://slurm.schedmd.com/sshare.html) 和 [QOS](https://slurm.schedmd.com/qos.html)。应用不修改控制器的优先级插件或权重。

QOS 缓存 60 秒，Fairshare 缓存 15 秒，均按平台集群及连接版本隔离。界面标注采样时间和缓存状态。全局写入及结果核实使各集群的策略/记账缓存与进行中的旧查询失效。响应沿用 8 MiB、20000 条上限。

## QOS 管理

支持创建新小写名称及编辑现有 QOS；编辑通过既有 ID 和名称定位，名称不可变。每次显式设置下列完整字段：

| 字段 | 输入语义 |
| --- | --- |
| 描述 | 1–200 字单行内容 |
| QOS 优先级 | 0–2147483647 的整数，贡献由站点 PriorityWeightQOS 等配置决定 |
| 用量计费因子 | 0–1000000 的有限数值，默认新建为 1 |
| 每用户运行作业上限 | 0–2147483647；空值/JSON null 表示无限制 |
| 每用户提交作业上限 | 同上，包含运行与等待作业 |
| 单作业时限 | 同上，单位分钟 |

数值零会按零提交，不能省略限额字段代替显式无限制。抢占、Flags、TRES 及其他高级配置保留现有值；QOS 的 Association/Partition 使用授权仍由管理员按站点策略维护。本项不提供删除 QOS、任意 Slurm JSON、抢占关系修改或 Fairshare 权重写入。TRES 配额属于下一项开发。

实测 v0.0.43 部分更新会把省略的 `preempt.mode` 重置为 DISABLED。服务端从最新快照取出原模式，绑定签名并显式回填，浏览器不能指定该字段。Flags 使用 `NOT_SET`。写后除核对可编辑值外，还核对未编辑字段的完整指纹；未能确认保留时不会标记已核实。

复用记账操作引擎：先读取最新定义及连接版本、复查平台/集群权限，生成两分钟确认；执行时再次比对完整快照，持久化意图审计后只发送一次单目标固定请求。确认绑定操作人、集群、所有字段和目标状态，需填写原因和精确 QOS 名称。应用内写入串行化，同名 QOS 的未决操作阻止新写入。

显式拒绝记为 failed，观察到目标值及未编辑字段一致为 confirmed，已受理未核实为 accepted，断线/超时等不确定结果为 unknown。操作记录支持服务重启后按持久 Spec 只读核实，不能自动重发未知写入。对外部管理员的并发修改仍无法提供 REST 跨请求事务，维护期间应协调同一 QOS 的变更。

## API

接口前缀 `/api/v1/clusters/{clusterId}`：

- `GET /qos`、`GET /qos/{name}`；`q/page/perPage/sort/order`，排序 name 或 priority。
- `GET /fairshare`；另支持精确 account/user、kind=account|user，排序 account/user/factor/id。
- `GET /qos-operations`；本人最近十条 QOS 操作，独立于账户身份操作列表。
- `POST /accounting-controls/prepare`；动作 `qos_create|qos_update`，Spec 的 qos 对象须显式提供所有八个字段，编辑还需既有 ID。
- `POST /accounting-controls/execute`；只接受 proof、confirmation、reason。
- `GET /accounting-operations/{operationId}`；只读核实，未编辑字段的内部指纹不在浏览器 DTO 中公开。

OpenAPI 版本为 0.16.0。审计支持 `qos.create/update`、可编辑字段前后值和策略详情导航。SlurmDBD、slurmrestd 及认证由系统管理员部署；应用安装器和权限迁移不会自动授予 Slurm 管理员，也不会修改第三方服务配置。

## 验收

2026-10-29 在 Rocky Linux 9.7 / Slurm 25.11.8 上，通过独立平台数据库和既有正式连接完成 QOS 创建、编辑、确认重放、零值/无限值及控制器 Fairshare 查询。预置临时 QOS 的 DENY_LIMIT、REQUEUE 和 GrpTRES=cpu=2，修复部分更新默认抢占模式后，验证所有未编辑字段保留。浏览器完成一次校验、原因和精确名称确认的编辑并显示已核实。原有 normal QOS、现有节点和作业保持原状。
