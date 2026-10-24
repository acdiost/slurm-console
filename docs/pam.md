# Linux PAM 登录

PAM 验证后端所在 Linux 主机的系统身份。当前提供 Rocky Linux 9 / RHEL 9 的 `password-auth` 集成，其他发行版需要管理员适配应用专属 PAM 策略。PAM 不通过 SSH 发送用户密码，也不授予 Slurm 或平台管理权限。

## 构建与安装

在目标 Linux 架构上构建，Go 1.27.3+、gcc、PAM 开发包及 sudo 为必需。前端可在开发机执行 `npm --prefix frontend ci && npm --prefix frontend run build` 后将 `frontend/dist/` 传入同一检出目录。

```sh
dnf install -y gcc pam-devel sudo
cd backend
go build -o slurm-console .
CGO_ENABLED=1 go build -o slurm-console-pam-helper ./cmd/pam-helper
cd ..
bash deploy/install-pam-rocky.sh
```

安装脚本需要 root，只安装本应用的二进制、静态资源、PAM 文件、sudoers 文件和 systemd 单元。它创建无登录 shell 的 `slurm-console` 服务账户，不修改 SSH、Slurm、authselect 的 `password-auth/system-auth`。已有 `/etc/pam.d/slurm-console` 会保留。升级时先停止服务，并按现有运维流程备份数据库，再安装新版本。

安装后的路径：

| 路径 | 用途与所有者 |
|---|---|
| `/opt/slurm-console/slurm-console`、`public/` | root 所有，普通用户运行/读取 |
| `/usr/local/libexec/slurm-console-pam-helper` | root 所有、0755，唯一允许提权的程序 |
| `/etc/pam.d/slurm-console` | root 所有、0644，仅此应用的认证/account 策略 |
| `/etc/sudoers.d/slurm-console-pam` | root 所有、0440，只允许服务账户运行无参数 helper |
| `/var/lib/slurm-console/pb_data` | 服务账户所有、0700，平台数据库 |

helper 固定使用 `slurm-console` PAM service，拒绝 UID 0、空密码、多次密码询问、身份重写及可被非 root 写入的策略文件。密码只通过 stdin 传入，不使用参数、环境变量或临时文件。helper 不创建会话、不设置用户凭据、不运行用户命令；8 秒硬超时，Web 侧最多并发 4 个 helper。Web 服务保持普通用户身份；因此此 systemd 单元不能增加 `NoNewPrivileges=true`，否则 sudo 将被阻止。

## 关联平台账户

服务停止时执行管理命令，所有命令使用同一数据库目录。下例中的 `alice` 必须是主机上已有、非 root 的有效系统账户。命令只保存用户名关联，不读取或设置系统密码。

```sh
umask 077
runuser -u slurm-console -- /opt/slurm-console/slurm-console users create-pam \
  --email alice@example.com --username alice --name Alice \
  --dir=/var/lib/slurm-console/pb_data
```

将已有平台账户切换为 PAM：

```sh
runuser -u slurm-console -- /opt/slurm-console/slurm-console users link-pam \
  --email alice@example.com --username alice \
  --dir=/var/lib/slurm-console/pb_data
```

`link-pam` 保留用户 ID、启用状态、显示名称与所有授权，替换原平台密码哈希并轮换 tokenKey，使旧令牌失效。系统用户名唯一，不允许多个平台用户关联同一系统账户。新建账户不分配角色；按需要继续用 `access grant` 显式授权，不自动提升为管理员。

```sh
runuser -u slurm-console -- /opt/slurm-console/slurm-console access grant \
  --email alice@example.com --role viewer --cluster research-a \
  --dir=/var/lib/slurm-console/pb_data
systemctl enable --now slurm-console
systemctl status slurm-console
```

未登记集群时先用 `access register-cluster` 登记；详见 [首次授权](authentication.md)。登录界面可输入系统用户名或平台邮箱，密码使用该系统账户密码。PocketBase 登录请求最长允许 255 个字符。公开注册、系统账户自动开通、PAM 密码修改和多因素交互均未开放。

## 本地浏览器联调

单元默认监听虚拟机的 `127.0.0.1:18090`，可通过 SSH 转发访问构建后的完整应用：

```sh
ssh -N -L 18090:127.0.0.1:18090 root@192.168.100.134
```

打开 `http://127.0.0.1:18090`。开发前端可使用同一转发：`SLURM_CONSOLE_API_TARGET=http://127.0.0.1:18090 npm --prefix frontend run dev`。正式内网发布应使用 HTTPS 反向代理；本任务不修改防火墙或对外开放端口。

## 配置与故障排查

| 环境变量 | 默认 | 含义 |
|---|---|---|
| `SLURM_CONSOLE_PAM_ENABLED` | `false` | 启用 helper；为 true 时启动必须通过探测 |
| `SLURM_CONSOLE_PAM_HELPER` | `/usr/local/libexec/slurm-console-pam-helper` | 绝对路径，应由 root 所有、父目录不可由服务账户写入 |
| `SLURM_CONSOLE_PAM_SUDO` | `true` | 通过 `/usr/bin/sudo -n` 调用；false 仅用于可信的特权测试环境 |

PAM 未启用或 helper 故障时，已关联 PAM 的账户会拒绝登录，不能使用数据库中的占位密码绕过。未关联的本地账户继续使用平台密码。`probe` 检查 helper、运行权限及策略文件元数据，真实模块行为由用户登录和 account 检查验证。

先检查 `journalctl -u slurm-console`、`/var/log/secure`、`visudo -c`、策略文件和 helper 的所有者/权限。保持 SELinux enforcing；不要通过禁用 SELinux、扩大 sudo 权限或让 Web 服务以 root 运行来排错。系统密码锁定后的新登录由 PAM auth 策略拒绝；已有 JWT 是否被 PAM account 策略拒绝取决于系统配置。需要立即撤销平台访问时，将平台 `active` 设为 false；单独 `passwd -l` 不保证撤销已发令牌。

参考：[Linux-PAM 开发接口](https://www.linux-pam.org/Linux-PAM-html/adg-interface.html)、[Go PAM binding v2](https://github.com/msteinert/pam)。
