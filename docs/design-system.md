# UI 设计系统

本地 `/design-system` 是第一项的验收入口。页面与组件使用官方 shadcn/ui 的 `radix-mira` 预设，并由 `frontend/components.json` 记录生成配置。图标为 Lucide，不混用其他 UI 组件库。

## 主题与 Token

- `src/styles/foundation.css`：脚手架生成的 shadcn 主题和 Tailwind Token 映射。
- `src/styles/semantics.css`：平台主题与状态 Token，引用 Radix Blue、Red、Grass、Amber、Slate 的浅色/深色色阶。
- 默认 Slurm 蓝，可切换科研红；颜色偏好仅改变品牌色，失败和危险操作始终使用红色语义。
- 支持浅色、深色、跟随系统；跟随系统时响应系统主题变化。偏好通过 Zustand persist 保存在本地，仅包含主题、配色和密度，不保存身份和凭据。
- 使用系统中文字体回退和本地打包的 Inter，数字与资源标识使用等宽字体。
- 正文、导航和表格使用 14px，辅助文字使用 12px，数据注释使用 13px，英文分组标识最低 11px；字号集中在 `semantics.css` 定义，保留 Mira 的紧凑布局。

## 状态

`components/slurm/status.ts` 统一解析状态代码，`SlurmStatusBadge` 同时显示原始代码、状态点和可访问的中文标签。运行使用绿，等待和维护使用琥珀，完成和信息使用蓝，错误使用红，取消和未知使用灰。

未知代码保留原值，不归为成功或失败。当前仅演示单个规范状态代码；Slurm 的复合节点标志、版本差异由后续 Adapter 规范化后再交给展示组件，不在 UI 中猜测。

## 布局与交互

桌面侧栏固定，小屏使用 Radix Sheet 导航抽屉；键盘可操作 Select、Tabs、ToggleGroup 和 AlertDialog。所有图标按钮提供可访问名称，表单关联 Label 和错误信息，页面提供跳转到主内容链接。尊重系统 reduced-motion 偏好。

页面保持标准密度；表格支持标准/紧凑行高。表格内部横向滚动，避免把移动端页面撑宽。表格展示固定样例，搜索和排序只针对样例，后续真实查询必须由服务端分页和过滤。

`OperationConfirmDialog` 目前仅用于确认交互预览，输入“演示”后才能继续；关闭后清除确认内容。真实业务接入时必须另外实现权限、目标、操作范围和服务端审计，弹窗本身不是授权机制。

表单使用 React Hook Form + Zod，演示必填、长度限制、重置和提示。当前“验证输入”不会创建配置。“空状态 / 加载中 / 异常”切换用于统一反馈样式。

## 扩展约定

新增通用组件优先由 shadcn CLI 生成到 `components/ui/`；业务展示组件放在 `components/slurm/`，避免复制独立的对话框、选择器、表格行为。语义色集中管理，不在页面内硬编码状态颜色。业务页面通过 TanStack Query 管理请求缓存，通过 Zustand 保存少量客户端偏好。

当前已接入平台认证和“我的权限”页面；设计系统中的作业、确认及表单仍为展示样例，未接入真实 Slurm 资源和监控。后续事项直接复用这套壳层、主题和组件。
