# Slurm Console 技术设计方案

**版本：** V1.0  
**日期：** 2026-10-29<br>
**定位：** 企业内部多集群 Slurm 管理系统  
**建设原则：** 轻量化、模块化、低维护、可扩展

## 当前进度与本地启动

已完成阶段一全部十一项：项目初始化及 UI 设计系统、PocketBase Go 框架与认证、基础 RBAC 及集群权限、集群连接配置、Slurm Adapter、集群总览、作业列表/详情/取消、节点查询与控制、分区查询、操作审计、基础部署与备份，并按实施顺序提前接入 Linux PAM 和 LDAP。V1 已接入多集群工作台、Linux 系统用户、本地账户、Slurm 记账身份以及 QOS / Fairshare、TRES 限额、Reservation 及 GPU 监控接入。当前支持用户名或邮箱登录、会话刷新、退出、按动作及集群授权的查询，以及 `/history`、`/gpu`、`/reservations`、`/tres`、`/policies`、`/accounts`、`/linux-users`、`/fleet`、`/overview`、`/jobs`、`/nodes`、`/partitions`、`/audits`、`/design-system`、`/access` 和 `/clusters` 页面。

要求 Node.js 22.16+（22.x）或 24 LTS、npm 10+、Go 1.27.3+（Go 的自动工具链可按 `go.mod` 下载）。PocketBase 固定为 v0.40.6：

```sh
make install
make dev-backend  # 终端一，后端监听 127.0.0.1:8090
make dev          # 终端二，打开 Vite 输出的地址
```

打开 Vite 输出的本地地址。校验使用 `make check`、`make test`、`make build`。

首次登录前，在 `backend/` 中运行 `go run . users create --email <你的邮箱> --name <显示名称>`，在隐藏输入提示中设置密码。账户默认没有集群权限；本机管理员可用 `access grant` 授权，详见 [认证与授权](docs/authentication.md)。没有内置账户或默认密码，公开注册已关闭。

