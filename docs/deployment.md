# 部署与本地开发

项目只安装 Slurm Console 本身。Slurm/slurmrestd、LDAP、FreeIPA/AD、Munge、反向代理及其证书由系统管理员安装和维护；仓库不提供这些组件的安装脚本、服务单元或演示目录。已有外部服务不会被应用安装脚本修改。

## Linux 部署

准备 Node.js 22.16+（22.x）或 24 LTS、npm 10+、Go 1.27.3+、systemd。使用 PAM 时另需 Linux PAM 运行库、sudo，构建 helper 需要 gcc 和 PAM 开发包；`--pam` 当前面向 Rocky/RHEL 的 `password-auth` 策略。

```sh
make install
make build
make pam-helper                 # 仅 PAM；须在目标 Linux 架构构建
sudo bash deploy/install.sh --pam
```

不用 PAM 则省略 `make pam-helper` 和 `--pam`。安装器创建普通服务账户，安装应用程序、静态文件和单个 systemd unit，启动于 `127.0.0.1:18090`。可重复执行升级；程序、前端或单元变化且已有数据库时，先停服并在 `/var/backups/slurm-console/` 生成加密一致快照。前端资源整套替换，应用必须通过数据库就绪检查；升级失败会尝试恢复升级前快照。没有变化且服务已运行时不重启应用。备份与恢复详见 [加密备份](backups.md)。

本应用仅使用以下部署文件：

| 文件 | 用途 |
|---|---|
| `deploy/install.sh` | 安装/更新应用并启动服务 |
| `deploy/setup-pam.sh` | 幂等配置并验证本应用 PAM/sudo 权限 |
| `deploy/slurm-console.service` | 普通用户运行的应用单元 |
| `deploy/slurm-console-backup.service`、`.timer` | root 执行的每日加密备份，默认保留 14 份 daily |
| `deploy/backup.env.example` | 可选独立存储复制目标（不包含密钥内容） |
| `deploy/slurm-console.env.example` | 所有可选集成的统一环境文件 |
| `deploy/ldap.example.json` | 应用的 LDAP 客户端配置 |
| `deploy/cluster-profiles.example.json` | 应用的已批准集群连接方式 |

配置集中在 `/etc/slurm-console/slurm-console.env`，安装器保留已有配置。按需取消注释 LDAP/集群配置路径，将编辑后的 JSON 安装为 `root:slurm-console 0640`；没有配置就不启用该集成。JSON 只引用服务端凭据文件，密码和令牌不能进入仓库、前端或命令参数。修改后执行 `systemctl restart slurm-console`。

首次账户和授权按 [认证说明](authentication.md) 通过本地 CLI 配置；没有默认账户或密码。PAM 关联及故障排查见 [PAM](pam.md)，目录准入条件见 [LDAP](ldap.md)，集群通信要求见 [集群连接](cluster-connections.md)。生产环境由管理员提供 HTTPS 反向代理/VPN，应用保留回环监听。

## 外部组件要求

- **Slurm REST**：管理员提供与集群兼容、启用 `openapi/slurmctld` 和 `data_parser/v0.0.43` 的 slurmrestd，确保固定服务身份可以读取 ping、jobs、nodes、partitions。当前支持受保护 Unix socket 的本地认证，或验证证书的 HTTPS/JWT。作业取消还要求固定身份具备目标作业的 Slurm 取消权限，并完成[平台身份关联](jobs.md)。节点控制需要具备 Slurm 节点更新权限的固定服务身份，见[节点管理](nodes.md)。应用不要求 root/SlurmUser 权限，也不自动扩大上游权限；Slurm `PrivateData` 等设置仍决定该身份可见范围。
- **LDAP/FreeIPA/AD**：可选。管理员提供 LDAPS 或强制 StartTLS、受信任 CA、专用只读查询身份、用户子树、唯一身份属性及反映禁用/锁定/准入规则的过滤器。本项目不管理目录服务、证书签发、系统域加入或目录用户生命周期。FreeIPA/AD 专属组同步尚未实现。
- **PAM**：本机系统身份来源和共享 PAM 策略由管理员维护。本项目只设置独立 `slurm-console` PAM service 和只允许运行专用 helper 的 sudo 规则。

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
