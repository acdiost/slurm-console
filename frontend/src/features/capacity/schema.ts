import { z } from "zod"
import { ClientResponseError } from "pocketbase"
const n = z.number().int().nonnegative()
export const resourceSchema = z.object({
  total: n,
  allocated: n,
  idle: n,
  unavailable: n,
})
export const frameSchema = z.object({
  version: z.literal(1),
  nodes: n,
  unavailableNodes: n,
  cpus: resourceSchema,
  memoryMiB: resourceSchema,
  gpus: z.object({
    total: n.nullable(),
    allocated: n.nullable(),
    idle: n.nullable(),
    unavailable: n.nullable(),
    unknownNodes: n,
  }),
})
export type Frame = z.infer<typeof frameSchema>
export type Resource = "cpu" | "memory" | "gpu"
export const reportSchema = z.object({
  revision: n,
  generatedAt: z.string(),
  source: z.object({ fetchedAt: z.string(), cached: z.boolean() }),
  report: z.object({
    totals: frameSchema,
    headroom: z.object({
      stateAvailableNodes: n,
      largestCpuIdle: n,
      largestMemoryIdleMiB: n,
      cpuIdleBuckets: z
        .array(z.object({ label: z.string(), nodes: n }))
        .length(5),
    }),
    partitions: z
      .array(
        z.object({
          name: z.string(),
          states: z.array(z.string()).nullable(),
          frame: frameSchema,
        })
      )
      .max(100),
    page: n,
    perPage: n,
    totalItems: n,
  }),
  partitionSource: z.object({
    status: z.enum(["ok", "error", "forbidden"]),
    code: z.string(),
    message: z.string(),
  }),
})
const stat = z.object({
  knownSamples: n,
  weightedAllocatedPercent: z.number().min(0).max(100).nullable(),
  peakAllocatedPercent: z.number().min(0).max(100).nullable(),
  minimumIdle: n.nullable(),
})
export const historySchema = z.object({
  revision: n,
  history: z.object({
    start: n,
    end: n,
    stepSeconds: z.literal(300),
    expectedSamples: n,
    availableSamples: n,
    points: z
      .array(
        z.object({
          bucket: n,
          observedAt: z.string(),
          frame: frameSchema.nullable(),
        })
      )
      .max(2016),
    cpus: stat,
    memoryMiB: stat,
    gpus: stat,
  }),
})
export type History = z.infer<typeof historySchema>["history"]
export function resource(frame: Frame, key: Resource) {
  return key === "cpu"
    ? frame.cpus
    : key === "memory"
      ? frame.memoryMiB
      : frame.gpus
}
export function percentage(v: number | null, total: number | null) {
  return v !== null && total !== null && total > 0 ? (100 * v) / total : null
}
export function number(v: number | null | undefined) {
  return v == null
    ? "—"
    : new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 }).format(v)
}
export function percent(v: number | null | undefined) {
  return v == null ? "—" : `${v.toFixed(1)}%`
}
export function memory(v: number | null | undefined) {
  return v == null
    ? "—"
    : v >= 1024
      ? `${number(v / 1024)} GiB`
      : `${number(v)} MiB`
}
export function date(v: number | string) {
  const t = typeof v === "number" ? v * 1000 : Date.parse(v)
  return Number.isFinite(t)
    ? new Date(t).toLocaleString("zh-CN", { hour12: false })
    : "—"
}
export function denied(e: unknown) {
  return e instanceof ClientResponseError && [403, 404].includes(e.status)
}
export function errorMessage(e: unknown) {
  if (denied(e)) return "容量分析或节点读取权限不可用，请检查当前集群授权。"
  if (e instanceof ClientResponseError && e.status === 429)
    return "容量查询繁忙，请稍后刷新。"
  return "容量来源暂不可用，请稍后刷新；缺失数据不会补零。"
}
