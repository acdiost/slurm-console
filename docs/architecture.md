# 工程架构

采用 README 定义的模块化单体：React Web Console → PocketBase/Go → Slurm Adapter → slurmrestd。当前完成阶段一全部十一项；`internal/clusterconfig` 实现受保护连接方式、配置持久化及有界的只读 Slurm ping，`internal/slurm/{client,adapter,models}` 提供明确版本的查询适配，`internal/services/slurm.go` 管理缓存、并发及配置失效。`internal/overview` 从标准 DTO 派生资源/状态汇总，`/overview` 展示真实集群概况；`internal/jobcontrol` 提供显式 Slurm 身份关联、短期确认及持久审计取消，`/jobs` 提供真实作业管理；`internal/nodecontrol` 及 `/nodes` 提供节点查询和 Drain/Resume；`/partitions` 提供分区查询、约束及关联节点资源；`internal/audit` 及 `/audits` 提供管理变更持久化和审计查询；`internal/backup` 和 `internal/maintenance` 提供加密快照、恢复及维护互斥，阶段一 MVP 已完成。

前端入口为 `frontend/src/main.tsx`，`app/` 管理 Router 和 QueryClient；`pages/` 组织页面；`features/` 保存功能；`components/ui/` 保存官方 shadcn/ui Mira 生成的组件；`components/slurm/` 封装状态等领域展示；`stores/` 保存主题和密度偏好；`services/` 预留业务请求边界；`styles/` 保存语义 Token。

已建立 React Router、TanStack Query、Zustand、TanStack Table、React Hook Form 和 Zod 基础。Motion、react-resizable-panels、dnd-kit 和 ECharts 在相应功能实际需要时引入，避免在初始化阶段增加无使用场景的依赖。UI 组件统一使用 shadcn/ui，交互使用已有的 Radix primitives，图标统一使用 Lucide。

当前 `/design-system` 是登录后的交互式 UI 验收页，`/access` 从后端读取当前用户的实际权限，`/clusters` 提供平台范围的集群配置。设计系统内的表格记录是固定示例，不代表已连接的集群；表单与操作确认只演示 UI 行为，不发送管理请求。未实施的业务导航显示禁用状态。

后端通过 PocketBase Go Framework 承载原生认证 API 和平台 `/api/v1` 路由。Go 迁移初始化平台用户、角色、动作权限、角色权限绑定、平台与集群范围授权及最小集群标识。前端使用官方 PocketBase SDK，以 sessionStorage 保存当前标签页会话，令牌有效期为一小时，刷新时必须到后端重新校验。平台令牌不是 Slurm JWT。

`rbac.Resolve` 每次读取服务端授权记录；权限不从 JWT 自定义声明或浏览器传入的角色判断。CLI 创建/授权与后端在同一工作目录或明确相同的 `--dir` 数据目录运行。普通用户不能修改授权 Collection。作业取消需要显式身份关联、最新作业校验和持久审计；平台审计查询已实现，其他控制动作按后续事项实施。

连接配置、迁移兼容性、传输认证和并发约束见 [集群连接配置](cluster-connections.md)。连接 endpoint 和凭据路径来自服务端受保护文件，平台 API 只接收引用，凭据不进入 PocketBase。

只读查询契约及边界见 [Slurm Adapter](slurm-adapter.md)，外部服务部署责任见 [部署说明](deployment.md)。

总览采用逐项授权、部分失败返回及来源级缓存时间，见 [总览契约](overview.md)。登录后默认进入 `/fleet`，逐集群展示授权范围内的连接、资源和作业；使用有界调度及 Adapter 来源缓存，配置变化只丢弃相应卡片，授权变化丢弃整页。见[多集群管理](multi-cluster.md)。设计系统仍仅为组件验收页。

作业列表、详情、取消及身份 CLI 见 [作业管理](jobs.md)。操作审计先于外部写入，未知结果只读核实，不自动重试写入。

