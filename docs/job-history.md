# 历史作业与记账查询

`/history` 从现有 slurmdbd 的 REST `v0.0.43` 读取历史作业及步骤，不依赖控制器内存中的已结束作业记录。安装器不安装或配置第三方记账组件，管理员应提供与控制器一致的 slurmdbd、数据库、REST 插件及固定身份的读取授权。

## 查询范围

默认最近 24 小时，可指定最多 31 天的提交时间区间 `[start,end)`，时间参数为 UNIX 秒；页面使用浏览器本地时间输入并显示。时间限制是提交时间，不是运行区间相交筛选；长作业若提交于区间外不列出，时长和 CPU 记账保持来源的完整值，不裁切到窗口。

支持单个精确用户名、Account、分区、QOS、状态或数字 Job ID，以及本地文本包含搜索、确定排序和分页。传给 Slurm 的条件固定包含实际控制器集群名、`usage_time_as_submit_time=true`、`show_duplicates=true`、`disable_truncate_usage_time=true`、`disable_whole_hetjob=true`，并读取步骤。不会接受任意上游参数、CSV、多集群名称、脚本或环境变量查询。来源返回后再次校验集群及筛选，跨集群数据导致拒绝响应。

记录可能包含 PENDING/RUNNING 等尚未结束的作业，状态以查询时来源实际状态为准；状态筛选再次按返回状态核对，不将 Slurm 曾经过的历史状态当成当前状态。实际可见范围受固定连接身份和 slurmdbd 的 PrivateData、保留策略及同步延迟限制。界面不会将空列表解释为全库无作业；Slurm 的标准“found nothing”空结果正常展示，其他不完整警告报错。

## 记录与指标

Job ID 不是唯一键。每条记录保留 SHA-256 标识，依据实际集群、Job ID、提交/开始时间和数组/异构维度生成，详情按该标识与同一提交时间窗口定位。重复 Job ID 不合并；完全无法区分的重复身份拒绝显示。重新排队导致开始时间变化时，旧详情可能 404，刷新后重新选择。标识只用于定位，不是授权凭据。

列表显示作业数量，步骤只在详情读取，不再作为作业行或重复计入时间。详情包括提交/开始/结束时间、经过与挂起时长、时间限制、总 CPU 记账秒数、退出状态/代码/信号、步骤派生退出码、资源申请/分配、数组/异构身份及各步骤的状态和记账。

未知、NO_VAL、负 TRES 哨兵保留未知，已记录的零保持零；未启动作业的特殊开始时间不显示为未来日期。退出码 0 不是作业成功的判断依据，应结合状态和信号。CPU 秒为来源 seconds+microseconds，缺少字段不补零；采集配置不完整时来源自身可能报告 0，因此零不证明未消耗资源。

申请/分配 TRES 的 mem 单位为 MiB，数量用十进制字符串保留大整数精度。步骤消耗 average/total 保留来源原始计量值，其单位和可用性依赖采集项，不跨 TRES 汇总、不自行推断显存或 GPU 利用率。本项不提供 MaxRSS 汇总、资源利用率百分比、账户用量汇总、运行环境、批处理脚本和文件内容。GPU 分配、实际监控与历史记账分别展示，详见 [GPU 监控](gpu-monitoring.md)。

## 权限与接口

独立集群动作 `job_history.read` 显式授予四类内置角色；自定义角色须显式绑定。`jobs.read` 或 `accounts.read` 不隐含历史查询。请求前、查询/缓存返回后都核对用户活动状态、实时动作授权、集群启停和连接版本。

- `GET /api/v1/clusters/{clusterId}/job-history`：`start/end/user/account/partition/qos/state/jobId/q/sort/order/page/perPage`。
- `GET /api/v1/clusters/{clusterId}/job-history/{recordKey}`：使用相同时间和精确筛选条件定位单条记录。无匹配返回 404。

默认每页 20，最大 100；允许提交/开始/结束时间、经过时长、Job ID 排序，未知值始终置末。列表省略步骤，详情保留步骤。30 秒有界缓存按集群、连接修订和完整条件隔离，页面不自动轮询；刷新按钮重发请求，30 秒内可能命中缓存。来源单响应 8 MiB、最多 5000 条作业、单作业 512 步骤及全查询 20000 步骤，超限须缩小条件，不静默截断。使用现有四并发、128 条/32 MiB 缓存限制，失败不缓存。

本机 root 服务经受限子进程以专用 Slurm socket 身份读取。子进程仅接受固定条件的规范序列化，不能读取任意记账端点或升级为脚本/环境查询。本功能全部只读。

契约依据测试环境安装版本的 `/openapi/v3`，参考 [Slurm REST API](https://slurm.schedmd.com/rest_api.html)、[sacct](https://slurm.schedmd.com/sacct.html) 与[记账采集配置](https://slurm.schedmd.com/accounting.html)。

详情新增“诊断作业”，绑定该记录标识、Job ID 和提交窗口；异常步骤与主/派生退出记录独立核对，不合并重复 Job ID，见 [作业异常诊断](job-diagnostics.md)。
