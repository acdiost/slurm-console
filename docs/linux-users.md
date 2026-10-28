# Linux 系统用户查询

`/linux-users` 展示指定集群身份主机的 NSS 用户列表、UID/GID、描述、主目录、Shell 及身份来源匹配信息，支持服务端筛选、分页、排序和精确用户名详情。Linux 身份、PocketBase 平台账户及 Slurm User/Association 分别建模；查询不会创建平台账户或 Slurm 关联。

查询支持本机和固定 SSH 来源；本机来源现可按明确启用的主机策略创建账户及锁定/解锁，见文末。密码设置、删除及目录生命周期管理未开放。

## 身份来源与可见范围

来源主机执行固定的 `getent passwd` 枚举，精确详情执行 `getent passwd <完整用户名>`。实际来源由该主机现有 NSS 配置决定，可以包含本地、LDAP、SSSD 等已接入身份。目录服务可能未开启枚举，因此列表和 `totalItems` 只表示本次可枚举及筛选后的记录，不能当作全部目录账户数量。精确查询可以读取未枚举但可按名称解析的目录用户。

- `source=local`：NSS 元数据与该主机 `/etc/passwd` 中的同名记录一致，页面标注“本地匹配”；这不证明 NSS 实际选择了哪个模块。
- `source=nss`：未匹配本地记录，标注“NSS 非本地匹配”，不猜测具体 LDAP/FreeIPA/AD 目录类型。
- `source=unknown`：无法可靠读取/解析本地记录，保留“来源未确认”。

`kind=privileged` 表示 UID 0；其他记录按来源主机 `/etc/login.defs` 的 `UID_MIN` 分为 `system/regular`。无法确认 UID_MIN 时为 `unknown`，不硬编码 1000。该分类是数值范围，不表示账户可登录或有管理员权限。页面不推断密码锁定、过期或 SSH/PAM 登录资格；只读查询子命令不读取 `/etc/shadow`，不返回 passwd 中的密码字段，也不读取用户主目录内容。

仅查询管理员配置的一个身份主机，不逐计算节点检查 UID/GID 一致性，也不宣称该主机能代表所有节点。来源标签应明确主机用途；多集群配置必须分别指向正确来源，不能默认查询平台所在主机来代表远端集群。

## 配置与部署

查询器复用应用二进制的独立子命令 `/opt/slurm-console/slurm-console linux-users-reader`，在数据库及认证初始化前分流。目标需 Linux 与 `/usr/bin/getent`，无需 cgo、root、sudo、新增二进制或常驻服务。macOS 开发端通过已部署 Linux API 验收，不在 macOS 上模拟 Linux NSS。

应用程序由现有安装器以 root:root 0755 安装，与应用一同加密备份和升级回滚；reader 子命令以普通查询用户运行，只接受固定 JSON 请求，不修改任何系统账户。远端只需部署同版本应用二进制到固定路径，并不需要启动平台 Web 服务或数据库。

复制 `deploy/linux-users.example.json` 到 `/etc/slurm-console/linux-users.json`，将键替换为集群记录 ID（不是 slug）。文件及父目录由部署管理员管理，文件禁止组或其他用户写入，应用账户需可读。添加应用环境：

```ini
SLURM_CONSOLE_LINUX_USERS_PROFILES=/etc/slurm-console/linux-users.json
```

本机来源仅在应用实际部署在该集群的身份主机时使用：

```json
{"clusters":{"<集群记录ID>":{"label":"控制节点 NSS 身份","transport":"local"}}}
```

远端来源使用现有 SSH，管理员负责部署同版本应用二进制和专用普通查询账户，并准备独立 SSH 私钥及经可信渠道核对的 known_hosts：

```json
{
  "clusters": {
    "<集群记录ID>": {
      "label": "集群 B 登录节点 NSS",
      "transport": "ssh",
      "host": "192.0.2.10",
      "port": 22,
      "username": "inventory",
      "identityFile": "/etc/slurm-console/keys/cluster-b",
      "knownHostsFile": "/etc/slurm-console/keys/known_hosts"
    }
  }
}
```

私钥须为应用可读的普通文件，权限 0600 或更严；配置、known_hosts 禁止组/其他用户写入，文件本身拒绝符号链接。SSH 使用固定 reader 路径、无交互公钥认证及严格主机密钥校验，关闭 Agent、配置文件、代理命令、端口转发和本地命令；不接受 root SSH 身份、浏览器提交的主机/命令/路径或首次连接自动信任。服务端应为该查询密钥配置 `restrict,command="/opt/slurm-console/slurm-console linux-users-reader"` 的受限 authorized_keys 条目，部署和密钥分发由系统管理员完成。

