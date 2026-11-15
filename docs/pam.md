# Linux PAM 登录

PAM 验证后端 Linux 主机的系统身份。发布包内置 Linux amd64/arm64 认证组件，支持 glibc 与系统 `libpam.so.0`；RHEL/Rocky 的 `password-auth`、Debian/Ubuntu 的 `common-auth/common-account` 及 `system-auth` 栈可自动识别。PAM 不通过 SSH 发送用户密码，也不授予 Slurm 或平台管理权限。

## 页面接入引导

管理员从工作台的「开始接入配置」或平台管理的「接入引导」进入 `/setup`，点击「一键配置并启用 PAM」。后端从当前发布包安装匹配架构的认证组件，自动生成应用专属策略，调用真实 PAM 运行库复核，通过后立即启用，无需重启应用。已有安全的专属策略与显式自定义认证组件会保留。用户无需源码、Go、make、C 编译器或开发头文件；此步骤也可跳过。

网页操作要求 `platform.settings.manage`，不接收任意可执行文件或系统用户密码。启用状态及独立 revision 持久化到平台数据库，立即生效并在重启后恢复；首次显式保存后优先于服务环境中的 `SLURM_CONSOLE_PAM_ENABLED`，未保存过则保留原环境配置。helper 不可用时，PAM 认证拒绝访问，本地管理员仍可登录修复。启用不改变已有账户认证来源；通过完成页的「管理用户与角色」（`/settings?tab=users`）创建或关联 PAM 身份，再分配角色与集群权限。

配置要求 Linux amd64/arm64 后端以 root 运行，并具备 glibc、系统 PAM 运行库与已有认证栈；musl/Alpine 不支持此认证组件。缺少运行库、完整发布组件或安全认证栈时，页面会返回具体问题，保留本地管理员登录。组件、策略创建、PAM 检测、启用设置与审计均完成后才报告成功；失败恢复本次创建或替换的文件，保留管理员并发修改。已启用时可点击「重新配置并复核 PAM」更新当前发布组件并检查；「重新检测」只检查状态。

## 发布与命令行配置

发布机器执行 `make release TARGET_ARCH=amd64`（或 `arm64`），打包脚本用 `CGO_ENABLED=0` 构建对应 Linux 认证组件并嵌入主程序。仍只交付 `slurm-console` 和 `slurm-console.service`，主程序保持纯 Go；认证组件在独立进程中按需加载系统 PAM 库。

目标主机优先使用页面一键配置。无页面时，可使用同一安装与复核流程：

```sh
sudo /opt/slurm-console/slurm-console pam configure \
  --dir=/var/lib/slurm-console/pb_data
```

此命令要求 Linux root，配置数据库中的启用状态，后端服务重启后加载。源码部署的 `sudo bash deploy/install.sh --pam` 与 `deploy/setup-pam.sh` 也调用该命令，已有活动服务会由脚本重启；页面配置直接激活当前进程，不需要重启。部署与备份维护锁串行化主机文件变化；既有 LDAP、集群环境配置、共享 PAM 策略、系统账户与 Slurm 保持原样。

安装后的路径：

| 路径 | 用途与所有者 |
|---|---|
| `/opt/slurm-console/slurm-console`、`public/` | root 所有，以 root 运行/读取 |
| `/usr/local/libexec/slurm-console-pam-helper` | root 所有、0755，由 root 服务直接调用的独立认证程序 |
| `/etc/pam.d/slurm-console` | root 所有、0644，仅此应用的认证/account 策略 |
| 旧版专属 sudo 规则 | 升级时仅删除内容完全匹配的旧规则，新部署无需 sudo |
| `/var/lib/slurm-console/pb_data` | root:root、0700，平台数据库 |

