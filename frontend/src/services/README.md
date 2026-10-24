# 请求层边界

`pocketbase.ts` 使用官方 SDK 和标签页级 sessionStorage；密码不持久化，存储不可用时仅在内存中维护会话。`api.ts` 访问 `/api/v1` 并在 401 时清除登录状态，业务缓存使用 `lib/query-client.ts` 的 QueryClient，退出时清空。

原生密码登录和令牌刷新复用 PocketBase `/api/collections/users` 端点，保留框架认证、密码校验、限流及原生错误格式。应用接口和权限校验使用 `/api/v1`。Slurm 示例仅在 `features/design-system` 内使用，不充当后端响应。
