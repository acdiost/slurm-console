import { z } from "zod"

export const clusterSchema = z.object({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
  description: z.string(),
})
export const clusterPageSchema = z.object({
  items: z.array(clusterSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
})
const number = z.number().int().nonnegative()
const resourceSchema = z.object({
  total: number,
  allocated: number,
  idle: number,
  unavailable: number,
})
const stateCount = z.object({ state: z.string(), count: number })
const slurmNumber = z.object({
  value: z.number().nullable(),
  infinite: z.boolean(),
})
function section<T extends z.ZodType>(schema: T) {
  const base = {
    code: z.string(),
    message: z.string(),
    fetchedAt: z.string(),
    cached: z.boolean(),
  }
  return z.discriminatedUnion("status", [
    z.object({ ...base, status: z.literal("ok"), item: schema }),
    z.object({
      ...base,
      status: z.enum(["error", "forbidden"]),
      item: z.null(),
    }),
  ])
}
export const overviewSchema = z.object({
  cluster: clusterSchema,
  revision: number,
  generatedAt: z.string(),
  health: section(
    z.object({
      status: z.enum(["connected", "controller_down"]),
      serverVersion: z.string(),
      apiVersion: z.string(),
      cluster: z.string(),
      controllers: z.array(
        z.object({ hostname: z.string(), state: z.string(), role: z.string() })
      ),
      capabilities: z.array(z.string()),
    })
  ),
  nodes: section(
    z.object({
      total: number,
      idle: number,
      allocated: number,
      mixed: number,
      unavailable: number,
      cpus: resourceSchema,
      memoryMiB: resourceSchema,
      gpus: z.object({
        total: number.nullable(),
        allocated: number.nullable(),
        idle: number.nullable(),
        unavailable: number.nullable(),
        unknownNodes: number,
      }),
      states: z.array(stateCount),
      issues: z.array(
        z.object({
          name: z.string(),
          states: z.array(z.string()),
          reason: z.string(),
        })
      ),
    })
  ),
  jobs: section(
    z.object({
      total: number,
      running: number,
      pending: number,
      failed: number,
      completing: number,
      completed: number,
      cancelled: number,
      other: number,
      states: z.array(stateCount),
    })
  ),
  partitions: section(
    z.object({
      total: number,
      up: number,
      items: z.array(
        z.object({
          name: z.string(),
          states: z.array(z.string()),
          nodes: z.string(),
          nodeCount: number,
          cpus: number,
          defaultTimeMinutes: slurmNumber,
          maxTimeMinutes: slurmNumber,
        })
      ),
    })
  ),
})
export type Overview = z.infer<typeof overviewSchema>
export type Resource = z.infer<typeof resourceSchema>
export type Section =
  | Overview["health"]
  | Overview["nodes"]
  | Overview["jobs"]
  | Overview["partitions"]
export function timestamp(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.valueOf())
    ? "—"
    : date.toLocaleTimeString("zh-CN", { hour12: false })
}
export function memory(value: number) {
  return `${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 2 }).format(value / 1024)} GiB`
}
export function timeLimit(value: z.infer<typeof slurmNumber>) {
  if (value.infinite) return "不限时"
  if (value.value === null) return "未设置"
  if (value.value >= 1440)
    return `${Number((value.value / 1440).toFixed(1))} 天`
  if (value.value >= 60) return `${Number((value.value / 60).toFixed(1))} 小时`
  return `${value.value} 分钟`
}
