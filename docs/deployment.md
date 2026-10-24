# 本地开发

要求 Node.js 22.16+（22.x）或 Node.js 24 LTS、npm 10+。后续后端需要 Go 1.24+，此阶段不启动后端服务。

```sh
make install
make dev
```

打开 Vite 输出的本地地址（默认 `http://localhost:5173`），自动进入 `/design-system`。

```sh
make check  # TypeScript 和 ESLint
make test   # 状态语义及关键交互
make build # 生成 frontend/dist
```

也可以直接在 `frontend/` 中运行对应的 npm 脚本。提交 `package-lock.json`，安装使用 `npm ci`。新增基础组件使用 `npx shadcn@latest add <component>`；`components.json` 固定 `radix-mira` 与 Lucide。

前端为 SPA；未来由 PocketBase 提供静态文件时，页面路径需要回退到 `index.html`，`/api` 保持后端路由。`deploy/` 目前只保留目录，systemd、反向代理、一致性备份和恢复验证在“基础部署与备份”事项中实现。

当前页面无业务接口、登录和集群连接能力。不要在前端环境变量中放入 Slurm JWT 或连接凭据。
