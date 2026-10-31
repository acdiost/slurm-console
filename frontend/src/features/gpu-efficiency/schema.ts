import { z } from "zod"
export { denied, date, percent, number } from "@/features/capacity/schema"
const nullable = z.number().nullable()
const count = z.number().int().nonnegative()
const stats = z.object({
  samples: count,
  coveragePercent: z.number().min(0).max(100),
  mean: nullable,
  p95: nullable,
  peak: nullable,
})
export const efficiencySchema = z.object({
  revision: count,
  page: count,
  perPage: count,
  totalItems: count,
  analysis: z.object({
    status: z.enum(["ok", "no_data", "not_configured"]),
    source: z.string(),
    range: z.string(),
    start: count,
    end: count,
    step: count,
    expectedSamples: count,
    lowThresholdPercent: count,
    minimumCoveragePercent: count,
    minimumLowSamplePercent: count,
    fetchedAt: z.string(),
    cached: z.boolean(),
    ignoredSeries: count,
    summary: z.object({
      observedDevices: count,
      low: count,
      active: count,
      insufficient: count,
      ambiguous: count,
      validSamples: count,
      meanUtilization: nullable,
    }),
    points: z
      .array(
        z.object({ time: count, meanUtilization: nullable, devices: count })
      )
      .max(144),
    items: z
      .array(
        z.object({
          uuid: z.string(),
          node: z.string(),
          index: z.string(),
          model: z.string(),
          status: z.enum(["low", "active", "insufficient", "ambiguous"]),
          utilization: stats,
          memoryPercent: stats,
          powerW: stats,
          lowSamples: count,
          lowSamplePercent: nullable,
        })
      )
      .max(100),
  }),
})
export type EfficiencyPage = z.infer<typeof efficiencySchema>
export type Analysis = z.infer<typeof efficiencySchema>["analysis"]
export const statuses: Record<string, string> = {
  low: "低利用率线索",
  active: "未达低利用率条件",
  insufficient: "利用率采样不足",
  ambiguous: "来源身份冲突",
}
export function trendSegments(points: Analysis["points"]) {
  const result: string[] = []
  let current: string[] = []
  const flush = () => {
    if (current.length) result.push(current.join(" "))
    current = []
  }
  points.forEach((p, i) => {
    if (p.meanUtilization === null) flush()
    else
      current.push(
        `${40 + (920 * i) / Math.max(1, points.length - 1)},${180 - 1.6 * p.meanUtilization}`
      )
  })
  flush()
  return result
}
