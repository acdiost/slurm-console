# 加密备份与恢复

所有维护命令在 Linux 部署主机本地执行，不通过浏览器开放。无需另装备份服务器或加密运行时；应用内置 [age](https://pkg.go.dev/filippo.io/age) 流式加密，默认使用专用 hybrid identity。创建和应用恢复要求 root；只读 verify 可由持有私有密钥文件的管理员执行。

## 备份范围

| 路径 | 内容 |
|---|---|
| `/var/lib/slurm-console/pb_data` | PocketBase 数据库、用户、RBAC、身份映射、操作审计及其他应用数据 |
| `/opt/slurm-console` | 与数据库同一时间点的后端程序和前端资源 |
| `/etc/slurm-console` | 应用环境、LDAP 客户端、批准的集群连接方式及放在此处的凭据 |
| `/etc/systemd/system/slurm-console*.service`、备份 timer | 仅本项目的三个固定服务/定时器文件 |
| 专用 PAM helper、`/etc/pam.d/slurm-console`、存在时的旧版专属 sudo 规则 | 存在时纳入本应用的 PAM 配置 |

固定白名单不包含 Slurm/slurmrestd、Munge、LDAP/FreeIPA/AD 服务、共享 `password-auth` 策略、反向代理及系统账户数据库。JSON 引用的外部 CA/JWT/查询密码文件若放在上述目录以外，由管理员单独保管和恢复。SQLite 的 WAL 会纳入快照，SHM 是可重建缓存，不纳入。密钥默认在 `/var/lib/slurm-console-backup/identity.txt`，独立于备份范围；禁止把密钥放入应用快照目录。

## 创建与校验

安装器首次生成密钥，后续执行保留原密钥。也可手动初始化：

```sh
sudo /opt/slurm-console/slurm-console backup init-key
sudo /opt/slurm-console/slurm-console backup create
sudo /opt/slurm-console/slurm-console backup verify \
  --file /var/backups/slurm-console/manual-<时间>-<编号>.tar.gz.age
```

将示例中的文件名替换为 create 返回的 `file`。也可通过 `--file /私有目录/新文件.tar.gz.age` 指定尚不存在的文件；目录须 root 所有且为 0700。密钥为 0600，备份为 0600，均拒绝不安全的符号链接。`--key /安全目录/identity.txt` 可指定另一份密钥；文件所有者应为执行命令的管理员，不通过参数或输出传递密钥内容。

create 获取维护锁，记录应用原有运行状态，短暂停服后读取一致文件快照，完成加密归档后恢复原运行状态并验证就绪；原本停止的服务保持停止。随后解密到私有临时目录，校验路径、完整清单、每文件 SHA-256、加密流结束标记及两个 SQLite 数据库的 `integrity_check`。只有完整通过才返回 `verified:true`、`databaseVerified:true`。SQLite 检查在私有副本上运行，不会在原数据目录创建 root 所有的 WAL/SHM 文件。

维护期间安装器、PAM 设置及受支持的本地管理命令互斥。不要并发运行旧版本 CLI、直接修改 SQLite 或手工重启服务，这些操作不遵守维护锁。SIGINT/SIGTERM 会让正在创建的归档取消并尝试恢复服务；不要强制终止维护进程。主机断电或 SIGKILL 后应先检查应用状态、维护日志和剩余磁盘空间，再重新执行校验。

快照的明文总量上限 16 GiB、文件数上限 100000、清单上限 16 MiB。备份/校验需要额外临时空间，至少准备数据库副本、完整解密副本及加密归档的空间。失败不会覆盖既有备份。

## 每日备份、保留与独立存储

首次安装启用 `slurm-console-backup.timer`：按**服务器时区**每日 03:15，加 0–10 分钟随机延迟，关机错过后会补跑。后续安装保留已有 timer 的启停选择。

```sh
systemctl list-timers slurm-console-backup.timer
sudo systemctl start slurm-console-backup.service    # 立即备份一次
journalctl -u slurm-console-backup.service -n 40 --no-pager
```

默认保留最近 14 份规范命名的 daily 加密快照。只有新备份校验成功、已配置的第二份复制成功后才清理旧 daily；旧文件也须能解密并通过校验。手动备份、升级前快照、恢复前保全和历史 `.tar.gz` 均不自动删除。无法校验的旧包保留并报错。变更周期/保留数量使用 systemd drop-in，复制 `ExecStart` 时先写空 `ExecStart=`，再提供新的完整命令。暂停可执行 `systemctl disable --now slurm-console-backup.timer`。

管理员先准备独立存储并挂载，在该存储上创建 `root:root 0700` 目录，再编辑 `/etc/slurm-console/backup.env`：

```text
SLURM_CONSOLE_BACKUP_COPY_DIR=/mnt/independent-backups/slurm-console
```

环境文件是 systemd 读取的数据，不作为 shell 脚本执行。下次备份会原子发布第二份加密文件并核对密文哈希；复制失败时保留本地有效备份并让任务报错。也可单次使用 `backup create --copy-dir /mnt/independent-backups/slurm-console`。程序不负责挂载、SSH 登录或第三方存储部署，也不把同机另一目录认定为异机备份；管理员确认目的地确实为独立存储。

返回 `secondaryCopy:"not_configured"` 表示只有本机备份，`"verified"` 表示已完成配置目标的字节校验。第二存储的保留策略由该存储管理员设置。密钥必须另存到受保护的离线位置；不要只留在这台主机，也不要只与备份放在同一个存储。丢失密钥无法恢复加密备份。

## 隔离恢复演练

```sh
sudo /opt/slurm-console/slurm-console backup restore \
  --file /var/backups/slurm-console/manual-<时间>-<编号>.tar.gz.age \
  --to /var/lib/slurm-console-restore-drill
```

`--to` 必须是不存在的目录，且位于应用和密钥路径之外；只解密、检查及创建 root 独占的目录，不停服务、不覆盖当前环境、不安装 PAM/unit、不执行包内程序。内容保持私有且不自动授予执行权限。

先确认 `databaseVerified:true`，再在隔离端口/主机用包内匹配版本验证账户、RBAC、审计及认证集成。当前版本以 root 演练，旧版快照按包内 unit 的运行账户配置演练权限；显式指定演练 `--dir`、回环监听地址和对应认证配置，不能让演练进程指向生产数据目录。完成后停止演练进程，再由管理员清理该私有副本。

## 应用完整恢复

这是覆盖操作，应安排维护窗口。先确认备份来自可信管理员、密钥可用且架构相同；加密保护内容和完整性，不代替备份来源可信性。新主机先建立用于 Slurm 本地 socket 的 `slurm-console` 专用身份和应用依赖，恢复独立密钥及范围外的凭据/系统 PAM 身份。

```sh
sudo /opt/slurm-console/slurm-console backup restore \
  --file /var/backups/slurm-console/manual-<时间>-<编号>.tar.gz.age \
  --apply --confirm RESTORE
```

维护命令不依赖 PocketBase 启动，不提前打开目标数据库或加载已损坏的 LDAP/集群配置。它先完成解密、内容及 SQLite 校验，核对架构和专用 sudoers；然后停服务并生成 `before-restore-*.tar.gz.age` 加密保全。保全包用于保存恢复前证据，允许原数据库损坏，报告 `purpose:"preservation"/databaseVerified:false`，不能当作正常快照再次应用。

所有替换树先准备完毕，再替换应用固定目录/文件，按备份内 unit 重设数据权限和 SELinux 标签：当前 root 版本使用 UID/GID 0，旧版 `slurm-console` 使用该系统身份的 UID/GID，保证升级回退兼容。原文件暂存于同一文件系统的私有路径，原来运行的服务恢复后必须通过就绪检查；失败时回退原目录并尝试恢复原服务。成功后清理暂存原文件，保留加密保全包。原本停止的服务不自动启动，应由管理员按需启动并验收。范围外的 Slurm、目录服务、共享 PAM 策略及反向代理保持管理员原有配置。

恢复会回到备份时的用户、授权、认证映射及审计；备份之后的记录可能只保存在恢复前保全中。恢复对应版本的程序和数据库，不使用破坏性迁移 down。完成后验收 PAM/LDAP 登录、授权、审计和集群读取；外部系统中的作业/节点状态不会随平台数据库回退。

新命令只接受新的加密清单格式。旧安装器产生的 `.tar.gz` 原样保留，不能直接交给新命令应用；由管理员在私有目录检查并按对应版本的旧运维流程处理。

## 验证记录

2026-10-27 在 Rocky Linux 9.7 / ARM64 / SELinux Enforcing 上完成验收：升级快照和每日服务均生成通过加密、哈希及 SQLite 校验的包；重复安装保留应用 PID、密钥和 timer 启停选择。私有挂载命名空间中的安装器故障注入验证了健康检查失败后程序与数据库一同回退。

从每日加密包恢复到独立数据目录，并以包内匹配程序在回环端口启动：PAM 登录、平台用户 ID、RBAC、全部审计记录及前端读取通过。测试程序放在独立 `/opt` 演练目录并设置正确 SELinux 执行标签；生产数据目录没有执行覆盖恢复。演练进程、明文副本及专用转发均已清理，Slurm/slurmrestd 的进程及外部配置校验一致，原 DOWN 节点未改变。后端 106 项测试（含竞态检测）和前端 68 项测试通过。

当前仅启用本机备份，按用户选择暂不配置异机复制目标；解密密钥仍需管理员另行离线保管。

恢复后的 timer 启用状态需管理员重新确认；快照包含本项目 unit 文件，不备份 systemd 的 wants 链接。回退到不支持新备份命令的旧版本时，应停用新 timer，并使用保留的维护程序和对应备份执行后续恢复。
