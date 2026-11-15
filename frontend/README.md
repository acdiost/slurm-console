# Slurm Console 前端

React + TypeScript + Vite + Tailwind CSS，使用 shadcn/ui Mira 与 Lucide。

```sh
npm ci
npm run dev
npm run typecheck
npm run lint
npm test
npm run build
```

匿名用户进入 `/login`，登录后进入 `/fleet`；`/access` 显示实际授权。开发服务器将 `/api` 代理到 `127.0.0.1:6829`，请同时启动后端。完整约定见根目录 `docs/design-system.md`、`docs/authentication.md` 与 `docs/deployment.md`。

具有平台设置管理或集群管理权限的用户可以进入 `/setup`，完成可选 PAM 环境检查与启用，再通过 Unix socket 或 HTTPS/JWT 接入 slurmrestd。工作台首次安装欢迎入口只依据 `/setup/status` 的 `needsSetup`，不依据筛选后的集群列表；普通用户不请求此管理状态。已有集群可从向导进入配置页检查停用或未配置状态。JWT 仅保留在当前表单内存，保存成功或离开步骤后清空。`/settings?tab=users` 可直接进入用户与角色管理，仍遵循当前用户权限。

正式交付从项目根目录执行 `make release TARGET_ARCH=amd64` 或 `arm64`，构建产物会嵌入 Go 二进制，目标主机无需单独安装前端目录或 Node.js。直接运行本目录的 `npm run build` 只生成 `dist/`，不会更新二进制中的资源。

平台认证及权限查询已连接后端；设计系统的作业表格和确认操作仍是显式示例，尚未连接 Slurm。
