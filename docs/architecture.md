# 工程架构

采用 README 定义的模块化单体：React Web Console → PocketBase/Go → Slurm Adapter → slurmrestd。当前仅完成阶段一第一项。

前端入口为 `frontend/src/main.tsx`，`app/` 管理 Router 和 QueryClient；`pages/` 组织页面；`features/` 保存功能；`components/ui/` 保存官方 shadcn/ui Mira 生成的组件；`components/slurm/` 封装状态等领域展示；`stores/` 保存主题和密度偏好；`services/` 预留业务请求边界；`styles/` 保存语义 Token。

已建立 React Router、TanStack Query、Zustand、TanStack Table、React Hook Form 和 Zod 基础。Motion、react-resizable-panels、dnd-kit 和 ECharts 在相应功能实际需要时引入，避免在初始化阶段增加无使用场景的依赖。UI 组件统一使用 shadcn/ui，交互使用已有的 Radix primitives，图标统一使用 Lucide。

当前 `/design-system` 是交互式 UI 验收页。表格记录是内存中的固定示例，不代表已连接的集群；表单与操作确认只演示 UI 行为，不发送管理请求。未实施的业务导航显示禁用状态。

后端仅初始化目录和 Go module。认证、授权、Slurm 数据、控制操作及审计按后续清单依次实施。
