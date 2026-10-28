# LDAP 目录认证

平台支持本地密码、PAM、LDAP 三种认证来源。同一登录界面使用用户名或平台邮箱，后端根据已关联账户的 `auth_source` 选择认证方式，不尝试其他来源的密码。LDAP 账户由管理员显式关联，不自动注册、不按邮箱合并账户、不从目录组自动授予角色。

## 连接与配置

LDAP 直接由 root 运行的 Go 后端访问，不经过 PAM、SSH 或特权 helper。固定使用 `go-ldap/ldap/v3`，支持验证证书和主机名的 LDAPS、StartTLS。`ldap://` 始终先执行 StartTLS，失败后立即关闭连接；没有明文绑定或跳过证书校验的选项。目录查询账户只需读取指定用户子树及身份/资格属性，不应具备目录管理或修改密码权限。

配置模板见 [`deploy/ldap.example.json`](../deploy/ldap.example.json)。将配置安装为 `/etc/slurm-console/ldap.json`，设置环境变量 `SLURM_CONSOLE_LDAP_CONFIG` 为其绝对路径。未设置时关闭 LDAP，但已关联 LDAP 的账户仍拒绝平台密码登录。

| 字段 | 含义 |
|---|---|
| `url` | `ldaps://host:636` 或需要 StartTLS 的 `ldap://host:389`，不允许 URL 内带凭据 |
| `baseDN` | 用户查询子树，限制搜索范围 |
| `bindDN` | 专用只读查询账户 DN，禁止匿名查询回退 |
| `bindPasswordFile` | 查询账户密码文件的绝对路径，内容只包含密码，可有一个结尾换行 |
| `caFile` | 可选的 PEM CA 文件；为空时使用操作系统信任库 |
| `usernameAttribute` | 默认 `uid`，用于校验返回的唯一规范用户名 |
| `idAttribute` | 默认 `entryUUID`，用于绑定不可变目录身份；也支持二进制属性，例如 AD 的 `objectGUID` |
| `userFilter` | 含且仅含一个 `{username}` 的 LDAP 过滤器，后端按 RFC 4515 转义输入 |

配置与 CA 文件不能被组或其他用户写入；密码文件还禁止其他用户的任何访问权限。建议配置和密码均 `root:root 0640`，CA 为 `root:root 0644`，父目录由 root 管理。不把查询密码放进 JSON、环境变量、命令参数或仓库。配置和查询密码在进程启动时读取，修改后重启服务。

默认过滤器面向 OpenLDAP `inetOrgPerson`，要求 `uid` 匹配，并排除存在 `pwdAccountLockedTime` 或 `pwdReset=TRUE` 的条目。默认策略对锁定采取保守处理：锁定时间属性尚未由目录清除时仍拒绝平台访问；有自动解锁需求时应由目录管理员配置匹配其策略的过滤器。LDAP 没有跨产品统一的禁用/过期属性，生产目录必须按自身规则设置资格过滤器，不能认为一次密码绑定能够撤销所有已有平台令牌。

例如，AD 应按实际 OU 使用自己的对象类型、`sAMAccountName`、`objectGUID`，并将禁用状态及准入组条件加入过滤器；本版本未验证 AD/FreeIPA 的完整生命周期、组同步或专属策略。AD 不应使用默认 OpenLDAP 过滤器。

每次登录执行只读服务绑定、限量查询、唯一标识比对及用户 DN 密码绑定，同时读取服务端返回的密码策略控制并拒绝其错误状态。刷新令牌和业务请求重新执行只读查询、资格过滤及唯一标识比对，不保留用户密码。证书、连接、超时或查询服务绑定失败返回 503，未关联、禁用、锁定、匹配不唯一或标识改变则拒绝认证/返回 401。查询最多接受一条身份记录，禁止别名展开和自动跟随 referral；单次操作最多 8 秒、并发最多 8 个连接。

## 关联账户与部署

停止服务并按现有运维流程备份数据库后，安装新后端。用配置对应的服务账户检查加密连接、查询绑定和 baseDN 可见性：

```sh
SLURM_CONSOLE_LDAP_CONFIG=/etc/slurm-console/ldap.json \
  /opt/slurm-console/slurm-console users check-ldap
```

管理员不需要提供用户密码。`create-ldap` 与 `link-ldap` 会实时查目录并保存其唯一标识，拒绝查询不到或有多个匹配的用户。

```sh
umask 077
SLURM_CONSOLE_LDAP_CONFIG=/etc/slurm-console/ldap.json \
  /opt/slurm-console/slurm-console users create-ldap \
  --username alice --email alice@example.com --name Alice \
  --dir=/var/lib/slurm-console/pb_data
```

已有账户使用 `users link-ldap --email alice@example.com --username alice`，并携带同一 `--dir` 和环境变量。关联保留平台 ID、启用状态、名称及 RBAC，替换平台密码哈希并轮换 tokenKey，使旧令牌失效。邮箱是平台标识，不因目录邮件属性自动变更。系统用户名在平台中全局唯一；目录唯一标识也不能关联多个 LDAP 平台账户。

目录用户删除后同名重建会产生新 `entryUUID`，已有映射不能登录或继续使用令牌。确需把新的目录身份关联至旧平台账户时，由管理员审核后显式执行 `link-ldap`；不会自动继承旧用户权限。

在 `/etc/slurm-console/slurm-console.env` 中设置 `SLURM_CONSOLE_LDAP_CONFIG=/etc/slurm-console/ldap.json`，再执行 `systemctl restart slurm-console`。新账户没有角色，仍通过 `access grant` 单独管理。PAM 与本地恢复账户继续可用；目录故障不会切换认证来源。

## 管理员提供的目录服务

目录服务器、TLS 证书、只读查询账户、测试用户和目录运维由系统管理员负责。仓库只提供应用客户端配置和协议测试，不安装 LDAP/FreeIPA/AD，不生成目录服务单元或演示目录。对接前使用 `users check-ldap` 检查 TLS、绑定和用户可见性，再按实际目录规则验证锁定/禁用、同名重建及故障恢复行为。不要启用会记录密码的客户端调试日志。

参考：[OpenLDAP TLS 安全机制](https://www.openldap.org/doc/admin26/security.html)、[go-ldap API](https://pkg.go.dev/github.com/go-ldap/ldap/v3)。
