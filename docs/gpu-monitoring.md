# GPU 监控

`/gpu` 接入既有 Prometheus/DCGM，展示物理 GPU 的实际利用率、显存、温度、功耗，以及 1/6/24 小时趋势。Slurm 配置量、已分配量及不可用资源另从节点读取，分别标明来源和时间；不将分配量当成利用率，也不展示 GPU 历史记账量。

本项按用户确认，完成可配置接入和隔离协议验证，真实 GPU 监控源稍后由管理员配置。未配置来源时，正式页面显示“未配置监控源”，实际指标和观测计数保留未知；不会使用演示数据填充正式页面。测试虚拟机没有物理 GPU，本次不宣称完成真实硬件采集验收。

## 连接既有监控源

仅需增加本应用的连接文件，无需修改应用数据库。参考 [metrics-profiles.example.json](../deploy/metrics-profiles.example.json)，将条目键替换为平台集群 ID，保存为 `/etc/slurm-console/metrics-profiles.json`，推荐 root:root 0600，父目录由 root 控制；禁止符号链接和组/其他用户写入。然后在既有 `/etc/slurm-console/slurm-console.env` 增加：

```sh
SLURM_CONSOLE_METRICS_PROFILES=/etc/slurm-console/metrics-profiles.json
```

重启本应用加载配置。应用安装器不安装、配置或重启 Prometheus、DCGM Exporter、GPU 驱动等外部组件。监控源的采集、物理卡注册、标签、网络和凭据由管理员维护。

配置字段：

| 字段 | 说明 |
| --- | --- |
| `label` | 页面显示的来源名称 |
| `endpoint` | Prometheus 根地址；远程必须 HTTPS 并校验证书，禁止 URL 用户凭据、查询、路径和跳转 |
| `clusterLabel` / `clusterValue` | 精确限定当前集群，必须与 Prometheus 实际标签匹配；不能使用 `__name__` 或 UUID 标签 |
| `nodeLabel` | Exporter 节点标签，例如 `Hostname` |
| `nodeMap` | 可选、一一对应的 Exporter 主机名到 Slurm 节点名映射；不配置则按标签中的明确节点名展示 |
| `utilizationUnit` | 必填 `percent` 或 `ratio`；按采集源实际单位选择，ratio 乘 100 显示百分比，不根据数值大小猜测 |
| `caFile` | 可选受保护的 PEM CA 文件；默认系统 CA，TLS 1.2+，不允许跳过校验 |
| `tokenFile` | 可选 Bearer token 文件，必须为普通文件且仅属主可读写，推荐 0600；不放入 URL、页面或日志 |

本地协议验收允许字面回环 IP 的 HTTP 来源，禁止通过 HTTP 发送凭据。来源按平台集群 ID 绑定，管理员更换 Slurm 连接时须同步核对监控的集群标签和节点映射。配置不接受浏览器提供的地址或 PromQL。

## 数据口径与限制

固定读取以下 DCGM 指标：

- `DCGM_FI_DEV_GPU_UTIL`：实际 GPU 利用率，按声明单位统一为百分比。
- `DCGM_FI_DEV_FB_USED/FREE/TOTAL`：MiB。TOTAL 未采集时，仅在已用与空闲均有效且采样相差不超过 60 秒时相加，标明“已用 + 空闲合计”；不含未单独采集的保留显存，不等同显卡标称容量。
- `DCGM_FI_DEV_GPU_TEMP`：摄氏度；`DCGM_FI_DEV_POWER_USAGE`：瓦。

字段可用性取决于硬件、驱动和 Exporter 的实际配置。经典指标名和实际单位应由管理员根据 `/metrics` 核对，参考 [NVIDIA DCGM Exporter 指标](https://docs.nvidia.com/datacenter/dcgm/latest/reference/dcgm-exporter-metrics.html)。本项不提供 AMD、逐进程、逐作业或 MIG 实例聚合。MIG UUID、GPU_I_ID/GPU_CI_ID 序列不会合并成物理卡；页面显示忽略的序列数。

物理卡按节点和 UUID 识别。跨节点 UUID 冲突、同指标多条来源、型号或索引冲突标为重复来源，不取平均或最大值掩盖冲突。零保留为零；缺失、NaN/Inf、哨兵和异常范围值保留未知。当前查询使用十分钟原始样本窗口，保留真实采样时间；超过 120 秒的值过期，缓存命中也重新检查新鲜度。超过十分钟没有样本的设备可能不再出现在当前观测中，不能据此推断硬件数量。

趋势仅允许当前可识别、无身份冲突的节点/UUID。1/6/24 小时分别使用 60/300/600 秒步长，固定 `last_over_time(...[60s])` 防止默认五分钟回看掩盖长缺口。缺失点保持 null，图形断开；不补零或无限向前填充。趋势显存总量在未出现 TOTAL 序列时可按同一时点的 USED+FREE 计算。当前值与趋势分别采样，不保证最后一点完全相同。参考 [Prometheus HTTP API](https://prometheus.io/docs/prometheus/latest/querying/api/)。

## 权限与接口

集群 `metrics.read` 控制监控和趋势，内置四类角色已有此读取权限；不新增写权限。Slurm 分配汇总另需 `nodes.read`，缺权不访问节点来源，返回 forbidden/item=null。来源前和返回前复查权限，连接停用或版本变化不返回旧结果。监控故障不抹去仍可读取的 Slurm 调度数据。

- `GET /api/v1/clusters/{clusterId}/metrics/gpu`：`q/node/status/sort/order/page/perPage`，包含独立监控、调度来源及分页观测。排序未知值始终在末尾。
- `GET /api/v1/clusters/{clusterId}/metrics/gpu/{uuid}/history?node=...&range=1h|6h|24h`：固定窗口趋势；身份不在当前快照时 404，来源失败 503。

完整结构见 OpenAPI 0.19.0。当前缓存 15 秒，趋势缓存 30 秒；单次响应最多 4 MiB、4096 条序列、512 个物理 GPU，趋势单卡有界查询。超限或不完整警告返回来源不可用，不展示被静默截断的统计。缓存总量 8 MiB、最多 64 条、并发查询最多四个，无凭据透传、任意查询或重定向。

窗口效率与采样覆盖另见 [GPU 资源效率分析](gpu-efficiency.md)，使用相同来源配置，无需新增第三方组件。