设计系统的作业记录仍为固定样例；[集群连接配置](docs/cluster-connections.md) 支持受保护的服务端连接方式、增改启停、REST API 兼容性与控制器连通检测。[Slurm Adapter](docs/slurm-adapter.md) 已提供健康、作业、节点、分区只读 API，明确支持 `v0.0.43`。[集群总览](docs/overview.md) 已接入真实资源、调度与控制器状态。[作业管理](docs/jobs.md) 支持真实筛选分页、详情和经显式身份关联的审计取消。[节点管理](docs/nodes.md) 支持状态矩阵、资源详情及逐节点审计的 Drain / Resume。[分区查询](docs/partitions.md) 支持筛选排序、约束详情及独立授权的关联节点资源。[操作审计](docs/audits.md) 支持跨集群检索、详情、当前页导出及平台管理变更的事务留痕。[基础部署与备份](docs/backups.md) 已支持加密快照、每日调度、校验和恢复。[多集群管理](docs/multi-cluster.md) 提供登录后默认首页 `/fleet`，按授权分页展示各集群的连接、资源和作业，并保留跨业务页集群选择。[Linux 系统用户查询](docs/linux-users.md) 支持本机/固定 SSH 身份来源、NSS 枚举、筛选分页和精确用户名详情；本机来源增加受限的账户创建、到期锁定/解锁、显式确认及持久审计，目录账户仍由管理员维护。[账户与 Association](docs/accounting.md) 已接入按集群隔离的 Account、Slurm 用户和关联查询，以及确认、审计和只读结果核实的账户创建/编辑、Slurm 用户创建及关联创建/移除；全局身份变更需要独立平台记账权限。[QOS / Fairshare](docs/qos-fairshare.md) 已支持按授权范围读取策略、确认和审计的 QOS 新增/编辑，以及控制器公平份额展示；全局修改要求独立平台权限。[TRES 限额](docs/tres.md) 已支持注册资源目录、QOS / Association 的容量和分钟额度、单资源设置/清除及写后保留核实。[Reservation 资源预留](docs/reservations.md) 已支持查询、明确节点的新建、结束时间调整及经作业检查的删除，并提供确认、审计和只读核实。[GPU 监控](docs/gpu-monitoring.md) 已支持既有 Prometheus/DCGM 接入、实际指标和固定窗口趋势，调度分配量独立展示；真实监控源待管理员配置。[历史作业与记账](docs/job-history.md) 已支持按提交时间的有界查询、重复 Job ID 独立记录、退出结果、TRES 与步骤详情。[批量操作](docs/bulk-operations.md) 已支持本页多选作业取消和节点 Drain/Resume、服务端有界执行、逐项审计与只读结果恢复。[筛选条件持久化](docs/saved-filters.md) 已支持按本人、模块和集群保存具名方案，跨浏览器恢复默认方案及显式地址优先。[告警通知](docs/notifications.md) 已支持本人规则、定时采样、触发/恢复去重、站内未读与实时变更提示。[集群容量分析](docs/capacity.md) 已支持配置/分配/余量对比、共享分区去重、节点余量分布及每五分钟汇总趋势。[GPU 资源效率分析](docs/gpu-efficiency.md) 已支持物理卡窗口统计、采样覆盖、逐卡排序和低利用率线索；真实硬件源仍待配置。[作业异常诊断](docs/job-diagnostics.md) 已支持当前/独立历史记录的按需规则解释、支撑证据、步骤异常和粗略 CPU 记账指标。V2 下一项为“运维操作模板”。Web 服务以 root 运行，直接调用 PAM 与本地账户 helper；Slurm 本地 socket 请求仍以专用 `slurm-console` 身份执行；认证支持平台密码、使用受限 helper 的 [PAM](docs/pam.md)，以及校验证书的 [LDAP / StartTLS](docs/ldap.md)。LDAP 关联目录唯一标识，保留现有 RBAC；FreeIPA/AD 的专属生命周期及目录组同步尚未实现。

部署只安装本应用；LDAP、FreeIPA、slurmrestd 等第三方服务由系统管理员自行安装。PAM 可通过 `sudo bash deploy/install.sh --pam` 一键配置，重复执行保持幂等。安装器默认启用每日加密备份；异机复制目标和密钥离线保管由管理员配置，见 [恢复流程](docs/backups.md)。

开发说明见 [本地开发](docs/deployment.md)、[UI 设计系统](docs/design-system.md) 和 [工程架构](docs/architecture.md)。

---

## 1. 项目目标

### 1.1 系统定位

建设一个统一的 Slurm Web 管理平台，集中管理公司内部少量 HPC/AI 算力集群。

核心功能覆盖：

- 集群状态与资源概览
- 作业查询、管理与控制
- 计算节点、GPU 与分区管理
- Slurm 用户、账户与关联关系管理
- QOS、Fairshare、TRES 与资源限额
- 资源使用统计与历史记账
- 管理员权限控制及操作审计
- PAM 本地系统认证与 LDAP 目录认证（未来兼容 FreeIPA 专属集成）

### 1.2 设计约束

| 项目 | 规划 |
|---|---|
| 服务对象 | 内部运维、管理员及授权员工 |
| 集群规模 | 初期 1–3 个，设计支持扩展 |
| 部署模式 | 单实例、内网部署 |
| 核心后端 | Go + PocketBase |
| 数据存储 | PocketBase SQLite |
| Slurm 接口 | slurmrestd / slurmdb |
| 运行环境 | Linux，推荐 Rocky Linux 9 |
| 高可用要求 | 初期不做服务级 HA |
| 身份管理 | PocketBase Auth，预留 FreeIPA 集成 |
| 运维原则 | 最少组件，自动备份，易于升级 |

**非目标：**

第一版不开发完整的 HPC 用户门户、在线 IDE、文件管理器、计费结算系统或复杂的集群自动部署系统。