配置在启动时加载并校验，变更后重启应用。缺少某集群来源时返回 `LINUX_USERS_NOT_CONFIGURED`，不回退到本机或其他集群；非法配置阻止应用启动。该配置在 `/etc/slurm-console` 内随应用备份；远端 SSH 服务、NSS/目录和用户数据不由应用安装或备份。

## API、权限与边界

- `GET /api/v1/clusters/{clusterId}/linux-users`：`page` 默认 1、范围 1–1000000；`perPage` 默认 20、范围 1–100；`q` 对用户名/描述/主目录做字面搜索（最多 128 个 Unicode 字符且 512 字节）；`source=local|nss|unknown`；`kind=privileged|system|regular|unknown`；`sort=name|uid|gid`，`order=asc|desc`，默认名称升序，数值相同按名称升序。
- `GET /api/v1/clusters/{clusterId}/linux-users/{username}`：精确读取，不接受查询参数。用户名支持 Unicode 字母、数字、`_ . @ + \\ $ -`，最大 255 字节；纯数字不作为用户名查询，避免与 getent 的 UID 查找语义混淆。

两个接口均需活动平台用户及集群范围 `linux_users.read`。新迁移将读取动作显式绑定到内置 Super Admin/Cluster Admin/Operator/Viewer；不授予任何 Linux 写能力。自定义角色需管理员自行绑定。授权在来源访问前校验，响应前再次校验，撤权后丢弃结果；配置版本或启停变化时返回 409 丢弃来源数据。原生 Collection 权限仍锁定，没有系统账户的持久化业务表或浏览器代理命令端点。

成功响应沿用 `{data,requestId}`。列表 `data={items,page,perPage,totalItems,fetchedAt,cached:false,sourceLabel,enumeration:"nss",uidMin,revision}`；详情为 `item` 和相同元数据，`enumeration="lookup"`。无来源、来源错误为 503；未知/不可见用户为 404；连接停用或请求期间配置变化为 409；繁忙为 429；取消为 408；授权失败为 401/403，非法/重复/未知参数为 400。不返回 SSH 主机、凭据路径、原始 stderr 或口令字段。

最多 4 个并发来源请求，超出立即返回繁忙；来源进程预算 10 秒，reader 内 NSS 查询预算 8 秒，响应最多 8 MiB 和 20000 记录。超限或非法结果明确失败，不截断为看似完整列表。无业务缓存或自动重试。页面默认 30 秒刷新，可暂停，后台标签页不轮询；失败后隐藏旧列表/详情。集群选择及跨业务导航沿用[多集群管理](multi-cluster.md)约定，切换时清空原用户详情。

