# TRES 限额

`/tres` 提供注册资源目录、Association / QOS 的本层资源限额、单资源设置和清除。使用既有固定 Slurm 连接身份及 `v0.0.43`，不创建 TRES 类型、不安装第三方服务，不修改控制器执行策略。

## 配置范围

| 类型 | QOS | Association |
| --- | --- | --- |
| 总并发资源 `GrpTRES` | 支持 | 支持，Account 层级也约束下级 |
| 单作业 `MaxTRESPerJob` | 支持 | 支持 |
| 每用户 / 每账户 `MaxTRESPerUser/Account` | 支持 | 不适用 |
| 单节点 `MaxTRESPerNode` | 支持 | 支持 |
| 累计额度 `GrpTRESMins` | 支持 | 支持 |
| 运行额度 `GrpTRESRunMins` | 支持 | 支持 |
| 单作业额度 `MaxTRESMinsPerJob` | 支持 | 支持 |

编辑白名单为已注册的 cpu、mem、node、billing、license 及 gres/gpu、gres/gpu:型号。目录来自共享记账库，不代表所选集群的物理资源或可调度容量；其他注册类型仍可查询。内存使用 MiB；时间额度是资源单位乘分钟，不能当作作业墙钟分钟。Relative QOS 的容量限额使用整数百分比 0–100，时间额度本项只读及清除，由站点管理员维护。

读取计数使用十进制字符串保留 uint64 精度，有限值、显式无限、未设置分别表示。写入值接受标准十进制字符串 `0..9223372036854775807`。零按零提交；“清除本层配置”移除该资源在此类限额中的设置，随后依 Slurm 的父级、QOS、分区和站点规则求值。页面不把空值表述为最终无限，也不计算有效继承限额。

## 授权与执行

资源目录要求集群 `tres.read`。Association 查询另需 `associations.read`；QOS 另需 `qos.read` 与 `associations.read`，缺少平台 `platform.qos.read` 时只显示当前实际 Slurm 集群 Association 引用的 QOS。读取缓存 60 秒，命中缓存后仍复查当前权限。

Association 更新要求 `tres.read/update`、`associations.read/update`，绑定目标 ID、实际集群、账户、用户和分区完整身份。QOS 更新另需集群 `qos.read/update` 和平台 `platform.qos.read/manage`，全局修改不会由 Cluster Admin 隐式获得。内置四类角色显式获得 `tres.read`；Super Admin、Cluster Admin 获得 `tres.update`。root 层级和服务关联只读。

修改先校验最新目录、原配置和权限，产生两分钟确认；输入原因及精确 QOS 名称或 Association ID 后执行。每次只修改一个资源的一类限额，不接受浏览器提供 Slurm JSON。确认绑定全部原配置，陈旧快照需要重新校验；重放同一确认返回原操作，不重发写请求。本人最近十次 TRES 操作独立展示；可从平台审计跳转对应对象。

Slurm 25.11.8 实测表明，Association 的资源列表采用替换语义，且局部更新其他 TRES 类型时省略的总并发列表会被清空。服务端从最新快照携带全部六类 Association TRES 列表，仅修改选中的条目；QOS 按单项合并并显式保留抢占模式与其他配置。写后同时核实目标值和所有未编辑字段。无法完整保留、未登记资源、过大的保留列表或不可写数值均在发送前拒绝。保留载荷最多 7000 字节，以满足确认、IPC 及审计边界。

上游受理而尚未核实的状态保持 accepted；连接丢失保持 unknown。结果查询只读观察，重启后也能继续核实，不自动重试写入或声称事务回滚。

## 系统管理员需要准备

已有 slurmdbd 记账库、包含 slurmdbd 插件的 slurmrestd、受限固定连接以及足以更新关联和 QOS 的 Slurm 管理权限。GPU 等类型须由管理员在 Slurm 中注册和配置。应用安装器只安装本应用，不修改 `AccountingStorageEnforce`、`PriorityFlags`、TRES 类型或其他站点策略。

配置写入成功并不表示调度已经强制执行。管理员须按站点策略设置 `AccountingStorageEnforce`；累计分钟的统计和衰减、组限额及 QOS/分区优先关系也受 Slurm 配置影响。参考 [Slurm Resource Limits](https://slurm.schedmd.com/resource_limits.html) 与 [TRES](https://slurm.schedmd.com/tres.html)。

## API 与验收

- `GET /api/v1/clusters/{clusterId}/tres`：共享资源目录。
- `GET /tres-limits?target=association|qos`：筛选、排序、分页，返回资源目录与本层列表。
- `GET /tres-limits/{kind}/{targetId}`：可见对象详情，不可见返回 404。
- `GET /tres-operations`：本人最近十次 TRES 操作。
- 复用 `/accounting-controls/prepare`、`/execute`，动作 `qos_tres_update`、`association_tres_update`，Spec 使用 `tres`；单条结果沿用 `/accounting-operations/{operationId}`。完整结构见 OpenAPI 0.17.0。

2026-10-29 在 Rocky Linux 9.7 / Slurm 25.11.8 上使用独立平台数据库及临时 QOS、Account、用户关联验证 19 次写入和确认重放，覆盖零值、清除、容量及分钟额度、同类其他资源和不同限额类型的保留。QOS 的 DENY_LIMIT/REQUEUE、Association 的默认身份、份额及作业限制均参与未编辑字段核实。原有对象保持一致。测试环境 `AccountingStorageEnforce=none`，本次验收配置写入及控制器同步；未宣称调度强制执行，也未更改站点执行策略。GPU 类型由自动化夹具验证，不据此声称虚拟机具备 GPU。
