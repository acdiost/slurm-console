import { z } from "zod"
import { ClientResponseError } from "pocketbase"
import { numberSchema } from "@/features/jobs/schema"
export const states = [
  "PENDING",
  "RUNNING",
  "SUSPENDED",
  "COMPLETED",
  "CANCELLED",
  "FAILED",
  "TIMEOUT",
  "NODE_FAIL",
  "PREEMPTED",
  "BOOT_FAIL",
  "DEADLINE",
  "OUT_OF_MEMORY",
  "REQUEUED",
  "REVOKED",
]
export const tresSchema = z.object({
  id: z.string(),
  type: z.string(),
  name: z.string(),
  count: z.string().nullable(),
})
export const exitSchema = z.object({
  status: z.array(z.string()),
  code: numberSchema,
  signal: numberSchema,
  signalName: z.string(),
})
const stepSchema = z.object({
  id: z.string(),
  name: z.string(),
  states: z.array(z.string()),
  nodes: z.string(),
  tasks: numberSchema,
  startTime: numberSchema,
  endTime: numberSchema,
  elapsedSeconds: numberSchema,
  totalCpuSeconds: z.number().nullable(),
  exit: exitSchema,
  allocatedTres: z.array(tresSchema),
  consumedAverage: z.array(tresSchema),
  consumedTotal: z.array(tresSchema),
})
export const historyJobSchema = z.object({
  key: z.string(),
  id: z.string(),
  cluster: z.string(),
  name: z.string(),
  user: z.string(),
  account: z.string(),
  partition: z.string(),
  qos: z.string(),
  states: z.array(z.string()),
  reason: z.string(),
  nodes: z.string(),
  submitTime: numberSchema,
  startTime: numberSchema,
  endTime: numberSchema,
  elapsedSeconds: numberSchema,
  suspendedSeconds: numberSchema,
  timeLimitMinutes: numberSchema,
  totalCpuSeconds: z.number().nullable(),
  arrayJobId: numberSchema,
  arrayTaskId: numberSchema,
  hetJobId: numberSchema,
  hetJobOffset: numberSchema,
  exit: exitSchema,
  derivedExit: exitSchema,
  allocatedTres: z.array(tresSchema),
  requestedTres: z.array(tresSchema),
  steps: z.array(stepSchema),
})
export const historyPageSchema = z.object({
  items: z.array(historyJobSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  cluster: z.string(),
  start: z.number(),
  end: z.number(),
  cached: z.boolean(),
  fetchedAt: z.string(),
})
export const historyDetailSchema = z.object({
  item: historyJobSchema,
  start: z.number(),
  end: z.number(),
  cached: z.boolean(),
  fetchedAt: z.string(),
})
export type HistoryJob = z.infer<typeof historyJobSchema>
export type HistoryTRES = z.infer<typeof tresSchema>
export function errorMessage(e: unknown) {
  if (e instanceof ClientResponseError) {
    if (e.status === 403) return "你没有该集群的历史作业读取权限。"
    if (e.status === 404)
      return "记录不在当前提交时间范围内，或已不对连接身份可见。"
    return (
      e.response?.error?.message ||
      "记账来源暂不可用，请核对 slurmdbd 与 REST 接口。"
    )
  }
  return "历史查询未完成，请检查网络后重试。"
}
export function cpu(value: number | null) {
  return value === null
    ? "未知"
    : `${value.toLocaleString("zh-CN", { maximumFractionDigits: 6 })} CPU 秒`
}
export function resourceText(items: HistoryTRES[], usage = false) {
  return items.length
    ? items
        .map(
          (v) =>
            `${v.type}${v.name ? `/${v.name}` : ""}=${v.count === null ? "未知" : v.count}${!usage && v.type === "mem" ? " MiB" : ""}`
        )
        .join(" · ")
    : "未记录"
}
export function localDate(seconds: number) {
  const d = new Date(seconds * 1000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}T${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`
}