---

## 2. 总体技术架构

### 2.1 前端技术栈

| 分类 | 技术 | 用途 |
|---|---|---|
| 基础框架 | React + TypeScript | 页面与业务逻辑 |
| 工程构建 | Vite | 构建及开发 |
| UI 组件 | shadcn/ui | 统一组件体系 |
| UI 风格 | Mira | 高信息密度运维界面 |
| CSS | Tailwind CSS | 样式与设计 Token |
| 图标 | Lucide | 全站统一图标 |
| 动画 | Motion | 交互动画 |
| 可调整布局 | react-resizable-panels | 工作区面板 |
| 拖拽 | dnd-kit | 自定义布局与排序 |
| 状态管理 | Zustand | 客户端全局状态 |
| 请求缓存 | TanStack Query | 服务器数据管理 |
| 数据表格 | TanStack Table | 高密度数据展示 |
| 图表 | Apache ECharts | 统计及监控可视化 |
| 颜色体系 | Radix UI Colors | 主题色及状态语义色 |
| 表单验证 | React Hook Form + Zod | 表单及输入校验 |
| 路由 | React Router | 页面导航 |

**UI 设计原则：**

1. 全站使用 shadcn/ui 组件体系。
2. 采用 Mira 作为默认风格，支持浅色和深色主题，优先科研红和 Slurm 蓝。
3. 普通管理页保持标准密度；作业与节点表格支持 Compact 模式。
4. 不混用 Ant Design、MUI 等其他组件系统。
5. Motion 动画仅用于必要的交互反馈。
6. 统一通过 Radix Colors 语义 Token 显示 Slurm 状态。
7. 大量记录采用服务端分页与筛选，不依赖前端一次性加载全部数据。
8. 严禁在已有依赖可以正确表达的情况下自行设计实现组件和行为。


### 2.2 后端技术栈

| 分类 | 技术 | 用途 |
|---|---|---|
| 开发语言 | Go | 核心业务开发 |
| 应用框架 | PocketBase Go Framework | HTTP 服务、认证和数据管理 |
| 应用数据库 | SQLite | 用户、配置与审计 |
| HTTP API | PocketBase Custom Routes | Slurm 管理接口 |
| API 规范 | OpenAPI 3.x | 前后端接口契约 |
| 实时通信 | PocketBase SSE | 通知与操作状态 |
| 调度接口 | slurmrestd | 访问 Slurm |
| 监控接口 | Prometheus HTTP API | 获取监控指标 |
| 部署 | systemd + Caddy/Nginx | 进程与反向代理管理 |
| 日志 | Go slog + journald | 应用运行日志，满足等保规范要求 |

不为本项目额外部署 Redis、消息队列、独立网关或微服务编排平台。

### 2.3 架构分层

```text
┌───────────────────────────────────┐
│        React Web Console          │
│    shadcn/ui + TanStack Query     │
└────────────────┬──────────────────┘
                 │ HTTPS
┌────────────────▼──────────────────┐
│          PocketBase + Go          │
│                                   │
│  Authentication / RBAC            │
│  Custom REST API                  │
│  Audit / Config / Notification    │
│                                   │
│  ┌─────────────────────────────┐  │
│  │    Slurm Application Layer  │  │
│  │                             │  │
│  │  JobService                 │  │
│  │  NodeService                │  │
│  │  PartitionService           │  │
│  │  AccountService             │  │
│  │  MonitoringService          │  │
│  └──────────────┬──────────────┘  │
│                 │                 │
│  ┌──────────────▼──────────────┐  │
│  │       Slurm Adapter         │  │
│  │  API Client / Cache / Auth  │  │
│  └──────────────┬──────────────┘  │
└─────────────────┼─────────────────┘
                  │ Internal Network
     ┌────────────┼───────────────┐
     ▼            ▼               ▼
  slurmrestd   SlurmDBD        Prometheus
  (Slurm API)  (via REST)      (Metrics)
     │            │               │
     └──────── Slurm Cluster ─────┘
```

