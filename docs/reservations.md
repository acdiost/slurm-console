# Reservation 资源预留

`/reservations` 按已授权的集群查看控制器预留，支持筛选、排序、分页、详情、新建、调整结束时间及删除。支持已验证的 Slurm `v0.0.43`，复用固定连接及审计确认引擎，不安装或重配 Slurm 服务。

## 操作范围

新建要求明确预留名称、起止时间、节点名称，以及至少一种正向用户/Account 访问名单。节点逐一填写，以逗号或空格分隔；各名单最多 128 项，禁止重复、`ALL`、节点范围及正负名单表达式。节点必须属于当前控制器。开始在一分钟后、一年内，窗口最多三十天；表单时间使用浏览器标明的时区，API 使用 Unix 秒。明确节点按整节点预留，不配置核心切片、GPU 数量、许可证或动态节点请求。

普通预留和维护预留均可创建。维护选项对应 `MAINT`，允许与其他预留重叠，应在确认时核对范围。本项不开放 `IGNORE_JOBS`、`OVERLAP`、周期、磁性或浮动时间等标记。Slurm 仍检查资源可用性及固定身份权限，平台不恢复 DOWN 节点，也不为创建预留取消作业。

只有基本、明确节点的一次性预留可在本项变更。调整仅提交结束时间，要求尚未开始且距离开始超过一分钟；节点、访问名单、起始时间和其他可见配置保持不变，并在写后核实。修改 ACL、节点及其他策略需管理员用原生工具维护。含周期、动态资源、分区选择、用户组、核心切片、启动延迟、特征、许可证、BurstBuffer 等高级或未知配置的预留可完整查询，页面不开放调整或删除。

调整与删除均查询最新作业，存在未结束引用作业时拒绝；若任何未结束作业未提供预留归属，也拒绝变更。删除只释放明确名称的预留，不取消作业。控制器自动过期后，详情可能返回 404。列表状态由当前时间及有限起止时间计算；“生效中”不表示节点健康、作业可运行或所有资源可用。

## 权限与确认

`reservations.read` 显式绑定四类内置角色。`reservations.create/update/delete` 绑定 Super Admin 和指定集群的 Cluster Admin，Operator/Viewer 只读。新建另需 `nodes.read`；调整、删除另需 `jobs.read`。这些权限不授予主机、PAM、Slurm 用户或全局记账管理权限。

查询缓存十五秒，返回前复查授权。跨集群目标通过登记连接、实际 Slurm 集群和预留名称定位，不接受浏览器传入 REST 地址或 Slurm JSON。变更先校验最新权限、连接版本、资源和完整原状态，生成两分钟确认。填写单行原因并精确输入名称后执行，先持久化审计，再发送一次请求；重放同一确认返回原记录。目标已有未决操作时拒绝新的写入。

结果 `confirmed` 表示已观察到目标及未编辑字段符合预期；`accepted` 和 `unknown` 仅允许只读核实，重启后亦可继续，不自动重发。已删除状态可能由控制器到期清理产生，不能据此证明删除因果。Slurm 接口没有条件更新/CAS，外部 CLI 在校验后并发修改仍存在竞态；平台不声称原子事务或自动回滚。

删除审计保留原起止时间、节点、用户及 Account 范围。本人最近十次预留操作独立展示，平台审计可跳转业务结果；高级配置不通过任意 JSON 入口改写。

## 接口和部署要求

以下路径均位于 `/api/v1/clusters/{clusterId}`：

- `GET /reservations?q&state&page&perPage&sort&order`；状态 `scheduled/active/ended/unknown`，排序 `name/startTime/endTime`。
- `GET /reservations/{name}`；缺失或过期返回 404。
- `POST /reservation-controls/prepare`：`action=reservation_create|reservation_update|reservation_delete`，`spec` 为预留参数。
- `POST /reservation-controls/execute`：提交确认、原因及服务端 proof。
- `GET /reservation-operations`、`/{operationId}`：最近记录及只读核实。

请求结构见 OpenAPI 0.18.0。创建参数包含 `name/startTime/endTime/nodes/users/accounts/maintenance`；调整仅 `name/endTime`，删除仅 `name`。其他字段为空、零或省略。原有记账接口仍管理记账对象，预留使用独立入口。

管理员需准备包含 slurmctld API 的 slurmrestd，以及具备足够上游权限的固定身份。无需新增第三方组件或修改 `AccountingStorageEnforce`。应用保留 Slurm 的资源与 ACL 求值，详见 [SchedMD 预留说明](https://slurm.schedmd.com/reservations.html)。

## 实机验证

2026-10-29 在 Rocky Linux 9.7 / Slurm 25.11.8 使用独立平台数据库验证新建、结束时间调整、删除、确认重放及只读核实。测试预留位于两周后的短时间窗口，明确限定 localhost 和测试访问用户。调整后仅 `end_time` 变化，节点、访问范围、MAINT/SPEC_NODES 及其他可见字段保持一致；现有 DOWN 节点状态和原有作业保持原状。额外创建的隔离排队作业验证了引用预留时阻止调整/删除，测试作业随后取消。查询归属使用 v0.0.43 作业信息的 `resv_name`，与提交参数 `reservation` 区分。验收后清理临时预留与隔离服务，不变更正式登录账户或站点策略。
