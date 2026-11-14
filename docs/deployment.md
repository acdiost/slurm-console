# 部署与本地开发

项目只安装 Slurm Console 本身。Slurm/slurmrestd、LDAP、FreeIPA/AD、Munge、反向代理及其证书由系统管理员安装和维护；仓库不提供这些组件的安装脚本、服务单元或演示目录。已有外部服务不会被应用安装脚本修改。

## Linux 部署

### 两文件交付与终端引导（推荐）

交付目录只有 `slurm-console` 和 `slurm-console.service`。前端、安装器和备份服务模板已打进二进制；目标主机不需要源码、Node.js、npm 或 Go。选择与目标 CPU 架构一致的 Linux 版本，以 root 执行：

```sh
chmod +x slurm-console
sudo ./slurm-console install
```

安装器自动准备应用/配置/数据目录、加密备份密钥、systemd 主服务及每日备份任务，交互配置完成后启动并检查就绪。需要 Linux、bash、systemd、flock 和基本系统工具；非 root 安装或配置会被拒绝。应用及 Slurm 本地 Unix socket 连接均使用 root，不再要求创建 `slurm-console` 系统账户。Slurm REST 服务由管理员提供，安装器不修改其配置或启动身份。

终端向导依次设置平台名称、管理员邮箱/名称/密码，可选配置一个 Slurm 连接（本地 socket，或 HTTPS/JWT）。直接回车使用已有值；首次安装使用显示的默认值。首次密码留空时生成随机密码，仅在当前终端显示；已有密码或 JWT 留空时保持不变。密码及 JWT 输入不回显、不进入命令参数或审计；非敏感可选项可输入 `-` 清空。默认监听 `127.0.0.1:18090`，HTTPS 反向代理继续由管理员配置。

Slurm API 支持 `v0.0.40`–`v0.0.44`。版本输入 `auto` 可通过认证后的 OpenAPI 文档选择最高可用且被批准的版本；手动输入可省略 `v`，例如 `0.0.40`。留空保留已有版本，新连接默认 `v0.0.43`。选择结果固定保存，不在业务请求失败后自动切换。版本功能差异及白名单配置见[集群连接配置](cluster-connections.md)。

后续重复运行同一命令即可升级并重新引导，或执行：

```sh
sudo /opt/slurm-console/slurm-console configure
```

重复执行复用管理员、角色授权、集群记录及凭据，保留未选择的其他集成。现有集群的 API 版本、启停、说明及超时沿用已有值。配置过程有停服窗口；开始前生成并验证升级快照，失败时尝试整体回退。没有数据库的首次安装失败后可直接重新执行。输入 EOF 视为取消，不会把缺失输入当作全部接受默认值。

完整环境配置仍位于 `/etc/slurm-console/slurm-console.env`。LDAP、FreeIPA、监控等可选集成继续按各自文档配置；两文件基础包默认使用平台密码认证，现有 PAM helper 和策略会保留，新启用 PAM 仍使用下面的可选 helper 构建/配置流程。

在开发/发布机器生成交付物：

```sh
make install
make release TARGET_ARCH=amd64  # dist/slurm-console-linux-amd64/，x86_64
make release TARGET_ARCH=arm64  # dist/slurm-console-linux-arm64/，aarch64
```

`make release` 自动构建前端并嵌入程序，使用 `CGO_ENABLED=0` 生成 Linux 主程序。不要使用裸 `go build` 制作交付包，它不会更新嵌入资源。交付的 systemd 文件与程序内模板一致，安装命令会自动安装内置模板和备份单元；本机已有 systemd drop-in 保持原样。

已有活动 Super Admin 时，向导默认沿用该账户，包括 PAM/LDAP 等外部认证来源；不会因为缺少本地密码账户而额外创建管理员。外部账户密码留空保留，不能在该向导中修改其密码。

2026-11-09 在 `192.168.100.134`（Linux ARM64）完成两文件包安装与重复配置验收：服务实际 UID/GID 为 root，移走外部前端目录后页面仍可访问，root socket 的 ping、作业、节点、分区读取成功。原有 2 个账户、2 个集群、授权、配置文件、备份密钥保持一致；外部 Slurm/FreeIPA/LDAP 服务配置与 PID 未变化，两份升级快照通过校验，备份 timer 继续运行。独立临时库完成首次管理员创建、正常密码登录、重复配置及密码保留验证，临时库已清理。生产 PAM/LDAP 账户登录由管理员自行验收。`tjzs` SSH 转发在测试前以原身份和 root 请求均重置连接，未修改该既有连接配置。

### 从源码部署及可选 PAM

准备 Node.js 22.16+（22.x）或 24 LTS、npm 10+、Go 1.27.3+、systemd。使用 PAM 时另需 Linux PAM 运行库，构建 helper 需要 gcc 和 PAM 开发包；`--pam` 当前面向 Rocky/RHEL 的 `password-auth` 策略。

```sh
make install
make build
make pam-helper                 # 仅 PAM；须在目标 Linux 架构构建
sudo bash deploy/install.sh --pam
```

