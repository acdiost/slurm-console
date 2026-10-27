# 集群总览

`/overview` 展示用户授权范围内某个集群的真实数据；登录后的默认首页为[多集群工作台](multi-cluster.md) `/fleet`。集群选择器使用服务端分页（每页 20 个）和字面搜索，URL 可携带 `cluster=<记录 ID>&clusterPage=<候选页码>&clusterQ=<候选搜索>`；刷新或分享链接保留选择。旧 `?page=` 链接会规范化为 `clusterPage` 和明确的集群 ID。搜索、翻页只浏览候选，明确选择后才切换集群；在总览、作业、节点、分区间导航保留集群上下文。列表只展示具有 `clusters.read` 的集群，权限查询不依赖前端角色判断。

页面包含控制器/RESTD/API 版本，CPU、内存、GPU 调度容量，当前作业分类，节点状态标记、分区预览和需关注节点。它不展示模拟趋势、虚构告警或操作流水。设计系统中的样例继续独立存在。

## 数据与权限

`GET /api/v1/clusters/{clusterId}/overview` 要求活动平台用户及该集群的 `clusters.read`，不接受查询过滤器。返回 `{data,requestId}`，`data` 包含 `cluster`、`revision`、`generatedAt` 与 `health/jobs/nodes/partitions` 四个分项。

每项结构为 `{status,code,message,item,fetchedAt,cached}`：

- `ok`：包含真实汇总及来源完成时间。
- `forbidden`：缺少该项的 `jobs.read/nodes.read/partitions.read`，不访问相应上游，`item=null`。
- `error`：上游故障、响应异常或查询繁忙，`item=null`；其他来源仍可展示。错误信息经过安全转换，不暴露原始 Slurm 响应。

停用返回 409，未授权返回 403；请求期间配置 revision 变化会丢弃整份结果并返回 409，避免拼接不同配置的数据。分页后的当前作业/节点列表不用于计算总量；服务端直接汇总 Adapter 的有界完整结果。总览不落地数据库。

## 资源口径

- 容量按节点去重，不能把多个分区的节点或 CPU 数量相加。
- CPU 总量使用 `cpus`，可用处理器使用 `effective_cpus`（字段缺失时退回总 CPU）。已分配为 `alloc_cpus`；有效 CPU 之外的预留部分、不可调度状态节点的未分配部分计入“不可用/预留”。内存按 `real_memory/alloc_memory` 汇总，单位 MiB；不把 OS 空闲内存当作调度空闲量。
- 常规可用状态为 IDLE/MIXED/ALLOCATED，DOWN、DRAIN、FAIL、NO_RESPOND、MAINT、RESERVED、重启及电源转换等标记将剩余资源计为不可用。未知状态按保守规则处理。空闲量是快照估算，不保证任意作业立即可运行。
- GPU 来自 GRES/`gres_used`，支持 `gpu:数量`、`gpu:类型:数量` 及括号内 IDX/拓扑注释。区分没有 GPU（零）与无法确认数量/分配（null）；模糊的混合通用/类型计数或未知格式不猜测。GPU 为调度资源分配量，不是显存、计算利用率或历史记账，也不把 MPS/shard 当作 GPU 数量。
- 节点类别互斥，但原始状态标记可重叠，因此状态标记数不应相加。需关注节点按名称排序，最多预览 5 个；分区最多预览 6 个，并显示真实总数。
- 作业分类互斥：异常结束、取消、完成、收尾、排队、运行、其他。异常结束包括 FAILED/TIMEOUT/NODE_FAIL/BOOT_FAIL/OUT_OF_MEMORY/DEADLINE。数据来自控制器当前保留的可见记录，包括终态；不是“最近一天失败量”或长期历史统计。

## 刷新与故障反馈

默认每 10 秒刷新，支持暂停和手动刷新，后台标签页不轮询。每项标注自己的数据时间与缓存来源，不能把总览生成时间当作所有数据的采样时间。沿用 Adapter 的 5/10/15 秒缓存、相同查询合并及上游并发上限；上游数据非原子采样。

切换集群会清空旧指标；延迟响应按 Query key 隔离。403/404 后停止自动刷新并隐藏旧缓存数据，允许手动重试；分项权限被撤销后该项直接显示无权限。失败与未知数据使用状态说明和“—”，不会伪装为零容量。

只读总览不安装或调整 Slurm/LDAP 等外部组件。节点运维、作业控制及完整操作审计已分别接入对应页面；历史记账留待后续阶段。

口径依据：[Slurm sinfo](https://slurm.schedmd.com/sinfo.html)、[Slurm GRES](https://slurm.schedmd.com/gres.html)。
