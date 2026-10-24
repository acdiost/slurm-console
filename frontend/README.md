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

匿名用户进入 `/login`，登录后进入 `/design-system`；`/access` 显示实际授权。开发服务器将 `/api` 代理到 `127.0.0.1:8090`，请同时启动后端。完整约定见根目录 `docs/design-system.md`、`docs/authentication.md` 与 `docs/deployment.md`。

平台认证及权限查询已连接后端；设计系统的作业表格和确认操作仍是显式示例，尚未连接 Slurm。