PocketBase SQLite 仅保存管理平台自身的数据，不取代 Slurm 数据库。

---

## 3. 功能模块

### 3.1 集群总览

展示：

- 集群在线状态
- 集群版本与控制器状态
- CPU、GPU、内存资源总量
- 已分配、空闲和不可用资源
- RUNNING / PENDING / FAILED 作业数量
- 节点状态统计
- 集群告警及近期管理操作

### 3.2 作业管理

核心功能：

- 作业列表、状态查询与高级筛选
- Job ID、用户、账户、分区、QOS 查询
- 作业资源申请与实际分配信息
- 作业优先级与排队原因
- 作业取消、挂起、恢复及重新排队（按 API 能力开放）
- 作业运行详情与历史记录

作业日志不默认存储在 PocketBase；后期通过受限的日志读取服务提供访问。

### 3.3 节点管理

提供表格、状态矩阵两种视图。

支持：

- 节点状态与资源容量查询
- CPU/GPU 分配状态
- DRAIN、DOWN 等节点状态及原因
- Drain / Resume 操作
- 节点备注和维护信息
- 批量操作及结果反馈

节点控制必须具备权限校验、操作确认和审计记录。

### 3.4 分区管理

- 查看分区状态和容量
- 分区与节点关联
- 时间限制与资源约束
- 分区资源统计
- 修改分区配置（V1 阶段）

### 3.5 账户与资源策略

支持：

- Account 管理
- User / Association 管理
- QOS 管理
- Fairshare 信息展示
- TRES 资源配额
- Reservation 管理
- GPU 配额与使用统计

Slurm 用户、Account 和 Association 不等同于 PocketBase 平台用户，必须明确分开建模。

### 3.6 监控和统计

- CPU/GPU 资源使用趋势
- GPU 显存、利用率及温度
- 作业历史与资源消耗
- 集群资源利用率
- 账户和用户使用统计

GPU 监控由现有 Prometheus/Exporter 系统提供。

**需要区分：GPU 资源分配量、GPU 实际利用率、GPU 历史记账量，三者不能混为一谈。**

---

## 4. PocketBase 数据模型

建议建立以下 Collections：

| Collection | 用途 |
|---|---|
| users | 平台用户 |
| roles | 平台角色 |
| permissions | 权限定义 |
| role_permissions | 角色权限关联 |
| user_cluster_roles | 集群范围授权 |
| clusters | 集群配置与标识 |
| slurm_identities | 平台用户与 Slurm 身份映射 |
| operation_audits | 管理操作审计 |
| notifications | 用户通知 |
| notification_rules | 通知规则 |
| user_preferences | 界面个性化设置 |
| saved_filters | 用户保存的列表筛选配置 |

集群连接凭据必须保存在服务端受保护的密钥存储或配置中，Collection 只保存必要的引用及非敏感元数据。

作业、节点、分区和历史记账数据不作为 PocketBase 的持久化业务主表。

---

## 5. API 设计

统一使用：

`/api/v1/`

### 5.1 集群

```http
GET /api/v1/clusters
GET /api/v1/clusters/{clusterId}
GET /api/v1/clusters/{clusterId}/overview
GET /api/v1/clusters/{clusterId}/health
```

### 5.2 作业

```http
GET  /api/v1/clusters/{clusterId}/jobs
GET  /api/v1/clusters/{clusterId}/jobs/{jobId}
POST /api/v1/clusters/{clusterId}/jobs/{jobId}/cancel
POST /api/v1/clusters/{clusterId}/jobs/{jobId}/requeue
```

### 5.3 节点

```http
GET  /api/v1/clusters/{clusterId}/nodes
GET  /api/v1/clusters/{clusterId}/nodes/{nodeName}

POST /api/v1/clusters/{clusterId}/nodes/{nodeName}/drain
POST /api/v1/clusters/{clusterId}/nodes/{nodeName}/resume
```

