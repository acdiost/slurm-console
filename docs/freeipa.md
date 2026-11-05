# FreeIPA 目录接入

`/directory` 提供 FreeIPA 用户、组及组角色映射。FreeIPA 身份、平台账户和 Slurm 身份分别管理；不按邮箱合并、不自动创建平台账户、不自动授予角色，不创建 Slurm Association。现有平台密码、PAM 与通用 LDAP 保持各自的认证来源。

## 服务端配置

将 `deploy/freeipa.example.json` 安装到 `/etc/slurm-console/freeipa.json`，设置 `SLURM_CONSOLE_FREEIPA_CONFIG=/etc/slurm-console/freeipa.json` 后重启应用。URL 必须是 HTTPS origin，验证系统或显式 CA 信任及主机名，不跟随重定向；配置与 CA 不允许组/其他用户写入，服务密码文件必须为普通文件、绝对路径及 0600 或更严。密码与会话 Cookie 不进入平台数据库、审计、浏览器、URL 或仓库。配置无效阻止启动；目录故障不阻止本地恢复账户登录。

服务身份应为专用 FreeIPA 账户。只读连接需要用户、组与身份属性的读取能力；启用写操作时需要对应 FreeIPA 委派权限。`User Administrators` 与 `Group Administrators` 是涵盖用户及组生命周期的较宽权限，其中包括本应用没有暴露的操作，必须由管理员明确批准。可由目录管理员按站点策略建立更细权限的委派角色。服务密码到期或账户停用时，目录功能明确失败，不能回退到 admin 凭据。

```sh
SLURM_CONSOLE_FREEIPA_CONFIG=/etc/slurm-console/freeipa.json \
  /opt/slurm-console/slurm-console users check-freeipa
SLURM_CONSOLE_FREEIPA_CONFIG=/etc/slurm-console/freeipa.json \
  /opt/slurm-console/slurm-console users create-freeipa \
  --dir=/var/lib/slurm-console/pb_data --email=alice@acdiost.internal \
  --username=alice --name=Alice
```

关联已有平台账户使用 `users link-freeipa`，保留平台 ID、活动状态及手工 RBAC，轮换 tokenKey；目录用户必须存在、启用且未过期。也可以在平台设置创建 FreeIPA 来源的关联账户，不提供目录密码。`freeipa_entry_id` 保存不可变 `ipaUniqueID` 并隐藏，不向浏览器返回。同名用户重建不继承旧平台身份。

## 登录与资格复查

登录使用 FreeIPA HTTPS `login_password`，由 FreeIPA 核验密码、过期及认证策略；不回退到平台密码或其他目录。关联时、登录前后、刷新令牌、业务请求和后台任务核对用户唯一标识、`nsAccountLock`、Kerberos 账户及密码到期状态。服务端使用有时限的会话 Cookie；不缓存用户资格或授权结果。仅支持密码式认证，用户须先在 FreeIPA 完成初始密码变更；没有实现浏览器 Kerberos SSO、OTP 专用交互或密码重置。

## 用户与组管理

独立权限 `platform.directory.read/manage` 默认仅赋予 Super Admin，自定义角色需要显式授权。读取及返回前复查权限；列表按用户名/组名排序，支持服务器分页、搜索，来源最多查询 1000 条，FreeIPA 返回 truncated 时明确失败，不把截断当作完整计数。

当前操作：创建用户（姓、名及可选邮箱，不设置密码）、启用/停用用户、创建 POSIX 组（由 FreeIPA 分配 GID）、添加/移除直接用户成员。管理员、连接服务身份、FreeIPA 内置管理组受保护。涉及已映射到平台角色的组成员变更还需要 `platform.roles.manage`。不提供任意 JSON-RPC 代理、用户/组删除、密码管理、UID/GID 修改、嵌套组编辑或 FreeIPA 原生角色授予。

每次写入要求完整目标名称、原因、原条目版本及唯一操作编号。先持久记录执行意图，成功后才发送固定 RPC；每进程串行发送，结果未知时保留原编号并阻止同一目标的新操作，不自动重试。`我最近的目录操作` 展示本人最近十条记录，重启后仍可查看。目录侧拒绝与网络结果未知分开呈现；结果记录失败时保留未完成意图。未知结果必须由目录管理员核对，不提供未经核实的清除入口。版本核对是发送前检查，FreeIPA JSON-RPC 不提供跨系统原子事务，不能阻止目录管理员恰在核对后并发修改。

## 组角色映射

管理映射须同时具有目录读取与 `platform.roles.manage`，输入 `CHANGE ACCESS`。映射固定组的 `ipaUniqueID`、角色与可选集群范围，最多 100 条；集群角色必须绑定明确集群，平台角色不能绑定集群。原生映射 Collection 的 CRUD 完全锁定，变更与平台审计在同一事务提交。

