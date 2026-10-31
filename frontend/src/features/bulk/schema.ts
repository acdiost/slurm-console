import { z } from "zod"
import { ClientResponseError } from "pocketbase"
import { jobSchema } from "@/features/jobs/schema"
import { nodeSchema } from "@/features/nodes/schema"
export type BulkAction = "jobs.cancel" | "nodes.drain" | "nodes.resume"
export const itemSchema = z.object({
  action: z.string(),
  target: z.string(),
  operationId: z.string(),
  allowed: z.boolean(),
  status: z.enum([
    "ready",
    "processing",
    "confirmed",
    "accepted",
    "failed",
    "unknown",
  ]),
  code: z.string(),
  message: z.string(),
  states: z.array(z.string()),
  job: jobSchema.nullable(),
  node: nodeSchema.nullable(),
})
export const viewSchema = z.object({
  id: z.string(),
  action: z.string(),
  status: z.enum(["ready", "blocked", "processing", "completed"]),
  allowed: z.boolean(),
  expiresAt: z.string(),
  items: z.array(itemSchema),
  counts: z.record(z.string(), z.number()),
})
export type BulkView = z.infer<typeof viewSchema>
export const label = (action: string) =>
  ({
    "jobs.cancel": "取消作业",
    "nodes.drain": "Drain 排空",
    "nodes.resume": "Resume 恢复",
    "nodes.control": "节点控制",
  })[action] || "资源控制"
export const statusLabel = (status: string) =>
  ({
    ready: "等待确认",
    processing: "正在处理",
    confirmed: "已核实",
    accepted: "已受理，待核实",
    failed: "未完成",
    unknown: "结果待核实",
  })[status] || "未知"
export const phrase = (action: BulkAction, n: number) =>
  `${action.split(".")[1].toUpperCase()} ${n}`
export const validReason = (reason: string) =>
  Array.from(reason.trim()).length >= 2 &&
  Array.from(reason.trim()).length <= 300 &&
  !["\r", "\n", String.fromCharCode(0)].some((c) => reason.includes(c))
export function errorMessage(e: unknown) {
  return e instanceof ClientResponseError
    ? e.response?.error?.message || "批量请求未完成，请核对权限与操作记录。"
    : "未取得可靠响应，请读取操作记录，勿重复提交。"
}
export function validOperations(ids: string[]) {
  return (
    ids.length > 0 &&
    ids.length <= 10 &&
    new Set(ids).size === ids.length &&
    ids.every((id) => /^[A-Za-z0-9_-]{20,64}$/.test(id))
  )
}
