# 部署与本地开发

项目只安装 Slurm Console 本身。Slurm/slurmrestd、LDAP、FreeIPA/AD、Munge、反向代理及其证书由系统管理员安装和维护；仓库不提供这些组件的安装脚本、服务单元或演示目录。已有外部服务不会被应用安装脚本修改。

## Linux 部署

准备 Node.js 22.16+（22.x）或 24 LTS、npm 10+、Go 1.27.3+、systemd。使用 PAM 时另需 Linux PAM 运行库，构建 helper 需要 gcc 和 PAM 开发包；`--pam` 当前面向 Rocky/RHEL 的 `password-auth` 策略。

```sh
make install
make build
make pam-helper                 # 仅 PAM；须在目标 Linux 架构构建
sudo bash deploy/install.sh --pam
```

不用 PAM 则省略 `make pam-helper` 和 `--pam`。安装器以 root 运行应用，保留专用 `slurm-console` 系统身份用于 Slurm 本地 socket 认证，安装应用程序、静态文件和 systemd 单元，启动于 `127.0.0.1:18090`。可重复执行升级；程序、前端或单元变化且已有数据库时，先停服并在 `/var/backups/slurm-console/` 生成加密一致快照。前端资源整套替换，应用必须通过数据库就绪检查；升级失败会尝试恢复升级前快照。没有变化且服务已运行时不重启应用。备份与恢复详见 [加密备份](backups.md)。

本应用仅使用以下部署文件：

| 文件 | 用途 |
|---|---|
| `deploy/install.sh` | 安装/更新应用并启动服务 |
| `deploy/setup-pam.sh` | 幂等配置并验证本应用 PAM，root 直接调用 helper |
| `deploy/slurm-console.service` | root 运行的应用单元 |
| `deploy/slurm-console-backup.service`、`.timer` | root 执行的每日加密备份，默认保留 14 份 daily |
| `deploy/backup.env.example` | 可选独立存储复制目标（不包含密钥内容） |
| `deploy/slurm-console.env.example` | 所有可选集成的统一环境文件 |
| `deploy/ldap.example.json` | 应用的 LDAP 客户端配置 |
| `deploy/cluster-profiles.example.json` | 应用的已批准集群连接方式 |

配置集中在 `/etc/slurm-console/slurm-console.env`，安装器保留已有配置。按需取消注释 LDAP/集群配置路径，将编辑后的 JSON 安装为 `root:root 0640`；没有配置就不启用该集成。JSON 只引用服务端凭据文件，密码和令牌不能进入仓库、前端或命令参数。修改后执行 `systemctl restart slurm-console`。

首次账户和授权按 [认证说明](authentication.md) 通过本地 CLI 配置；没有默认账户或密码。PAM 关联及故障排查见 [PAM](pam.md)，目录准入条件见 [LDAP](ldap.md)，集群通信要求见 [集群连接](cluster-connections.md)。生产环境由管理员提供 HTTPS 反向代理/VPN，应用保留回环监听。

## 外部组件要求

- **Slurm REST**：管理员提供与集群兼容、启用 `openapi/slurmctld` 和 `data_parser/v0.0.43` 的 slurmrestd，确保固定服务身份可以读取 ping、jobs、nodes、partitions。当前支持受保护 Unix socket 的本地认证，或验证证书的 HTTPS/JWT。作业取消还要求固定身份具备目标作业的 Slurm 取消权限，并完成[平台身份关联](jobs.md)。节点控制需要具备 Slurm 节点更新权限的固定服务身份，见[节点管理](nodes.md)。应用不要求 root/SlurmUser 权限，也不自动扩大上游权限；Slurm `PrivateData` 等设置仍决定该身份可见范围。
- **LDAP/FreeIPA/AD**：可选。管理员提供 LDAPS 或强制 StartTLS、受信任 CA、专用只读查询身份、用户子树、唯一身份属性及反映禁用/锁定/准入规则的过滤器。本项目不管理目录服务、证书签发、系统域加入或目录用户生命周期。FreeIPA/AD 专属组同步尚未实现。
- **PAM**：本机系统身份来源和共享 PAM 策略由管理员维护。本项目只设置独立 `slurm-console` PAM service，root 服务直接运行专用 helper，无需 sudo 规则。

## 本地开发

