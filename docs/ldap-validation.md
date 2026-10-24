# LDAP 验证记录

2026-10-24，在用户提供的 Rocky Linux 9.7 / ARM64 虚拟机验证，使用现有 OpenLDAP 2.6.8 程序另建测试实例；原有 389/636 目录不变。新实例只监听回环地址 7389/3269，使用自建测试 CA、只读查询账户及独立 MDB。

本地通过 TypeScript、ESLint、前端生产构建、24 项前端测试，以及 Go vet、含竞态检查的 28 项后端测试。新增检查覆盖安全配置、拒绝不可信证书、StartTLS 失败不发送绑定凭据、TLS 握手可取消、LDAP 关联的令牌轮换、认证来源隔离和 RBAC。

`deploy/ldap-lab/verify.py` 在独立平台数据库和临时 systemd 服务中验证：

- LDAPS 用户名/邮箱登录、令牌刷新、每 IP 登录限流。
- 未关联目录账户被拒绝，平台旧密码不能成为 LDAP 的备用入口。
- 关联后原集群 Viewer 权限保留，只能查询授权集群。
- `pwdAccountLockedTime`、`pwdReset` 触发资格拒绝，已有令牌/刷新也不能通过。
- 重复 uid 拒绝；目录账户删除后同名重建产生新 UUID，旧映射及令牌被拒绝。
- 目录停止时登录/用户查询/刷新返回 503；恢复目录就绪后可继续使用有效映射。
- `ldap://` 经 StartTLS 的用户登录和令牌刷新成功；不信任测试 CA 的连接被拒绝。
- SELinux 保持 enforcing；原 OpenLDAP、PAM 平台服务、slurmctld/slurmd 保持 active。

临时测试条目、数据库和应用服务完成后清理。保留隔离演示目录及 `ldapdemo`，平台账户没有任何角色；原 `consoleadmin` 仍使用 PAM 并保留 Super Admin。部署升级前已停服备份原平台数据库。

本次未验证 AD/FreeIPA 专属生命周期，也没有实现目录组到平台角色同步或 slurmrestd 连接。目录准入/锁定规则以服务端配置的过滤器和上游密码策略为准。