### 5.4 账户及监控

```http
GET /api/v1/clusters/{clusterId}/accounts
GET /api/v1/clusters/{clusterId}/qos
GET /api/v1/clusters/{clusterId}/associations

GET /api/v1/clusters/{clusterId}/metrics/cpu
GET /api/v1/clusters/{clusterId}/metrics/gpu
GET /api/v1/clusters/{clusterId}/metrics/memory
```

以上为内部平台接口契约，并非全部与 slurmrestd 原生路径一一对应。

所有接口统一返回错误码、错误信息和请求标识；分页、过滤、时间范围及批量操作格式应在 OpenAPI 中规范化。

---

## 6. Slurm Adapter 设计

Slurm Adapter 独立于 PocketBase 的路由和数据模型。

建议接口：

```go
type SlurmClient interface {
    GetJobs(ctx context.Context, opts JobQuery) ([]Job, error)
    GetJob(ctx context.Context, id string) (*Job, error)
    CancelJob(ctx context.Context, id string) error

    GetNodes(ctx context.Context) ([]Node, error)
    DrainNode(ctx context.Context, name, reason string) error
    ResumeNode(ctx context.Context, name string) error

    GetPartitions(ctx context.Context) ([]Partition, error)
}
```

实际工程中应进一步统一分页结构、错误类型和操作结果。

每个集群使用独立的客户端配置：

- API Endpoint
- Slurm 版本与 REST API 版本
- 认证方式
- 请求超时
- 重试策略
- 缓存策略
- 支持的 API 能力

启动时检查集群连通性和 API 兼容性。

禁止由浏览器直接访问 slurmrestd。

### 查询及缓存策略

默认：

| 数据 | 初始刷新策略 |
|---|---|
| 作业 | 5–10 秒 |
| 节点 | 10–15 秒 |
| 分区 | 15–30 秒 |
| 账户 / QOS | 60 秒 |
| GPU 指标 | 10–30 秒 |
| 历史作业 | 按需查询 |

上述周期均为建议初值，需要进行性能测试。

后端使用内存缓存、同类请求合并和限流，避免多个浏览器并发查询 Slurm 控制器。

操作请求不自动重试；涉及状态变更时采用状态核验和明确结果反馈。

---

## 7. 身份认证与权限安全

### 7.1 权限模型

建议采用 RBAC + 集群范围授权。

| 角色 | 读取 | 管理作业 | 管理节点 | 管理账户 | 平台配置 |
|---|---|---|---|---|---|
| Super Admin | 全部 | 全部 | 全部 | 全部 | 允许 |
| Cluster Admin | 授权集群 | 授权集群 | 授权集群 | 授权集群 | 无 |
| Operator | 授权集群 | 受限 | 受限 | 无 | 无 |
| Viewer | 授权集群 | 无 | 无 | 无 | 无 |

具体权限采用细粒度动作定义，如 `jobs.cancel`、`nodes.drain`、`accounts.update`，而不是仅判断角色名称。

### 7.2 身份映射

三层身份：

1. PocketBase User：平台登录身份。
2. Slurm Identity：实际 Linux/FreeIPA 用户。
3. RBAC Permission：允许执行的管理操作。

后台必须同时检查平台授权和 Slurm 端权限。

### 7.3 高风险操作控制

所有状态变更操作必须：

- 验证用户身份与集群权限
- 验证目标资源和允许操作范围
- 记录操作人、目标、时间、请求标识及操作结果
- 对高风险操作执行二次确认
- 不允许普通用户绕过 Go API 修改审计记录

**重要：不能把拥有高权限的 Slurm JWT 直接交给浏览器，也不能把平台用户权限简单映射成任意 Slurm 用户代理能力。**

Slurm 操作认证应优先采用受限身份与受信任的内部代理；高权限功能须单独审查。