只有显式关联且活动的 FreeIPA 平台账户参与映射。每次 RBAC 解析实时读取直接所属组并核对用户与组的不可变身份；不包含嵌套成员，不复制为手工授权。退组、停用、删除/重建或移除映射后，后续请求不再获得该映射；目录故障时拒绝依赖目录的授权。移除映射保留管理员手工授予的角色，因此用户可能仍有手工权限。应保留至少一个直接授权的本地恢复管理员。

## API

全部在 `/api/v1/directory` 下，沿用 `{data,requestId}`。提供配置读取、`GET /users`、`GET /groups`、`POST /changes`、`GET /operations`、`GET /operations/{operationId}`、`GET/POST /role-mappings`、`DELETE /role-mappings/{mappingId}`；完整字段见 OpenAPI。目录条目的不可变标识、服务凭据和原始 RPC 响应不外发；公开 `version` 是用于发送前核对的摘要。

## 2026-11-04 部署记录

按管理员明确授权，在 `192.168.100.134` 安装 Rocky 仓库 FreeIPA 4.13.4，主机名 `ipa.acdiost.internal`，域 `acdiost.internal`，Realm `ACDIOST.INTERNAL`，包含集成 CA、DNS 和反向区，上游 DNS 为 `192.168.100.2`。原有占用 389/636 的 OpenLDAP 已停止并禁用；原数据、配置及主机配置位于 root 私有目录 `/root/freeipa-migration-20261104-2G5fiL`，控制台一致性快照已加密并验证。原独立测试 LDAP 继续运行，现有目录用户没有自动迁移至 FreeIPA。

原虚拟机约 1.7 GiB RAM；按管理员选择保留内存并将交换空间补足至 4 GiB。安装已成功，`ipactl status` 的目录、Kerberos、DNS、Web、CA 等服务运行正常；本机 DNS 正反向/SRV 查询、管理员 Kerberos 登录和使用 CA 校验的 HTTPS 200 已通过。经管理员明确批准，已持久允许 `192.168.100.0/24` 访问 FreeIPA 与 DNS，服务器自身 DNS 设为 `127.0.0.1`；从开发电脑验证带 CA 校验的 HTTPS 200 与 DNS A/SRV 查询通过。客户端仍需配置该 DNS（或相应的域解析），并信任 FreeIPA CA。

协议依据：[FreeIPA JSON-RPC](https://freeipa.readthedocs.io/en/ipa-4-9/api/jsonrpc_usage.html)、[用户生命周期](https://www.freeipa.org/page/V4/User_Life-Cycle_Management.html)、[组管理 API](https://freeipa.readthedocs.io/en/latest/api/group_management.html)。

## 开发验证

2026-11-04：前端 215 项测试、类型检查、Lint、生产构建通过；后端完整 `go test -race ./...` 与 `go vet ./...` 通过，随后补充的未知禁用状态保护及 FreeIPA 专项竞态回归也通过。已生成 Linux ARM64 构建，OpenAPI 111 个路径解析及内部引用校验通过；隔离示例数据验证桌面、手机及深色页面，手机端无页面级横向溢出。使用获批的 `slurm-console-ipa` 服务账户在服务器本机独立实例完成 20 项真实联调：目录用户/组创建、操作重放、平台显式关联、原生密码登录、成员增删与集群角色即时生效/撤销、禁用及密码过期阻断旧令牌、同名用户/组重建不继承原身份或授权，以及审计不泄露不可变目录标识。临时目录用户和组已按唯一标识核对后清理；私有测试实例已停止，证据报告保存在服务器 `/root/freeipa-integration-qa-report.json`。

## 正式控制台启用

已通过现有安装器升级正式控制台，在应用环境中启用 `SLURM_CONSOLE_FREEIPA_CONFIG=/etc/slurm-console/freeipa.json`。升级前加密一致性快照：`/var/backups/slurm-console/upgrade-20261104T155249-126525.tar.gz.age`。正式应用健康检查通过，独立 FreeIPA 探测通过，未登录访问目录接口为 401；Super Admin 的目录读取/管理权限迁移已验证。Slurm、PAM 专属文件与独立测试 LDAP 配置摘要保持一致，原有 Slurm 及控制台服务正常。

服务账户绑定 `Slurm Console Directory Manager` 角色，具有管理员明确批准的 `User Administrators` 与 `Group Administrators` 权限。配置及随机密码文件均为 root:root 0600；服务密码设为 90 天有效，需要管理员在到期前轮换并更新受保护文件。没有把 FreeIPA admin 账户自动关联为平台管理员，也没有建立任何正式目录组角色映射；管理员在界面中显式配置所需映射。

本次正式 ARM64 程序 SHA-256：`95f5e50b1ef3b18cc94e9767dea57c1ba42d0d9e231b2b3e04c743784679f956`。刷新控制台后，具备 `platform.directory.read` 的用户可通过“身份与策略 → FreeIPA 目录”进入。
