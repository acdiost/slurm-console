import { z } from "zod"
export { errorMessage } from "@/features/accounting/schema"
export const sampleSchema = z.object({
  value: z.number().nullable(),
  timestamp: z.number().nullable(),
  status: z.enum(["fresh", "missing", "stale", "invalid", "ambiguous"]),
})
export const deviceSchema = z.object({
  uuid: z.string(),
  node: z.string(),
  index: z.string(),
  model: z.string(),
  status: z.string(),
  utilization: sampleSchema,
  memoryUsedMiB: sampleSchema,
  memoryFreeMiB: sampleSchema,
  memoryTotalMiB: sampleSchema,
  temperatureC: sampleSchema,
  powerW: sampleSchema,
  totalSource: z.string(),
})
const number = z.number().nullable()
export const pageSchema = z.object({
  monitoring: z.object({
    status: z.string(),
    message: z.string(),
    source: z.string(),
    fetchedAt: z.string(),
    cached: z.boolean(),
    ignoredSeries: z.number(),
    items: z.array(deviceSchema),
  }),
  allocation: z.object({
    status: z.string(),
    message: z.string(),
    fetchedAt: z.string(),
    cached: z.boolean(),
    item: z
      .object({
        gpus: z.object({
          total: number,
          allocated: number,
          idle: number,
          unavailable: number,
          unknownNodes: z.number(),
        }),
      })
      .nullable(),
  }),
  items: z.array(deviceSchema),
  summary: z.record(z.string(), z.number()),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  revision: z.number(),
})
export const historySchema = z.object({
  uuid: z.string(),
  node: z.string(),
  range: z.string(),
  start: z.number(),
  end: z.number(),
  step: z.number(),
  series: z.record(
    z.string(),
    z.array(z.object({ time: z.number(), value: z.number().nullable() }))
  ),
  fetchedAt: z.string(),
  cached: z.boolean(),
})
export type Device = z.infer<typeof deviceSchema>
export type Sample = z.infer<typeof sampleSchema>
export type History = z.infer<typeof historySchema>
export const labels: Record<string, string> = {
  fresh: "新鲜",
  partial: "部分指标未知",
  stale: "数据过期",
  ambiguous: "重复来源",
  missing: "未采集",
  invalid: "无效值",
  not_configured: "未配置监控源",
  no_data: "无物理 GPU 数据",
  error: "来源不可用",
  ok: "来源正常",
}
export const metrics: Record<string, { label: string; unit: string }> = {
  utilization: { label: "GPU 实际利用率", unit: "%" },
  memoryUsedMiB: { label: "显存已用", unit: "MiB" },
  memoryTotalMiB: { label: "监测显存总量", unit: "MiB" },
  memoryFreeMiB: { label: "显存空闲", unit: "MiB" },
  temperatureC: { label: "GPU 温度", unit: "°C" },
  powerW: { label: "GPU 功耗", unit: "W" },
}
export function value(s: Sample, unit = "") {
  return s.value === null
    ? labels[s.status] || "未知"
    : `${s.value.toLocaleString("zh-CN", { maximumFractionDigits: 1 })}${unit ? ` ${unit}` : ""}`
}
export function time(v: string) {
  return v ? new Date(v).toLocaleTimeString("zh-CN", { hour12: false }) : "未知"
}
