# 本地开发

要求 Node.js 22.16+（22.x）或 Node.js 24 LTS、npm 10+，后端要求 Go 1.27.3+（可使用自动工具链）。依赖版本由 npm lockfile 与 Go module 固定。

```sh
make install
make dev-backend  # 终端一
make dev          # 终端二
```

首先按 [认证与首次授权](authentication.md) 创建平台账户，打开 Vite 输出的本地地址（默认 `http://localhost:5173`），登录后进入 `/design-system`。后端默认监听 `127.0.0.1:8090`，前端 `/api` 代理到此地址。

如果默认后端端口已被其他服务使用，可运行 `make dev-backend BACKEND_HTTP=127.0.0.1:18090`，前端对应运行 `SLURM_CONSOLE_API_TARGET=http://127.0.0.1:18090 make dev`。不要覆盖其他项目的 PocketBase 服务或数据目录。

```sh
make check # TypeScript、ESLint、go vet
make test  # 前端交互与真实 PocketBase/SQLite 集成测试
make build # frontend/dist 和 backend/slurm-console
```

也可以直接在 `frontend/` 中运行对应的 npm 脚本。提交 `package-lock.json`，安装使用 `npm ci`。新增基础组件使用 `npx shadcn@latest add <component>`；`components.json` 固定 `radix-mira` 与 Lucide。

构建后，可在 `backend/` 中运行 `./slurm-console serve --http=127.0.0.1:8090`，PocketBase 默认从 `../frontend/dist` 提供 SPA。`SLURM_CONSOLE_PUBLIC_DIR` 可指定绝对静态目录；`--dir` 指定数据库目录。未知 `/api` 不回退到 HTML。所有 CLI 与服务应使用同一数据目录，初始化迁移前停止服务。

`deploy/` 目前只保留目录，systemd、反向代理、一致性备份和恢复验证在“基础部署与备份”事项中实现。内网正式环境应经 HTTPS 反向代理提供服务；PocketBase 默认监听回环地址。

目前具备平台登录和授权查询，没有 Slurm 连接能力。不要在前端环境变量中放入 Slurm JWT 或连接凭据。SMTP、PAM/LDAP/FreeIPA 尚未配置；账户恢复当前由可信管理员维护。
