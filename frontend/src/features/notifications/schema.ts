import { z } from "zod"
import type { PlatformUser } from "@/stores/auth"
import { ClientResponseError } from "pocketbase"
export const kinds = {
  cluster_health: "集群健康异常",
  node_unavailable: "节点不可用",
  job_pending: "排队作业提交超时",
  gpu_temperature: "GPU 温度过高",
}
export type Kind = keyof typeof kinds
export const permissions: Record<Kind, string> = {
  cluster_health: "clusters.read",
  node_unavailable: "nodes.read",
  job_pending: "jobs.read",
  gpu_temperature: "metrics.read",
}
export const states: Record<string, string> = {
  disabled: "已停用",
  unknown: "无法判断",
  ok: "正常",
  pending: "等待持续时间",
  firing: "告警中",
  blocked: "权限已撤销",
}
export const kindSchema = z.enum([
  "cluster_health",
  "node_unavailable",
  "job_pending",
  "gpu_temperature",
])
export const ruleSchema = z.object({
  id: z.string(),
  clusterId: z.string(),
  name: z.string(),
  kind: kindSchema,
  severity: z.enum(["warning", "critical"]),
  enabled: z.boolean(),
  threshold: z.number().int(),
  holdMinutes: z.number().int(),
  revision: z.number().int().positive(),
  state: z.string(),
  incidentActive: z.boolean(),
  pendingSince: z.string(),
  lastChecked: z.string(),
  summary: z.string(),
  affectedCount: z.number().int(),
  samples: z.array(z.string()).max(5),
  usable: z.boolean(),
  stale: z.boolean(),
})
export const ruleListSchema = z.object({
  items: z.array(ruleSchema).max(20),
  maxPerUser: z.number(),
  maxRules: z.number(),
  intervalSeconds: z.number(),
})
export type Rule = z.infer<typeof ruleSchema>
export const notificationSchema = z.object({
  id: z.string(),
  clusterId: z.string(),
  ruleId: z.string(),
  ruleName: z.string(),
  kind: kindSchema,
  severity: z.enum(["warning", "critical"]),
  phase: z.enum(["firing", "recovered"]),
  incidentId: z.string(),
  message: z.string(),
  affectedCount: z.number().int(),
  samples: z.array(z.string()).max(5),
  read: z.boolean(),
  createdAt: z.string(),
  href: z.string().regex(/^\/(?:overview|nodes|jobs|gpu)\?/),
})
export const notificationPageSchema = z.object({
  items: z.array(notificationSchema).max(100),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  unreadCount: z.number(),
})
export function allows(user: PlatformUser | null, kind: Kind, cluster: string) {
  const p = permissions[kind]
  return (
    !!user &&
    (user.access.platformPermissions.includes(p) ||
      user.access.clusters.some(
        (c) => c.cluster.id === cluster && c.permissions.includes(p)
      ))
  )
}
export function errorMessage(e: unknown) {
  if (e instanceof ClientResponseError) {
    if (e.status === 409)
      return "规则版本已变化、达到数量限制或检查繁忙，请刷新后重试。"
    if (e.status === 403) return "当前没有该集群对应来源的读取权限。"
    if (e.status === 400) return "请检查名称、阈值、持续时间及集群启用状态。"
  }
  return "告警通知暂不可用，请稍后刷新。"
}
export function dateTime(value: string) {
  const n = Date.parse(value)
  return Number.isFinite(n)
    ? new Date(n).toLocaleString("zh-CN", { hour12: false })
    : "尚未检查"
}
export function thresholdLabel(kind: Kind) {
  return kind === "node_unavailable"
    ? "触发节点数"
    : kind === "job_pending"
      ? "提交后分钟数"
      : kind === "gpu_temperature"
        ? "温度阈值（°C）"
        : "异常次数"
}