审计记录应有独立备份或外部留存机制，以降低管理员误操作和数据库损坏造成的证据丢失风险。

---

## 8. 前端布局方案

### 8.1 导航结构

```text
集群总览
│
├── 资源与调度
│   ├── 作业管理
│   ├── 节点管理
│   ├── 分区管理
│   └── CPU / GPU 资源
│
├── Linux 用户
│   ├── 用户
│   └── 用户组
│
├── 账户与策略
│   ├── 用户与账户
│   ├── Association
│   ├── QOS / Fairshare
│   └── Reservation
│
├── 监控与统计
│   ├── 资源监控
│   ├── 作业历史
│   └── 使用统计
│
└── 平台设置
    ├── 集群配置
    ├── 用户与角色
    ├── 操作审计
    └── 系统设置
```

### 8.2 核心业务组件

```text
SlurmJobTable
SlurmJobDetail
JobStatusBadge

SlurmNodeTable
SlurmNodeGrid
NodeStateBadge

PartitionUsage
TRESUsage
GPUStatusGrid

SlurmAccountTree
QoSSelector
FairshareDisplay

ClusterHealthCard
OperationConfirmDialog
```

所有组件遵循统一设计 Token，优先使用 shadcn/ui 和 Lucide。

---

## 9. 项目目录

```text
slurm-console/
├── frontend/
│   ├── src/
│   │   ├── app/
│   │   ├── pages/
│   │   ├── features/
│   │   ├── components/
│   │   │   ├── ui/
│   │   │   └── slurm/
│   │   ├── hooks/
│   │   ├── stores/
│   │   ├── services/
│   │   └── styles/
│   ├── package.json
│   └── vite.config.ts
│
├── backend/
│   ├── main.go
│   ├── internal/
│   │   ├── api/
│   │   ├── auth/
│   │   ├── rbac/
│   │   ├── slurm/
│   │   │   ├── client/
│   │   │   ├── adapter/
│   │   │   └── models/
│   │   ├── services/
│   │   ├── monitoring/
│   │   ├── audit/
│   │   └── cache/
│   ├── migrations/
│   └── go.mod
│
├── api/
│   └── openapi.yaml
│
├── deploy/
│   ├── systemd/
│   └── reverse-proxy/
│
├── docs/
│   ├── architecture.md
│   ├── api-design.md
│   ├── permissions.md
│   └── deployment.md
│
└── README.md
```

采用模块化单体架构，不引入微服务。

---

## 10. 部署与运维方案

### 10.1 部署架构

第一版建议部署在独立的内部管理节点或虚拟机中。

```text
Internal Users
     │
 HTTPS / VPN
     │
Caddy / Nginx
     │
PocketBase + Go
     ├── React Static Files
     ├── SQLite Database
     ├── Slurm Adapter
     └── Monitoring Client
              │
     Internal Cluster Network
              │
       slurmrestd
              │
           Slurm
```

推荐 systemd 管理进程，不要求 Docker 或 Kubernetes。

### 10.2 备份与恢复

- 每日备份 PocketBase 数据库。
- 将备份复制到独立存储。
- 当前采用短暂停服及维护锁的一致文件快照，包含 PocketBase 数据、WAL 和匹配程序；在私有副本验证 SQLite 完整性。
- 备份内的配置和凭据使用 age 加密，解密密钥独立保管。
- 定期验证恢复流程。
- 通过 Git 管理数据库迁移和配置模板。
- 升级前执行完整备份与兼容性验证。

### 10.3 系统可靠性

必须考虑以下故障：

- Slurm 控制器不可访问
- slurmrestd 认证失败
- 集群 API 版本不兼容
- SlurmDBD 不可用
- PocketBase 数据库写入失败
- 部分集群离线
- 前端请求超时

平台故障不得影响 Slurm 自身调度运行。

集群管理员始终应保留 SSH + Slurm CLI 应急管理通道。

---

## 11. 分阶段实施