枚举和登录语义依据：[getent(1)](https://man7.org/linux/man-pages/man1/getent.1.html)。查询结果不能替代系统管理员对认证和访问策略的核验。

## 已验证范围

2026-10-27 在现有 Rocky Linux 虚拟机部署本机只读来源，实际枚举 27 条记录。现有 PAM 账户登录后，列表、分页、筛选、精确详情和不存在用户的 404 均通过 HTTP 与浏览器验收；独立子命令以普通应用账户运行，设置无效 LDAP 路径仍可查询，确认不初始化平台认证或数据库。外部 Slurm、LDAP、PAM 配置哈希保持一致，未创建或修改任何系统用户。

自动验证包含后端 123 项测试、竞态检测、`go vet`，前端 85 项测试、类型检查、lint、构建和 OpenAPI 引用校验。非本地 NSS 及未枚举目录用户由 fixture 验证；SSH 的参数约束、受保护文件与主机密钥校验配置由单元测试验证，尚未在第二台真实 SSH 身份主机或目录 NSS 上联调。

## 本地账户创建、锁定与解锁

2026-10-28 增加本机来源的账户写操作。SSH 来源仍保持只读，LDAP/FreeIPA/AD 目录账户由目录管理员维护。`linux_users.create/lock/unlock` 为独立集群动作，显式绑定内置 Super Admin 与 Cluster Admin；Operator、Viewer 和自定义角色不会自动获得写能力。平台授权、`manageLocal` 来源开关和 root 主机策略须同时满足。

主机策略默认只允许 UID 10000–59999，须由管理员预留为本地身份段，避免与目录及现有账户分配重叠。名称/UID 唯一性根据当前 NSS 可见范围检查；不能据此保证不可见目录中不存在同名身份。root、应用进程身份、策略保护名单、GID 0、属于本机 root/wheel/sudo/admin 组的账户及非本地匹配账户均不可修改。普通服务进程始终以原应用账户运行；不会授予任意 useradd/usermod 或 root shell。

- 创建：明确指定小写 Unix 用户名、UID、描述和 `/sbin/nologin` 或 `/bin/bash`。固定创建同名主组与 `/home/用户名`，主目录权限 0700，使用空的专用 skel。账户初始设置不可用口令 `!` 与到期日 1；密码/SSH 凭据由系统管理员另行配置，平台不传输或保存口令。主目录、同名用户/组或 UID 已存在时拒绝，不覆盖或复用。
- 锁定：使用 `usermod --expiredate 1` 设置账户到期，而非仅给密码加锁。系统会先保存原到期日和账户身份摘要。后续依赖账户到期校验的 PAM/SSH 登录会受影响；已有会话、进程和 Slurm 作业不被终止。
- 解锁：仅针对由平台锁定、身份未变化且原到期策略仍有效的账户，恢复原到期日；原先为无到期日则恢复为 -1。不会清除管理员原设的过期策略、重设密码或解除其他认证限制，不能保证解锁后一定能登录。

平台先签发两分钟有效的不透明确认，绑定操作者、集群、配置版本、输入、主机状态与操作编号；用户须输入完整用户名及原因。执行前再验授权和配置，持久审计成功后才调用主机。受限的 root 写入子命令会读取 shadow 到期字段，并计算用于校验身份的摘要，但不输出口令字段。主机单独串行校验并保存执行意图及结果，同一操作编号绝不重发 OS 命令。超时、网络丢失或进程中断保留未知状态；通过原操作编号只读核实。`我最近的操作` 可找回本人最近五条记录，服务重启后也可读取。未核实操作阻断同一账户后续请求。

### 一次启用及幂等配置

应用升级仍使用原安装器，并生成加密升级前快照；完成升级后，在主机上显式启用指定集群：

```sh
sudo bash deploy/setup-linux-users.sh <集群记录ID>
```

脚本不创建系统账户。它建立 root:root 0600 的 `/etc/slurm-console/linux-users-admin.json`（已有策略保持原样），为该本机来源启用 `manageLocal`，安装只允许 `/usr/bin/nsenter --mount=/proc/1/ns/mnt -- /opt/slurm-console/slurm-console linux-users-admin` 固定参数的 sudo 规则。重复执行保持幂等；配置、探测或健康检查失败时回退配置。固定 nsenter 启动方式仅让受限子命令进入主机挂载空间，确保标准账户工具可以更新主机账户数据库；Web 服务本身继续保持原有只读 `/etc` 挂载。UID 范围和保护名单只能由系统管理员编辑该 root 策略，修改后每次主机调用立即重新读取。来源开关更改需重启应用。

主机执行日志及到期恢复状态位于 `/var/lib/slurm-console-linux-users`，root:root 0700。它与 Linux passwd/group/shadow 属于同一主机状态生命周期，须随主机备份保留；应用备份不恢复 Linux 系统账户，也不回滚这份主机执行日志。应用备份会包含应用配置、专属 sudo 规则及平台审计。主机日志丢失、身份变化或管理员另行修改到期策略时，平台拒绝猜测原状态，交由系统管理员核对。

接口为 `GET /clusters/{id}/linux-user-controls`、`POST /linux-user-controls/prepare`、`POST /linux-user-controls/execute` 和 `GET /linux-user-operations/{operationId}`。操作结果只包含用户名、动作、状态、时间及安全信息，不返回 shadow 口令字段、主机身份摘要或可重放的 root 请求。完整字段见 OpenAPI。

账户到期和密码锁定的区别依据：[usermod(8)](https://man7.org/linux/man-pages/man8/usermod.8.html)。

### 2026-10-28 实机验收

在现有 Rocky Linux 9 虚拟机启用默认 UID 范围，使用独立测试账户 `scqa_local_1028`（UID/GID 15050）验证创建、初始到期锁定、解锁、再次锁定及恢复。主目录实际为 0700；PAM account 校验在锁定时拒绝，在恢复无到期日后通过，整个过程未设置可用密码。相同请求重放返回同一操作记录；root 与 consoleadmin 的操作被策略拒绝，平台审计保留用户名、原因、请求 ID、结果和实际字段变化。

首次实机请求被服务的只读 `/etc` 阻断，系统保留未知意图且未重试。核对用户、组、UID、主目录和邮箱均未产生变更后，管理员将这条测试意图标记为失败并通过原操作查询同步审计；随后采用固定主机挂载空间启动方式完成验证。Web 服务自身仍保持只读 `/etc`。独立配置脚本重复运行不重启应用；Slurm、LDAP、PAM 配置哈希及 Slurm 服务进程保持一致。

测试用户、私有主目录及空主组已清理，主机日志与平台审计保留。当前后端 129 项测试及竞态检测、前端 89 项测试、类型检查、lint、构建及 OpenAPI 引用校验通过。SSH 来源仍只读，未新增目录或分布式账户写入能力。