节点查询、状态矩阵及逐节点审计控制见 [节点管理](nodes.md)。节点控制沿用固定 REST 身份，不自动授予上游管理员权限。

分区详情按 `partitions.read` 授权，节点资源单独检查 `nodes.read`，部分来源失败保持分区信息可读，见 [分区查询](partitions.md)。

操作审计独立使用平台权限，查询不回读 Slurm；平台数据变更与审计同事务，凭据和内部确认字段不进入查询 DTO，见 [操作审计](audits.md)。

备份/恢复命令在 PocketBase 自动初始化之前独立分流，避免在故障数据库上启动框架。服务短暂停止后生成加密文件快照，恢复校验和路径限制见 [备份与恢复](backups.md)。

`internal/linuxusers` 查询受保护配置中按集群 ID 映射的本机或固定 SSH 身份主机，reader 只输出公开 NSS 元数据；独立 `linux_users.read` 授权和来源后的权限/版本复查不依赖 Slurm REST 身份。系统账户不保存为 PocketBase 业务表；`/linux-users` 与其他业务页共享集群上下文。见 [Linux 系统用户查询](linux-users.md)。

Linux 本地账户写入通过固定 root 子命令和 root 所有的 UID 策略隔离；平台先持久审计，主机再记录意图与结果。平台或主机未知结果只读取核实，绝不自动重发 OS 命令。主机恢复状态独立于应用数据库生命周期。

应用 systemd 服务以 root 运行，PAM 与本地账户 helper 直接调用。Slurm Unix socket 请求通过一次性降权子进程保留专用 `slurm-console` 对端身份，使用有界 JSON 管道和固定环境，不继承 root 附加组；HTTPS/JWT 连接保持原传输。升级快照在权限迁移前生成，恢复依据归档 unit 选择 root 或旧版服务账户的数据所有者。

`internal/accounting` 通过固定 SlurmDBD REST 请求管理 Account/User/Association；全局身份变更与集群关联权限分离，写前确认与审计、写后只读核实，未决目标不重发。只读页面 `/accounts` 通过实际 Slurm 集群名过滤记账数据，隐藏其他集群身份及默认 Account。

QOS / Fairshare 使用既有 Slurm Client、Adapter 和有界缓存。QOS 全局写入复用记账确认/审计引擎；服务端保留抢占模式并核实未编辑字段指纹。Fairshare 将控制器份额与当前集群关联交叉校验，uint64 用量以字符串传输。

TRES 复用记账确认和审计引擎。目录绑定资源 ID 与类型，数值以十进制字符串传输。Association 写入从最新快照保留全部六类 TRES 列表，QOS 使用单项合并并保留抢占模式；结果核实覆盖目标及所有未编辑字段。见 [TRES 限额](tres.md)。

Reservation 通过 slurmctld API 查询及控制，复用持久确认/审计引擎，保持独立路由和操作家族。更新只发送结束时间并核实未编辑字段，作业检查保留未知归属状态。见 [资源预留](reservations.md)。

GPU 实际指标通过独立 gpumetrics 模块接入受保护的 Prometheus 配置，固定 DCGM 查询、单位归一、有界缓存及采样新鲜度检查；趋势与 Slurm 调度分配分别采样，缺失不补零。见 [GPU 监控](gpu-monitoring.md)。

历史作业使用现有 Slurm 连接和有界查询服务，Adapter 从控制器解析实际集群后限定 slurmdbd 条件，避免浏览器指定多集群范围。记账 DTO 与当前作业、监控分别建模，重复 ID 保留独立身份，步骤不重复计数。见 [历史作业](job-history.md)。

批量控制器只协调已实现的单资源服务：确认信息在服务端短期有界保存，每个真实控制仍在单项服务持久审计后发送一次。所有批次共享三项目标并发，浏览器断开后继续已授权的协调；进程重启不续发队列，改由逐项操作编号只读恢复。见 [批量操作](bulk-operations.md)。
