# 工程架构

采用 README 定义的模块化单体：React Web Console → PocketBase/Go → Slurm Adapter → slurmrestd。当前完成阶段一前七项；`internal/clusterconfig` 实现受保护连接方式、配置持久化及有界的只读 Slurm ping，`internal/slurm/{client,adapter,models}` 提供明确版本的查询适配，`internal/services/slurm.go` 管理缓存、并发及配置失效。`internal/overview` 从标准 DTO 派生资源/状态汇总，`/overview` 展示真实集群概况；`internal/jobcontrol` 提供显式 Slurm 身份关联、短期确认及持久审计取消，`/jobs` 提供真实作业管理；下一项为节点查询及 Drain/Resume。

前端入口为 `frontend/src/main.tsx`，`app/` 管理 Router 和 QueryClient；`pages/` 组织页面；`features/` 保存功能；`components/ui/` 保存官方 shadcn/ui Mira 生成的组件；`components/slurm/` 封装状态等领域展示；`stores/` 保存主题和密度偏好；`services/` 预留业务请求边界；`styles/` 保存语义 Token。

已建立 React Router、TanStack Query、Zustand、TanStack Table、React Hook Form 和 Zod 基础。Motion、react-resizable-panels、dnd-kit 和 ECharts 在相应功能实际需要时引入，避免在初始化阶段增加无使用场景的依赖。UI 组件统一使用 shadcn/ui，交互使用已有的 Radix primitives，图标统一使用 Lucide。

当前 `/design-system` 是登录后的交互式 UI 验收页，`/access` 从后端读取当前用户的实际权限，`/clusters` 提供平台范围的集群配置。设计系统内的表格记录是固定示例，不代表已连接的集群；表单与操作确认只演示 UI 行为，不发送管理请求。未实施的业务导航显示禁用状态。

后端通过 PocketBase Go Framework 承载原生认证 API 和平台 `/api/v1` 路由。Go 迁移初始化平台用户、角色、动作权限、角色权限绑定、平台与集群范围授权及最小集群标识。前端使用官方 PocketBase SDK，以 sessionStorage 保存当前标签页会话，令牌有效期为一小时，刷新时必须到后端重新校验。平台令牌不是 Slurm JWT。

`rbac.Resolve` 每次读取服务端授权记录；权限不从 JWT 自定义声明或浏览器传入的角色判断。CLI 创建/授权与后端在同一工作目录或明确相同的 `--dir` 数据目录运行。普通用户不能修改授权 Collection。作业取消需要显式身份关联、最新作业校验和持久审计；完整平台审计、其他控制动作及自动备份按后续事项实施。

连接配置、迁移兼容性、传输认证和并发约束见 [集群连接配置](cluster-connections.md)。连接 endpoint 和凭据路径来自服务端受保护文件，平台 API 只接收引用，凭据不进入 PocketBase。

只读查询契约及边界见 [Slurm Adapter](slurm-adapter.md)，外部服务部署责任见 [部署说明](deployment.md)。

总览采用逐项授权、部分失败返回及来源级缓存时间，见 [总览契约](overview.md)。登录后默认进入 `/overview`，设计系统仍仅为组件验收页。

作业列表、详情、取消及身份 CLI 见 [作业管理](jobs.md)。操作审计先于外部写入，未知结果只读核实，不自动重试写入。
