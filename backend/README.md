# 后端工程边界

PocketBase v0.40.6 内嵌 Go 应用，要求 Go 1.27.3+。启动 `go run . serve`（默认监听 `127.0.0.1:6829`），可通过 `--http` 覆盖监听地址。数据库默认写入当前工作目录的 `pb_data/`，通过 `--dir` 可选择其他目录；数据库与凭据不提交 Git。

`go run . users create --email <邮箱> --name <名称>` 创建平台账户，密码通过隐藏终端输入或 stdin 读取。初始账户没有任何集群授权。`go run . access --help` 查看平台/集群授权命令。认证和权限约定见 `../docs/authentication.md`、`../docs/permissions.md`。

Linux PAM 使用 `cmd/pam-helper`（Linux CGO 构建），由 root Web 服务直接调用，无需 sudo 规则。`users create-pam` 创建关联账户，`users link-pam` 切换已有账户；部署及故障排查见 `../docs/pam.md`。PAM 默认关闭，关联账户不会回退到平台密码。

LDAP 通过 `SLURM_CONSOLE_LDAP_CONFIG` 指向服务端 JSON 配置，支持 LDAPS / 必须成功的 StartTLS。`users check-ldap` 验证 TLS 与只读查询凭据；`users create-ldap` / `link-ldap` 关联目录唯一标识并保持 RBAC。客户端配置见 `../docs/ldap.md`，目录密码不保存在 PocketBase。

启动时应用 Go 迁移并关闭公开注册，所有业务和授权 Collection 原生 CRUD 规则均锁定。`GET /api/v1/auth/me` 校验活动平台用户并返回实时权限；`GET /api/v1/clusters` 在服务端筛选授权范围，详情接口要求 `clusters.read`。

通过根目录 `make build` 或 `make release TARGET_ARCH=amd64`（ARM64 使用 `arm64`） 构建时，前端及安装器会嵌入程序。发布包只含 `slurm-console` 和 `slurm-console.service`，目标 Linux 主机以 root 执行 `./slurm-console install` 即可通过终端向导安装、初始化管理员并启动；`configure` 可重复执行，空输入保留已有值及外部认证来源。向导沿用维护锁、升级快照、失败回退和每日备份，见 [部署说明](../docs/deployment.md)。

未设置 `SLURM_CONSOLE_PUBLIC_DIR` 时，后端优先使用内嵌前端；未准备内嵌资源的开发构建回退到 `../frontend/dist`。显式设置该环境变量时使用指定目录。未知 `/api` 请求始终返回 API 错误，不回退到 HTML。

- `internal/api`：平台 `/api/v1` 路由。
- `internal/application`：PocketBase 初始化、迁移和命令注册。
- `internal/distribution`：内嵌资源、root 安装入口及可重复执行的终端配置向导。
- `internal/auth`、`internal/rbac`：平台身份和集群范围授权。
- `internal/ldapauth`、`internal/pamauth`：LDAP 加密绑定和 PAM 子进程适配。
- `internal/slurm/{client,adapter,models}`：独立的 Slurm 接口适配。
- `internal/services`：应用服务、版本隔离缓存与取消后失效。
- `internal/nodecontrol`：节点状态确认、Drain/Resume 及持久审计。
- `internal/jobcontrol`：显式 Slurm 身份 CLI、最新确认及持久审计取消。
- `internal/audit`：管理变更的事务审计及意图快照。
- `internal/monitoring`、`internal/cache`：后续监控与缓存扩展。
- `migrations`：PocketBase 数据迁移。

不在浏览器保存集群凭据；不将作业、节点持久化为平台业务主表。平台用户、Slurm 身份和集群权限分别建模。

Slurm Adapter 的版本、查询及缓存约束见 `../docs/slurm-adapter.md`。正式服务及本地 Unix socket 请求均以 root 运行，旧 `localUser` 字段仅保留配置读取兼容，不再触发降权。安装器只管理本应用，不安装 LDAP/FreeIPA/slurmrestd；新启用 PAM 仍需可选 helper 和应用专属策略。

作业管理与 `slurm-identity link/unlink` 见 `../docs/jobs.md`。没有显式关联时，即使平台 Super Admin 也不能取消作业；默认仅本人范围。

节点管理见 `../docs/nodes.md`，应用不修改 Slurm 角色或节点配置文件。

分区列表、详情及关联节点资源见 `../docs/partitions.md`；节点数据单独授权，分区接口全部只读。

平台审计查询及管理命令留痕见 `../docs/audits.md`，只有 `platform.audit.read` 可检索，不回读或重试 Slurm 操作。

`backup init-key/create/verify/restore` 和 `healthcheck` 独立于 PocketBase 启动运行；`internal/backup` 负责 age 加密、清单及 SQLite 验证、范围受限恢复，`internal/maintenance` 提供部署/管理命令互斥。详见 `../docs/backups.md`。

资源分配时长报表的纯统计层位于 `internal/usagereport`，运行窗口和既有历史提交窗口分别建模；权限、覆盖和 CSV 说明见 [资源报表](../docs/usage-reports.md)。

`internal/confighistory` 保存字段白名单快照及版本来源。预览与连接保存共用草稿校验，版本与成功审计在同一事务提交；历史读取无外部请求，见 [配置变更对比](../docs/configuration-history.md)。
