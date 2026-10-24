# PAM 验证记录

验证环境：2026-10-24，用户提供的 Rocky Linux 9.7、Linux ARM64 虚拟机；Slurm 25.11.4。校准时间后安装 gcc / pam-devel，在目标主机用 Go 1.27.3 原生编译 PAM helper，Web 后端以普通 `slurm-console` 用户运行。保持 SELinux enforcing。

本地通过 TypeScript、ESLint、前端构建、23 项前端测试，以及 Go vet 和含竞态检测的 19 项后端测试。后端测试使用真实 PocketBase/SQLite 和认证 API；PAM provider 的单元测试覆盖独立进程协议、超时和输出边界。

虚拟机通过独立的临时非 root 系统账户、数据库和 systemd 服务验证，完成后清理测试账户与数据：

- 原生 PAM 正确密码通过，错误密码、空密码和 UID 0 拒绝。
- 用户名与邮箱均可通过 PocketBase 原生登录端点取得平台令牌。
- 系统用户名必须显式关联平台账户；原平台密码不能成为 PAM 的备用入口。
- 从本地认证切换为 PAM 后，原集群 Viewer 绑定保留，查询只返回授权集群。
- 原生令牌刷新成功，登录限流在第六次请求生效。
- `chage` 设置系统账户到期后，已有令牌的用户/集群查询及刷新返回 401；恢复账户后查询成功。
- 系统密码过期要求变更、`passwd -l` 锁定密码均拒绝新认证。
- SELinux 持续为 enforcing，`slurmctld` 和 `slurmd` 保持 active。

此验证不代表已接入 slurmrestd，也未改动 SSH、Slurm 或 authselect 公共策略。系统策略可由管理员继续维护；PAM 账户登录不自动授予平台或 Slurm 权限。