不用 PAM 则省略 `make pam-helper` 和 `--pam`。安装器以 root 运行应用和默认 Slurm 本地 socket 请求，安装应用程序、静态文件和 systemd 单元，启动于 `127.0.0.1:18090`。可重复执行升级；程序、前端或单元变化且已有数据库时，先停服并在 `/var/backups/slurm-console/` 生成加密一致快照。前端资源整套替换，应用必须通过数据库就绪检查；升级失败会尝试恢复升级前快照。直接运行源码安装脚本且没有变化、服务已运行时不重启应用。备份与恢复详见 [加密备份](backups.md)。

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
| `deploy/freeipa.example.json` | 应用的独立 FreeIPA HTTPS 连接配置 |
| `deploy/ai.example.json` | 可选 AI 服务接入，默认关闭 |
| `deploy/cluster-profiles.example.json` | 应用的已批准集群连接方式 |

配置集中在 `/etc/slurm-console/slurm-console.env`，安装器保留已有配置。按需取消注释 LDAP/集群配置路径，将编辑后的 JSON 安装为 `root:root 0640`；没有配置就不启用该集成。JSON 只引用服务端凭据文件，密码和令牌不能进入仓库、前端或命令参数。修改后执行 `systemctl restart slurm-console`。

直接运行源码安装脚本不会弹出终端向导；首次账户和授权按 [认证说明](authentication.md) 通过本地 CLI 配置。两文件安装命令会引导创建或复用管理员，应用没有固定默认密码。PAM 关联及故障排查见 [PAM](pam.md)，目录准入条件见 [LDAP](ldap.md)，集群通信要求见 [集群连接](cluster-connections.md)。生产环境由管理员提供 HTTPS 反向代理/VPN，应用保留回环监听。

## 外部组件要求

- **Slurm REST**：管理员提供与集群兼容、启用 `openapi/slurmctld` 和对应 `data_parser/v0.0.40`–`v0.0.44` 插件 的 slurmrestd。Unix socket 连接需要接受 root 客户端，并允许该身份读取 ping、jobs、nodes、partitions；HTTPS/JWT 使用受保护配置中的固定身份及校验过的证书。作业取消还要求连接身份具备目标作业的 Slurm 取消权限，并完成[平台身份关联](jobs.md)。节点控制需要 Slurm 节点更新权限，见[节点管理](nodes.md)。安装器不授予上游权限或修改外部服务身份，`PrivateData` 等设置仍决定可见范围。
- **LDAP/FreeIPA/AD**：可选。管理员提供 LDAPS 或强制 StartTLS、受信任 CA、专用只读查询身份、用户子树、唯一身份属性及反映禁用/锁定/准入规则的过滤器。本项目不管理目录服务、证书签发、系统域加入或目录用户生命周期。FreeIPA 现支持独立认证、用户/组管理及直接成员角色映射，见 [FreeIPA 接入](freeipa.md)；AD 专属组同步尚未实现。
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

Slurm `auth/local` 取 Unix socket 对端身份。当前由 root 主进程直接请求 socket；终端向导显式保存 `localUser: "root"`。旧 profile 的 `localUser` 字段保留读取兼容，但不再选择其他请求身份。单次请求保留截止时间、大小限制和写结果核验，不自动重发。HTTPS/JWT 连接使用原配置。Slurm 上游对 root 连接身份的准入与管理权限继续由集群管理员配置。

2026-10-28 在现有 Rocky Linux 虚拟机完成 root 迁移验收：服务实际 UID/GID 均为 0；PAM 登录、LDAP TLS 查询、Slurm 健康/作业/节点/分区及本地账户创建、锁定、解锁与审计通过，临时账户已清理。重复安装及两个配置脚本保持运行 PID；独立挂载命名空间验证 root/旧版快照恢复，以及模拟升级失败后的程序、数据库和旧版数据权限回退。原有 Slurm、LDAP、PAM 配置内容及外部 Slurm 服务 PID 保持一致。

GPU 实际监控使用管理员既有 Prometheus/DCGM；应用只读取受保护的 metrics-profiles.json，安装器不安装第三方监控组件。配置见 [GPU 监控](gpu-monitoring.md)。

告警通知随应用迁移自动启用，无新增配置或第三方告警服务；用户创建规则后才开始监控。反向代理应允许 text/event-stream、关闭响应缓冲，并使读取超时大于 30 秒；实时连接不可用时浏览器继续轮询。规则和通知随应用数据库备份，详见 [告警通知](notifications.md)。

容量采样随应用启动，使用现有已启用集群及可信连接方式，无需新增参数或第三方组件。历史从上线后积累，SQLite 汇总记录受时间和数量限制，并随已有数据库备份。详见 [容量分析](capacity.md)。

可选模型服务由管理员自行提供。应用只按受保护配置接入批准的 Chat Completions 兼容服务，不安装第三方模型组件；默认关闭，配置和发送范围见 [AI 运维辅助](ai-assistance.md)。

平台名称、登录公告及非敏感 AI 覆盖配置保存在应用 SQLite，随现有加密备份一起留存；没有额外服务或部署组件。服务器 AI JSON 继续提供密钥/CA 引用，覆盖优先级及用户授权说明见 [平台设置](platform-settings.md)。
