import { z } from "zod"

export const profileSchema = z.object({
  ref: z.string(),
  label: z.string(),
  endpoint: z.string(),
  authType: z.enum(["local", "jwt"]),
  apiVersions: z.array(z.string()),
})
export type ConnectionProfile = z.infer<typeof profileSchema>
export const configurationSchema = z.object({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
  description: z.string(),
  connectionRef: z.string(),
  apiVersion: z.string(),
  enabled: z.boolean(),
  timeoutSeconds: z.number(),
  revision: z.number(),
  endpoint: z.string(),
  authType: z.string(),
  profileAvailable: z.boolean(),
  lastProbe: z.object({
    status: z.string(),
    message: z.string(),
    checkedAt: z.string(),
    durationMs: z.number(),
    serverVersion: z.string(),
    controllers: z.array(
      z.object({ hostname: z.string(), state: z.string(), role: z.string() })
    ),
  }),
})
export type ClusterConfiguration = z.infer<typeof configurationSchema>
export const configurationPageSchema = z.object({
  items: z.array(configurationSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
})
export const connectionFormSchema = z.object({
  name: z
    .string()
    .trim()
    .min(2, "名称至少 2 个字符")
    .max(100, "名称最多 100 个字符"),
  slug: z
    .string()
    .min(1, "请输入集群标识")
    .max(64)
    .regex(/^[a-z0-9][a-z0-9-]*$/, "使用小写字母、数字或连字符"),
  description: z.string().trim().max(1000, "说明最多 1000 个字符"),
  connectionRef: z.string().min(1, "请选择连接方式"),
  apiVersion: z.string().min(1, "请选择 API 版本"),
  enabled: z.boolean(),
  timeoutSeconds: z.number().int().min(1, "至少 1 秒").max(30, "最多 30 秒"),
  revision: z.number().int().min(0),
})
export type ConnectionValues = z.infer<typeof connectionFormSchema>

export function connectionStatus(config: ClusterConfiguration) {
  if (!config.connectionRef) return { label: "未配置", tone: "neutral" }
  if (!config.enabled) return { label: "已停用", tone: "neutral" }
  if (!config.profileAvailable)
    return { label: "连接方式不可用", tone: "danger" }
  if (config.lastProbe.status === "connected")
    return { label: "连接正常", tone: "success" }
  if (config.lastProbe.status === "not_tested")
    return { label: "待检测", tone: "warning" }
  return { label: "检测失败", tone: "danger" }
}
