import { z } from "zod"
import { ClientResponseError } from "pocketbase"
export const numberSchema = z.object({
  value: z.number().nullable(),
  infinite: z.boolean(),
})
export const jobSchema = z.object({
  id: z.string(),
  name: z.string(),
  user: z.string(),
  userId: z.number().nullable(),
  account: z.string(),
  partition: z.string(),
  qos: z.string(),
  states: z.array(z.string()),
  reason: z.string(),
  nodes: z.string(),
  cpus: numberSchema,
  nodeCount: numberSchema,
  timeLimitMinutes: numberSchema,
  submitTime: numberSchema,
  startTime: numberSchema,
  endTime: numberSchema,
  arrayJobId: numberSchema,
  arrayTaskId: numberSchema,
  arrayTaskString: z.string(),
  hetJobId: numberSchema,
  hetJobOffset: numberSchema,
  hetJobIdSet: z.string(),
  restartCount: z.number().nullable(),
  priority: numberSchema,
  memoryPerNodeMiB: numberSchema,
  memoryPerCpuMiB: numberSchema,
})
export type Job = z.infer<typeof jobSchema>
export const jobsPageSchema = z.object({
  items: z.array(jobSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  fetchedAt: z.string(),
  cached: z.boolean(),
})
export const jobDetailSchema = z.object({
  item: jobSchema,
  fetchedAt: z.string(),
  cached: z.boolean(),
})
export const preparedSchema = z.object({
  allowed: z.boolean(),
  message: z.string(),
  job: jobSchema,
  identity: z
    .object({
      username: z.string(),
      uid: z.number(),
      scope: z.enum(["own", "all"]),
    })
    .nullable(),
  proof: z.string(),
  operationId: z.string(),
  expiresAt: z.string(),
})
export const operationSchema = z.object({
  operationId: z.string(),
  jobId: z.string(),
  status: z.enum(["confirmed", "accepted", "failed", "unknown"]),
  message: z.string(),
  reason: z.string(),
  states: z.array(z.string()),
  createdAt: z.string(),
  updatedAt: z.string(),
})
export type Operation = z.infer<typeof operationSchema>
export function errorMessage(error: unknown) {
  if (error instanceof ClientResponseError) {
    if (error.status === 403) return "你没有读取或操作该集群作业的权限。"
    if (error.status === 404)
      return "作业或操作记录不存在，或已不在控制器的可见记录中。"
    return error.response?.error?.message || "请求未完成，请刷新后重试。"
  }
  return "暂时无法连接平台服务，请检查网络后重试。"
}
export function quantity(value: z.infer<typeof numberSchema>, unit = "") {
  return value.infinite
    ? "不限"
    : value.value === null
      ? "—"
      : `${value.value.toLocaleString("zh-CN")}${unit}`
}
export function jobTime(value: z.infer<typeof numberSchema>) {
  if (value.infinite) return "不限"
  if (value.value === null || value.value === 0) return "未发生/未设置"
  const date = new Date(value.value * 1000)
  return Number.isNaN(date.valueOf())
    ? "—"
    : date.toLocaleString("zh-CN", { hour12: false })
}
export function active(job: Job) {
  return (
    !job.states.some((state) =>
      [
        "CANCELLED",
        "COMPLETED",
        "FAILED",
        "TIMEOUT",
        "NODE_FAIL",
        "BOOT_FAIL",
        "DEADLINE",
        "OUT_OF_MEMORY",
        "PREEMPTED",
        "COMPLETING",
      ].includes(state)
    ) &&
    job.states.some((state) =>
      ["PENDING", "RUNNING", "SUSPENDED"].includes(state)
    )
  )
}
export function operationLabel(status: Operation["status"]) {
  return {
    confirmed: "已核实取消",
    accepted: "已受理，待核实",
    failed: "未完成取消",
    unknown: "结果待核实",
  }[status]
}