### 阶段一：MVP

目标：完成 Slurm 基本管理闭环。

- [x] 项目初始化及 UI 设计系统
- [x] PocketBase Go 框架与认证
- [x] 基础 RBAC 及集群权限
- [x] 集群连接配置
- [x] Slurm Adapter
- [x] 集群总览
- [x] 作业列表、详情及取消
- [x] 节点查询、Drain / Resume
- [x] 分区查询
- [x] 操作审计
- [x] 基础部署与备份

**验收标准：** 管理员可登录平台，安全管理单个 Slurm 集群，查询结果与 Slurm CLI 一致，关键操作可追溯。

### 阶段二：V1

目标：覆盖主要管理需求。

- [x] 多集群管理
- [x] Linux 系统用户查询、本地账户创建及锁定/解锁
- [x] 账户与 Association
- [x] QOS / Fairshare
- [x] TRES 限额
- [x] Reservation
- [x] GPU 监控接入（真实来源待配置）
- [x] 历史作业与记账查询
- [x] 批量操作
- [x] 筛选条件持久化
- [x] 告警通知

**验收标准：** 可以通过平台完成绝大多数日常 Slurm 调度和账户管理操作。

### 阶段三：V2

目标：运维效率优化。

- [x] 集群容量分析
- [x] GPU 资源效率分析
- [x] 作业异常诊断
- [ ] 运维操作模板
- [ ] 资源使用报表
- [ ] 配置变更对比
- [ ] 可选 AI 运维辅助能力

---

## 12. 关键技术决策

| 决策项 | 结论 |
|---|---|
| 前端框架 | React + TypeScript + Vite |
| UI 方案 | shadcn/ui Mira |
| 状态管理 | Zustand + TanStack Query |
| 图标 / 动画 | Lucide / Motion |
| 后端 | Go + PocketBase 内嵌扩展 |
| 应用数据库 | SQLite |
| Slurm 通信 | slurmrestd |
| SlurmDBD | 通过 slurmdb REST API |
| 指标系统 | 接入已有 Prometheus |
| 缓存 | Go 进程内存缓存 |
| 实时通信 | 按需使用 PocketBase SSE |
| 认证 | PocketBase Auth，预留 FreeIPA / LDAP |
| 权限 | RBAC + 集群范围授权 |
| 部署 | Linux + systemd + 反向代理 |
| 架构 | 模块化单体 |
| MVP | 先实现单集群管控 |

---

## 13. 主要风险及应对

| 风险 | 应对 |
|---|---|
| PocketBase 尚未稳定至 v1 | 版本固定、迁移测试、回滚方案 |
| Slurm REST API 差异 | Adapter 版本兼容与能力探测 |
| Slurm 高权限代理风险 | 服务端鉴权、最小权限、凭据隔离 |
| 集群查询影响控制器 | 缓存、限流、请求合并 |
| SQLite 单实例故障 | 备份恢复、独立运维通道 |
| 过早引入复杂前端交互 | MVP 以表格和常规操作为主 |
| 多集群权限隔离不完整 | 所有 API 执行服务端集群授权 |
| 历史记账与实时状态不一致 | 按数据源建立明确权威边界 |

---

## 14. 最终结论

推荐采用：

**React + shadcn/ui (Mira) + PocketBase (Go Framework) + SQLite + slurmrestd + Prometheus**

该方案能够：

1. 降低初期部署与维护复杂度。
2. 复用 PocketBase 的身份认证和后台数据管理能力。
3. 让 Go 负责真正重要的 Slurm 控制逻辑。
4. 保留多集群管理、权限控制和监控分析的扩展空间。
5. 避免为了少量集群引入过度复杂的基础设施。

第一版的重点不是实现所有 Slurm 功能，而是建立**可靠、可审计、可扩展的基础管理能力**，随后逐步扩展。

**项目设计原则：保持简单，但不以牺牲权限安全、数据正确性和操作可靠性为代价。**

