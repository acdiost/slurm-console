import { z } from "zod"
import { ClientResponseError } from "pocketbase"
import { numberSchema } from "@/features/jobs/schema"
export { quantity, jobTime } from "@/features/jobs/schema"
const resource = z.object({
  total: z.number(),
  allocated: z.number(),
  idle: z.number(),
  unavailable: z.number(),
})
export const nodeSchema = z.object({
  name: z.string(),
  hostname: z.string(),
  address: z.string(),
  version: z.string(),
  architecture: z.string(),
  operatingSystem: z.string(),
  states: z.array(z.string()),
  cpus: z.number(),
  effectiveCpus: z.number(),
  allocatedCpus: z.number(),
  memoryMiB: z.number(),
  allocatedMemoryMiB: z.number(),
  freeMemoryMiB: numberSchema,
  partitions: z.array(z.string()),
  reason: z.string(),
  reasonUser: z.string(),
  comment: z.string(),
  reservation: z.string(),
  features: z.array(z.string()),
  activeFeatures: z.array(z.string()),
  bootTime: numberSchema,
  slurmdStartTime: numberSchema,
  reasonChangedAt: numberSchema,
  resumeAfter: numberSchema,
  gres: z.string(),
  gresUsed: z.string(),
})
export const nodeViewSchema = nodeSchema.extend({
  resources: z.object({
    cpus: resource,
    memoryMiB: resource,
    gpus: z.object({
      total: z.number().nullable(),
      allocated: z.number().nullable(),
      idle: z.number().nullable(),
      unavailable: z.number().nullable(),
      unknownNodes: z.number(),
    }),
  }),
})
export type Node = z.infer<typeof nodeViewSchema>
export const nodePageSchema = z.object({
  items: z.array(nodeViewSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  fetchedAt: z.string(),
  cached: z.boolean(),
})
export const nodeDetailSchema = z.object({
  item: nodeViewSchema,
  fetchedAt: z.string(),
  cached: z.boolean(),
})
export type Action = "drain" | "resume"
export const contextSchema = z.object({
  allowed: z.boolean(),
  message: z.string(),
  node: nodeSchema,
  action: z.enum(["drain", "resume"]),
  proof: z.string(),
  operationId: z.string(),
  expiresAt: z.string(),
})
export type Context = z.infer<typeof contextSchema>
export const operationSchema = z.object({
  operationId: z.string(),
  nodeName: z.string(),
  action: z.enum(["drain", "resume"]),
  status: z.enum(["processing", "confirmed", "accepted", "failed", "unknown"]),
  message: z.string(),
  reason: z.string(),
  states: z.array(z.string()),
  createdAt: z.string(),
  updatedAt: z.string(),
})
export type Operation = z.infer<typeof operationSchema>
export const actionLabel = (action: Action) =>
  action === "drain" ? "Drain 排空" : "Resume 恢复"
export const operationLabel = (status: Operation["status"]) =>
  ({
    processing: "正在处理",
    confirmed: "已核实",
    accepted: "已受理，待核实",
    failed: "未完成",
    unknown: "结果待核实",
  })[status]
export const nodeNameValid = (name: string) =>
  /^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$/.test(name)
export async function pooled<T, R>(
  values: T[],
  run: (value: T) => Promise<R>,
  concurrency = 3
): Promise<R[]> {
  const results: R[] = new Array(values.length)
  let index = 0
  await Promise.all(
    Array.from({ length: Math.min(concurrency, values.length) }, async () => {
      while (index < values.length) {
        const current = index++
        results[current] = await run(values[current])
      }
    })
  )
  return results
}

export function errorMessage(error: unknown): string {
  if (error instanceof ClientResponseError) {
    if (error.status === 403) return "你没有读取或控制该集群节点的权限。"
    if (error.status === 404)
      return "节点或操作记录不存在，或对连接身份不可见。"
    return error.response?.error?.message || "节点请求未完成，请刷新后重试。"
  }
  return "暂时无法连接平台服务，请检查网络后重试。"
}
