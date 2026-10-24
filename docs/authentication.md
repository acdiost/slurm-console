# 平台认证与首次授权

使用 PocketBase v0.40.6 管理平台账户、令牌及 RBAC；支持平台密码、Linux PAM、LDAP 三种认证来源。外部账户必须由管理员显式关联，平台 ID 与集群权限独立保存；系统/目录密码不进入 PocketBase。安装及关联命令见 [PAM 接入](pam.md)、[LDAP 接入](ldap.md)。FreeIPA/AD 专属生命周期、目录组同步及 Slurm 操作身份映射尚未接入。

## 首次启动

需要 Go 1.27.3+，或允许 Go 按 `go.mod` 自动下载工具链。前端要求 Node.js 22.16+（22.x）或 24 LTS。

```sh
make install
```

在 `backend/` 中先创建账户，终端会隐藏密码输入，密码长度为 12–71 个字符（同时受 PocketBase 的密码字节长度约束）：

```sh
go run . users create --email operator@example.com --name Operator
```

自动化场景也支持 stdin。不要把真实密码直接写在命令参数、脚本或 Git 文件中。程序不会打印密码，也不会自动生成默认账户或默认密码。

新账户没有任何角色。需要平台级管理员时，由管理主机上的可信操作员显式授予：

```sh
go run . access grant --email operator@example.com --role super_admin
```

所有命令默认使用当前目录的 `pb_data/`。也可以为所有命令指定同一个 `--dir=/absolute/path/pb_data`。初始化或数据库迁移前停止服务，完成后重启，避免集合缓存和 schema 不一致。

在项目根目录分别开启两个终端：

```sh
make dev-backend
```

```sh
make dev
```

打开 Vite 输出的地址。匿名访问 `/design-system` 或 `/access` 会进入 `/login`。Vite 将 `/api` 代理至 `127.0.0.1:8090`。

## 集群范围授权

当前集群集合只保存 `id/name/slug/description`，用于权限隔离，不包含 endpoint、JWT 或其他凭据，也不检查 Slurm 连接。

在 `backend/` 中登记集群标识后，可分配集群角色：

```sh
go run . access register-cluster --slug research-a --name Research-A
go run . access grant --email operator@example.com --role viewer --cluster research-a
go run . access revoke --email operator@example.com --role viewer --cluster research-a
```

`--cluster` 接受标识 slug 或记录 ID。平台角色不允许指定集群；集群角色必须指定集群。重复 grant 不会产生重复绑定。撤销或修改绑定后，服务端下一次请求就读取新权限；“我的权限”可点击刷新查看。

这些命令是可信管理主机上的管理工具，不是普通用户 API。它们需要数据库文件访问权限，授权变更会记录为结构化应用日志。完整、独立留存的操作审计仍属于后续审计事项。

## 认证流程和会话

- 登录和刷新使用 PocketBase 原生 `/api/collections/users/auth-with-password` 和 `/auth-refresh`，保留框架的认证规则与限流。本地账户使用 PocketBase 哈希校验；PAM/LDAP 账户分别使用 helper/加密目录绑定，绝不回退到平台密码或互相尝试密码。
- `users.active` 必须为 true；公开注册、普通用户 CRUD、自行修改启用状态和权限均关闭。管理员可在 PocketBase 控制台维护账户（控制台 `_superusers` 身份不作为平台用户接受）。
- 登录或恢复会话后调用 `/api/v1/auth/me`，校验活动平台用户并读取实际角色和动作权限。
- PAM 登录同时调用 `pam_authenticate` 和 `pam_acct_mgmt`。刷新令牌及业务 API 请求再次检查 PAM account 策略；账户到期、密码过期要求变更等拒绝结果会阻止访问。PAM 故障返回 503，前端保留可重试状态。
- LDAP 登录要求唯一匹配、不可变目录标识一致，并通过用户 DN 密码绑定；刷新及业务请求复查资格过滤器与目录标识。目录同名重建不能继承旧平台授权；锁定/禁用判断按管理员配置的目录过滤器执行。目录不可用返回 503。
- 平台 JWT 有效期为一小时，当前标签页的 sessionStorage 保存 SDK 会话，页面刷新必须到服务器重新校验；存储不可用时只使用内存。密码不保存，退出时清空令牌和业务请求缓存。前端识别到过期或 API 401 时返回登录。
- 认证服务不可用时不会进入受保护工作空间，保留可重试的会话恢复状态。
- PocketBase 使用无服务端会话的令牌认证：退出只清除本标签页的会话，不使已复制的令牌立即失效。需要立即阻断账户时，将 `active` 设为 false；已签发令牌也不能通过当前用户和业务授权检查。
- 登录按来源 IP 限制为每分钟 5 次，API 默认每 10 秒 300 次。服务端不信任浏览器传入的角色、权限或自定义 JWT 声明。

官方参考：[PocketBase 认证](https://pocketbase.io/docs/authentication/)、[Go 路由](https://pocketbase.io/docs/go-routing/)、[Go 迁移](https://pocketbase.io/docs/go-migrations/)。

## 数据迁移

Go 迁移随程序编译，启动时应用。关闭控制台自动生成迁移；集合变更需要在 Git 中评审。初始迁移关闭 PocketBase 默认公开注册，创建锁定的权限集合并初始化角色和动作。

认证、RBAC、PAM、LDAP 迁移不支持自动 down：自动删除认证字段、身份关联或授权记录可能重新启用禁用账户或丢失授权证据，因此命令会明确返回错误。需要回退时，在停服状态下使用升级前的一致性数据库备份，并恢复对应版本的程序。完整备份部署流程在后续事项实施。
