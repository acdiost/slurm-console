import { z } from "zod"
import { ClientResponseError } from "pocketbase"
const amount = z
  .string()
  .regex(/^(0|[1-9][0-9]{0,39})$/)
  .nullable()
const metric = z.object({
  resourceSeconds: amount,
  knownRecords: z.number().int().nonnegative(),
  unknownRecords: z.number().int().nonnegative(),
})
const row = z.object({
  key: z.string(),
  records: z.number().int().nonnegative(),
  countedRecords: z.number().int().nonnegative(),
  excluded: z.object({
    active: z.number(),
    complex: z.number(),
    uncertain: z.number(),
    overlapping: z.number(),
  }),
  cpu: metric,
  memory: metric,
  gpu: metric,
})
export const reportSchema = z.object({
  report: z.object({
    version: z.literal(1),
    basis: z.literal("execution_overlap_estimate"),
    cluster: z.string(),
    start: z.number(),
    end: z.number(),
    groupBy: z.enum(["user", "account", "partition"]),
    sourceRecords: z.number(),
    outsideRecords: z.number(),
    summary: row,
    groups: z.array(row).max(5000),
  }),
  fetchedAt: z.string(),
  cached: z.boolean(),
  revision: z.number(),
})
export type UsageReport = z.infer<typeof reportSchema>
export type ReportRow = z.infer<typeof row>
export type ReportMetric = z.infer<typeof metric>
export const dimensions = {
  user: "用户",
  account: "Account",
  partition: "分区",
}
export function hours(v: string | null, memory = false) {
  if (v === null) return "未知"
  const divisor = memory ? 3600n * 1024n : 3600n
  const scaled = (BigInt(v) * 1000n + divisor / 2n) / divisor
  if (BigInt(v) > 0n && scaled === 0n) return "<0.001"
  return `${(scaled / 1000n).toLocaleString("zh-CN")}.${String(scaled % 1000n).padStart(3, "0")}`
}
export function sorted(rows: ReportRow[], sort: string, order: string) {
  return [...rows].sort((a, b) => {
    if (sort === "key")
      return (order === "asc" ? 1 : -1) * a.key.localeCompare(b.key, "zh-CN")
    const x =
      sort === "records"
        ? String(a.records)
        : a[sort as "cpu" | "memory" | "gpu"].resourceSeconds
    const y =
      sort === "records"
        ? String(b.records)
        : b[sort as "cpu" | "memory" | "gpu"].resourceSeconds
    if (x === null && y !== null) return 1
    if (x !== null && y === null) return -1
    if (x !== null && y !== null && BigInt(x) !== BigInt(y))
      return (BigInt(x) < BigInt(y) ? -1 : 1) * (order === "asc" ? 1 : -1)
    return a.key.localeCompare(b.key, "zh-CN")
  })
}
export function errorMessage(e: unknown) {
  if (e instanceof ClientResponseError) {
    if (e.status === 403) return "报表或历史记账读取权限不可用。"
    if (e.status === 404) return "集群不存在或已不可访问。"
    return e.response?.error?.message || "记账报表暂不可用，请稍后重试。"
  }
  return "报表请求未完成，请检查网络后重试。"
}
function cell(v: unknown) {
  let s = String(v ?? "")
  let first = 0
  while (first < s.length && (s.charCodeAt(first) <= 32 || /\s/.test(s[first])))
    first++
  if (["=", "+", "@", "-"].includes(s[first]) || /^[\t\r\n]/.test(s))
    s = "'" + s
  return `"${s.replaceAll('"', '""')}"`
}
// All groups from one freshly authorized snapshot. Decimal integers retain precision.
export function csv(data: UsageReport, clusterId: string) {
  const r = data.report
  const headers = [
    "row_type",
    "logical_cluster_id",
    "source_cluster",
    "window_start_unix",
    "window_end_unix",
    "basis",
    "fetched_at",
    "group_by",
    "group",
    "records",
    "time_qualified_records",
    "excluded_active",
    "excluded_complex",
    "excluded_uncertain",
    "excluded_overlapping",
    "cpu_seconds_estimate",
    "cpu_known_records",
    "cpu_unknown_records",
    "memory_mib_seconds_estimate",
    "memory_known_records",
    "memory_unknown_records",
    "gpu_unit_seconds_estimate",
    "gpu_known_records",
    "gpu_unknown_records",
  ]
  const line = (row: ReportRow, type: string) => [
    type,
    clusterId,
    r.cluster,
    r.start,
    r.end,
    r.basis,
    data.fetchedAt,
    r.groupBy,
    row.key,
    row.records,
    row.countedRecords,
    row.excluded.active,
    row.excluded.complex,
    row.excluded.uncertain,
    row.excluded.overlapping,
    ...[row.cpu, row.memory, row.gpu].flatMap((m) => [
      m.resourceSeconds,
      m.knownRecords,
      m.unknownRecords,
    ]),
  ]
  return (
    "\uFEFF" +
    [
      headers,
      line(r.summary, "summary"),
      ...r.groups.map((v) => line(v, "group")),
    ]
      .map((v) => v.map(cell).join(","))
      .join("\r\n") +
    "\r\n"
  )
}
