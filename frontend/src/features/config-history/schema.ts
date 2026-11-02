import { z } from "zod"
import { ClientResponseError } from "pocketbase"
import { connectionFormSchema } from "@/features/clusters/schema"
export const labels = {
  name: "集群名称",
  slug: "集群标识",
  description: "说明",
  connectionRef: "连接方式引用",
  apiVersion: "REST API 版本",
  enabled: "平台连接状态",
  timeoutSeconds: "超时（秒）",
}
export const snapshotSchema = z.object({
  name: z.string(),
  slug: z.string(),
  description: z.string(),
  connectionRef: z.string(),
  apiVersion: z.string(),
  enabled: z.boolean(),
  timeoutSeconds: z.number().int().min(0).max(30),
})
const fieldSchema = z.object({
  field: z.enum([
    "name",
    "slug",
    "description",
    "connectionRef",
    "apiVersion",
    "enabled",
    "timeoutSeconds",
  ]),
  before: z.string().nullable(),
  after: z.string().nullable(),
  changed: z.boolean(),
})
export const fieldsSchema = z.array(fieldSchema).length(7)
export type DiffField = z.infer<typeof fieldSchema>
export const previewSchema = z.object({
  clusterId: z.string(),
  currentRevision: z.number().int().min(0).nullable(),
  nextRevision: z.number().int().positive(),
  before: snapshotSchema.nullable(),
  after: snapshotSchema,
  fields: fieldsSchema,
  configuration: connectionFormSchema.nullable(),
})
export type Preview = z.infer<typeof previewSchema>
export const versionSchema = z.object({
  id: z.string(),
  clusterId: z.string(),
  revision: z.number().int().min(0),
  schemaVersion: z.literal(1),
  kind: z.enum(["baseline", "change"]),
  action: z.string(),
  actorId: z.string(),
  actorLabel: z.string(),
  actorSource: z.string(),
  operationId: z.string(),
  requestId: z.string(),
  recordedAt: z.string(),
})
export type Version = z.infer<typeof versionSchema>
export const versionsSchema = z.object({
  items: z.array(versionSchema).max(100),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  clusterId: z.string(),
  currentStatus: z.enum(["matched", "different", "missing", "no_history"]),
  currentRevision: z.number().int().min(0).nullable(),
  fetchedAt: z.string(),
})
export const comparisonSchema = z.object({
  clusterId: z.string(),
  from: versionSchema,
  to: versionSchema,
  before: snapshotSchema,
  after: snapshotSchema,
  fields: fieldsSchema,
  fetchedAt: z.string(),
})
export function canHistory(permissions: string[]) {
  return ["platform.clusters.manage", "platform.audit.read"].every((p) =>
    permissions.includes(p)
  )
}
export const validId = (id: string) => /^[a-z0-9]{15}$/.test(id)
export function display(field: string, value: string | null) {
  if (value === null) return "不存在"
  if (value === "") return "空值"
  if (field === "enabled") return value === "true" ? "启用" : "停用"
  if (field === "timeoutSeconds" && value === "0") return "0（未配置）"
  return value
}
export function errorMessage(e: unknown) {
  if (e instanceof ClientResponseError) {
    if (e.status === 403) return "需要平台配置管理和审计读取权限。"
    if (e.status === 404) return "此集群的所选版本不存在。"
    return e.response?.error?.message || "配置版本请求未完成，请刷新后重试。"
  }
  return "配置版本请求未完成，请检查服务后重试。"
}