helper 固定使用 `slurm-console` PAM service，拒绝 UID 0、空密码、多次密码询问、身份重写及可被非 root 写入的策略文件。密码只通过 stdin 传入，不使用参数、环境变量或临时文件。helper 不创建会话、不设置用户凭据、不运行用户命令；8 秒硬超时，Web 侧最多并发 4 个 helper，最多接纳 16 个处理中/排队请求；排队计入 9 秒总截止时间，取消会释放队列位置，溢出或超时返回 503。每次请求仍独立执行 account 检查。Web 服务以 root 身份直接执行 helper，不再通过 sudo 提权。root 作为服务运行身份不改变 helper 对 UID 0 登录的拒绝策略。

## 关联平台账户

服务停止时执行管理命令，所有命令使用同一数据库目录。下例中的 `alice` 必须是主机上已有、非 root 的有效系统账户。命令只保存用户名关联，不读取或设置系统密码。

```sh
umask 077
/opt/slurm-console/slurm-console users create-pam \
  --email alice@example.com --username alice --name Alice \
  --dir=/var/lib/slurm-console/pb_data
```

将已有平台账户切换为 PAM：

```sh
/opt/slurm-console/slurm-console users link-pam \
  --email alice@example.com --username alice \
  --dir=/var/lib/slurm-console/pb_data
```

`link-pam` 保留用户 ID、启用状态、显示名称与所有授权，替换原平台密码哈希并轮换 tokenKey，使旧令牌失效。系统用户名唯一，不允许多个平台用户关联同一系统账户。新建账户不分配角色；按需要继续用 `access grant` 显式授权，不自动提升为管理员。

```sh
/opt/slurm-console/slurm-console access grant \
  --email alice@example.com --role viewer --cluster research-a \
  --dir=/var/lib/slurm-console/pb_data
systemctl enable --now slurm-console
systemctl status slurm-console
```

未登记集群时先用 `access register-cluster` 登记；详见 [首次授权](authentication.md)。登录界面可输入系统用户名或平台邮箱，密码使用该系统账户密码。PocketBase 登录请求最长允许 255 个字符。公开注册、系统账户自动开通、PAM 密码修改和多因素交互均未开放。

## 本地浏览器联调

单元默认监听虚拟机的 `127.0.0.1:6829`，可通过 SSH 转发访问构建后的完整应用：

```sh
ssh -N -L 6829:127.0.0.1:6829 root@your-server
```

打开 `http://127.0.0.1:6829`。开发前端可使用同一转发：`SLURM_CONSOLE_API_TARGET=http://127.0.0.1:6829 npm --prefix frontend run dev`。正式内网发布应使用 HTTPS 反向代理；本任务不修改防火墙或对外开放端口。

## 配置与故障排查

| 环境变量 | 默认 | 含义 |
|---|---|---|
| `SLURM_CONSOLE_PAM_ENABLED` | `false` | 启用 helper；为 true 时启动必须通过探测 |
| `SLURM_CONSOLE_PAM_HELPER` | `/usr/local/libexec/slurm-console-pam-helper` | 绝对路径，应由 root 所有、父目录不可由服务账户写入 |
| `SLURM_CONSOLE_PAM_SUDO` | root 时 `false` | root 始终直接调用；旧版非 root 运行可显式选择 sudo，部署脚本写入 false |

PAM 未启用或 helper 故障时，已关联 PAM 的账户会拒绝登录，不能使用数据库中的占位密码绕过。未关联的本地账户继续使用平台密码。`probe` 检查 helper、运行权限及策略文件元数据，真实模块行为由用户登录和 account 检查验证。

先检查 `journalctl -u slurm-console`、`/var/log/secure`、策略文件和 helper 的所有者/权限。部署保持 SELinux enforcing；先核实服务实际 UID、helper 及策略权限。系统密码锁定后的新登录由 PAM auth 策略拒绝；已有 JWT 是否被 PAM account 策略拒绝取决于系统配置。需要立即撤销平台访问时，将平台 `active` 设为 false；单独 `passwd -l` 不保证撤销已发令牌。

参考：[Linux-PAM 开发接口](https://www.linux-pam.org/Linux-PAM-html/adg-interface.html)、[purego v0.11.3](https://github.com/ebitengine/purego/tree/v0.11.3)。
