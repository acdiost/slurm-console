# 多集群管理

`/fleet` 是登录后的默认首页，按授权范围分页展示各逻辑集群的配置状态、控制器健康、节点与 CPU/内存/GPU 容量，以及当前可见的运行、排队、异常作业数。每张卡片分别进入该集群的总览、作业、节点或分区；具有 `platform.clusters.manage` 的用户还可进入集群配置。

多个逻辑连接可能指向同一控制器，工作台不对跨集群节点、CPU、GPU 或作业直接求和。容量和作业分类沿用[集群总览](overview.md)口径；DOWN 节点未分配资源计入不可用，GPU 无法确认时显示未知，控制器保留的终态作业不代表某个历史时间窗口。

## 查询与授权

`GET /api/v1/cluster-fleet` 要求活动平台用户，返回 `{data,requestId}`。`data` 包含 `items/page/perPage/totalItems/generatedAt/pageSummary/canManage`。

- `page`：1–1000000，默认 1；`perPage`：1–24，默认 12（页面提供 6/12/24）。
- `q`：名称、标识、描述的字面匹配，最多 128 个 Unicode 字符且 512 字节；SQLite `lower` 对 ASCII 忽略大小写，不提供 Unicode 大小写折叠。
- `configuration`：`enabled/disabled/unconfigured`，缺省全部；这是数据库中的配置状态，不是跨全部集群的实时健康筛选。
- `sort=name|slug`、`order=asc|desc`，默认名称升序，ID 打破同名排序；重复、未知或非法参数返回 400，访问上游前拒绝。

先按当前服务端 `clusters.read` 授权过滤，再在同一数据库事务内计数和分页，最后查询当前页的上游。无授权返回空列表，不泄露其他集群名称、数量或数据。节点和作业分别检查 `nodes.read/jobs.read`，未授权时不访问来源。`permissions` 仅用于该卡片导航，写操作仍由原业务接口重新授权和确认。

每项包含 `cluster`（仅 ID/名称/标识/描述）、`revision`、`configuration`、`state`、`permissions` 和 `health/nodes/jobs`。来源结构与总览相同：`status/code/message/item/fetchedAt/cached`；无权限为 `forbidden`，失败为 `error`，两者 `item=null`。不返回 endpoint、连接引用、凭据或原始上游错误。

## 故障、刷新和计数

- 状态区分 `connected`、`controller_down`、`unavailable`、`disabled`、`unconfigured`、`busy`、`changed`、`pending`。控制器未就绪不等同于 REST 不可达；未完成的卡片不判为离线。
- 单集群健康检查失败后不继续查询节点与作业；健康成功后资源来源独立失败，其他集群和来源仍可展示。缺权、失败和未知数据不填零。
- 最多 4 个工作台查询 worker，单集群依次读取健康、节点、作业，复用 Adapter 全局 4 个真实上游并发槽及按集群/版本隔离的来源缓存。整页等待预算 15 秒，保留此前已完成的卡片，其余标注未取得快照。共享的上游查询仍受自身超时约束。客户端取消返回 408。
- 响应前重新解析权限，授权动作或范围发生变化返回 409 丢弃整页；配置版本、标识或启停变化只将对应卡片标为 `changed`，丢弃其旧来源数据并要求刷新。
- `pageSummary` 只统计当前页的逻辑连接：`connected/controllerDown/unavailable/disabled/unconfigured/pending` 为互斥类别，其中 `unavailable` 包含查询失败、繁忙和配置已变化。`partial` 表示健康成功但节点或作业查询失败；`needsAttention` 对连接/数据待处理卡片去重计数，不包含已停用、未配置或仅缺权限的卡片，也不是节点健康告警数。`totalItems` 才是当前授权和筛选后的全局配置数。
- 页面默认每 30 秒刷新，可暂停或手动刷新，后台标签页不轮询。各来源显示自己的完成时间和缓存状态；整页生成时间不是原子采样时间。查询错误后隐藏旧整页数据，延迟响应按 Query key 隔离。

## 集群选择

总览、作业、节点、分区、Linux 用户共用可搜索的分页选择器。URL 的 `cluster` 是实际选择，`clusterPage/clusterQ` 仅用于候选列表；浏览候选不改变当前集群。明确选择才清空原集群的业务筛选、详情和待确认操作。跨这些页面导航只保留集群上下文，不转移旧作业、节点或操作确认；审计页面的 `cluster` 是审计筛选条件，不作为业务页集群上下文。

## 验证范围

自动测试覆盖独立连接缓存、授权范围与计数隔离、分页前过滤、部分失败、停用/未配置不访问上游、请求期间撤权或改配置、有界调度、选择器浏览与切换、过期响应及错误后隐藏旧数据。真实 Slurm 验证使用管理员现有服务；不安装或修改 Slurm、LDAP、FreeIPA 等外部组件。

2026-10-27 已在现有 Rocky Linux 虚拟机部署，并使用现有 PAM 账户验证 `/fleet`：1 个真实连接、1 个 DOWN 节点，CPU 总量 2、空闲 0、不可用 2。浏览器验证登录首页、空筛选、卡片导航及搜索候选时保留当前集群。Slurm 配置与服务进程保持不变。当前只有一个真实控制器，多连接数据差异、缓存隔离与撤权竞态由独立 Unix socket 上游测试验证，尚未在两个独立真实集群上联调。完整验证为后端 112 项测试及竞态检测、`go vet`、前端 78 项测试、类型检查、lint、构建和 OpenAPI 引用校验。
