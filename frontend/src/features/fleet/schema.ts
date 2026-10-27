import { z } from "zod"
import { ClientResponseError } from "pocketbase"
import { clusterSchema, overviewSchema } from "@/features/overview/schema"
export const fleetItemSchema = z.object({
  cluster: clusterSchema,
  revision: z.number(),
  configuration: z.enum(["enabled", "disabled", "unconfigured"]),
  state: z.enum([
    "connected",
    "controller_down",
    "unavailable",
    "disabled",
    "unconfigured",
    "busy",
    "changed",
    "pending",
  ]),
  permissions: z.array(z.string()),
  health: overviewSchema.shape.health,
  nodes: overviewSchema.shape.nodes,
  jobs: overviewSchema.shape.jobs,
})
export type FleetItem = z.infer<typeof fleetItemSchema>
export const fleetPageSchema = z.object({
  items: z.array(fleetItemSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  generatedAt: z.string(),
  canManage: z.boolean(),
  pageSummary: z.object({
    connected: z.number(),
    controllerDown: z.number(),
    unavailable: z.number(),
    disabled: z.number(),
    unconfigured: z.number(),
    pending: z.number(),
    partial: z.number(),
    needsAttention: z.number(),
  }),
})
export const stateLabels: Record<FleetItem["state"], string> = {
  connected: "控制器连通",
  controller_down: "控制器未就绪",
  unavailable: "连接查询失败",
  disabled: "连接已停用",
  unconfigured: "尚未配置",
  busy: "查询繁忙",
  changed: "配置已变化",
  pending: "本次未取得快照",
}
export function errorMessage(error: unknown) {
  if (error instanceof ClientResponseError) {
    if (error.status === 403) return "你没有读取工作台的权限。"
    return error.response?.error?.message || "工作台查询未完成，请刷新后重试。"
  }
  return "暂时无法连接平台服务，请检查网络后重试。"
}
export function clusterLink(path: string, id: string) {
  return `${path}?${new URLSearchParams({ cluster: id })}`
}
