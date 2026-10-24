# 后端工程边界

PocketBase v0.40.6 内嵌 Go 应用，要求 Go 1.27.3+。启动 `go run . serve --http=127.0.0.1:8090`。数据库默认写入当前工作目录的 `pb_data/`，通过 `--dir` 可选择其他目录；数据库与凭据不提交 Git。

`go run . users create --email <邮箱> --name <名称>` 创建平台账户，密码通过隐藏终端输入或 stdin 读取。初始账户没有任何集群授权。`go run . access --help` 查看平台/集群授权命令。认证和权限约定见 `../docs/authentication.md`、`../docs/permissions.md`。

Linux PAM 使用 `cmd/pam-helper`（Linux CGO 构建），由普通 Web 服务通过限定的 sudo 规则调用。`users create-pam` 创建关联账户，`users link-pam` 切换已有账户；部署及故障排查见 `../docs/pam.md`。PAM 默认关闭，关联账户不会回退到平台密码。

LDAP 通过 `SLURM_CONSOLE_LDAP_CONFIG` 指向服务端 JSON 配置，支持 LDAPS / 必须成功的 StartTLS。`users check-ldap` 验证 TLS 与只读查询凭据；`users create-ldap` / `link-ldap` 关联目录唯一标识并保持 RBAC。客户端配置见 `../docs/ldap.md`，目录密码不保存在 PocketBase。

启动时应用 Go 迁移并关闭公开注册，所有业务和授权 Collection 原生 CRUD 规则均锁定。`GET /api/v1/auth/me` 校验活动平台用户并返回实时权限；`GET /api/v1/clusters` 在服务端筛选授权范围，详情接口要求 `clusters.read`。

`SLURM_CONSOLE_PUBLIC_DIR` 可指定已构建的前端目录，默认 `../frontend/dist`；如果存在 `index.html`，同一后端提供 SPA 回退。未知 `/api` 请求始终返回 API 错误，不回退到 HTML。

- `internal/api`：平台 `/api/v1` 路由。
- `internal/application`：PocketBase 初始化、迁移和命令注册。
- `internal/auth`、`internal/rbac`：平台身份和集群范围授权。
- `internal/ldapauth`、`internal/pamauth`：LDAP 加密绑定和 PAM 子进程适配。
- `internal/slurm/{client,adapter,models}`：独立的 Slurm 接口适配。
- `internal/services`：应用服务。
- `internal/monitoring`、`internal/audit`、`internal/cache`：监控、审计、缓存。
- `migrations`：PocketBase 数据迁移。

不在浏览器保存集群凭据；不将作业、节点持久化为平台业务主表。平台用户、Slurm 身份和集群权限分别建模。

Slurm Adapter 的版本、查询及缓存约束见 `../docs/slurm-adapter.md`；部署只有应用安装器和可独立重复运行的 PAM 设置脚本，不安装 LDAP/FreeIPA/slurmrestd。