```sh
make install
make dev-backend  # 终端一，127.0.0.1:8090
make dev          # 终端二，打开 Vite 输出地址
make check        # TypeScript、ESLint、go vet
make test         # 前端交互及真实 PocketBase/SQLite 测试
make build
```

端口冲突时，使用 `make dev-backend BACKEND_HTTP=127.0.0.1:18090`，前端使用 `SLURM_CONSOLE_API_TARGET=http://127.0.0.1:18090 make dev`。`SLURM_CONSOLE_PUBLIC_DIR` 可覆盖静态文件目录，`--dir` 指定数据库目录；CLI 与服务必须使用同一数据库。未知 `/api` 不回退到 SPA。

在其他 Linux 发行版上，不使用 `--pam` 即可安装应用；PAM 接入需要管理员提供与该系统匹配的应用专属策略。每日备份与完整恢复工具已提供；独立存储挂载、复制目标与密钥离线保管由管理员配置，见 [备份与恢复](backups.md)。

部署使用系统自带的 `flock` 串行化维护操作。加密密钥首次生成在 `/var/lib/slurm-console-backup/identity.txt`，保留已有密钥；后续安装保留已有备份 timer 的启停选择。安装后用 `systemctl status slurm-console`、`systemctl list-timers slurm-console-backup.timer` 及 `journalctl -u slurm-console-backup.service` 验收。

Linux 系统用户查询的构建、来源映射及受限 SSH 配置见 [Linux 系统用户查询](linux-users.md)。查询器复用应用二进制的独立子命令；本机随 root 服务运行，固定 SSH 来源仍使用专用普通查询身份，无需 sudo。只有确实位于集群身份主机的应用才配置本机来源，远端集群必须指定其身份来源。

如启用本机账户写操作，升级应用后执行 `sudo bash deploy/setup-linux-users.sh <集群记录ID>`，建立限定子命令、UID 范围与保护名单的主机策略。默认部署继续只读；不安装 LDAP/FreeIPA/Slurm 等外部组件，见 [本地账户管理](linux-users.md#本地账户创建锁定与解锁)。

### root 运行与旧版迁移

`User=root`、`Group=root`，应用目录和数据库迁移为 root 所有。数据库保持 0700；父目录 `/var/lib/slurm-console` 为 root:root 0711，以兼容旧版快照回退时的路径遍历。安装器先停服并完成加密快照，再迁移权限；PAM 和本地账户操作直接运行固定 helper，无需 sudo/nsenter。`ProtectSystem=false` 允许标准账户工具修改主机账户数据库。平台 RBAC、确认、审计、UID 范围和保护名单继续生效，PAM 仍拒绝 root 登录。

Slurm `auth/local` 取 Unix socket 对端身份，部分部署拒绝 root。因此 root 主服务以清空环境、只保留专用 UID/GID 的短时子进程执行本地 REST 请求，继续使用 `slurm-console` 身份，且不继承 root 附加组。子进程不打开平台数据库，单次请求有截止时间和大小限制；写操作丢失结果仍按未知状态核实，绝不自动重发。HTTPS/JWT 连接使用原配置。Slurm 上游是否授予该连接身份管理权限，继续由集群管理员负责。

2026-10-28 在现有 Rocky Linux 虚拟机完成 root 迁移验收：服务实际 UID/GID 均为 0；PAM 登录、LDAP TLS 查询、Slurm 健康/作业/节点/分区及本地账户创建、锁定、解锁与审计通过，临时账户已清理。重复安装及两个配置脚本保持运行 PID；独立挂载命名空间验证 root/旧版快照恢复，以及模拟升级失败后的程序、数据库和旧版数据权限回退。原有 Slurm、LDAP、PAM 配置内容及外部 Slurm 服务 PID 保持一致。

GPU 实际监控使用管理员既有 Prometheus/DCGM；应用只读取受保护的 metrics-profiles.json，安装器不安装第三方监控组件。配置见 [GPU 监控](gpu-monitoring.md)。

告警通知随应用迁移自动启用，无新增配置或第三方告警服务；用户创建规则后才开始监控。反向代理应允许 text/event-stream、关闭响应缓冲，并使读取超时大于 30 秒；实时连接不可用时浏览器继续轮询。规则和通知随应用数据库备份，详见 [告警通知](notifications.md)。
