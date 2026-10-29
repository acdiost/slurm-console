import { z } from "zod"
import {
  specSchema,
  preparedSchema,
  operationSchema,
} from "@/features/accounting/schema"
export { errorMessage, statuses } from "@/features/accounting/schema"
export const reservationSchema = z.object({
  name: z.string(),
  startTime: z.number().nullable(),
  endTime: z.number().nullable(),
  nodes: z.string(),
  nodeNames: z.array(z.string()),
  nodeCount: z.number(),
  coreCount: z.number(),
  users: z.string(),
  accounts: z.string(),
  groups: z.string(),
  partition: z.string(),
  features: z.string(),
  licenses: z.string(),
  burstBuffer: z.string(),
  tres: z.string(),
  flags: z.array(z.string()),
  editable: z.boolean(),
  state: z.enum(["scheduled", "active", "ended", "unknown"]),
})
export type Reservation = z.infer<typeof reservationSchema>
export const mutationSchema = z.object({
  name: z.string(),
  startTime: z.number(),
  endTime: z.number(),
  nodes: z.array(z.string()).nullable(),
  users: z.array(z.string()).nullable(),
  accounts: z.array(z.string()).nullable(),
  maintenance: z.boolean(),
})
const spec = specSchema.extend({ reservation: mutationSchema })
export const reservationPreparedSchema = preparedSchema.extend({ spec })
export const reservationOperationSchema = operationSchema.extend({ spec })
export type Prepared = z.infer<typeof reservationPreparedSchema>
export type Action =
  "reservation_create" | "reservation_update" | "reservation_delete"
export const actions: Record<Action, string> = {
  reservation_create: "新建预留",
  reservation_update: "调整结束时间",
  reservation_delete: "删除预留",
}
export const states: Record<string, string> = {
  scheduled: "尚未开始",
  active: "生效中",
  ended: "已结束",
  unknown: "时间未知",
}
const meta = { fetchedAt: z.string(), cached: z.boolean() }
export const pageSchema = z.object({
  items: z.array(reservationSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  ...meta,
})
export const detailSchema = z.object({ item: reservationSchema, ...meta })
export const dateTime = (n: number | null) =>
  n === null
    ? "未知"
    : new Date(n * 1000).toLocaleString("zh-CN", { hour12: false })
export function localInput(n: number) {
  const d = new Date(n * 1000)
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 19)
}
export const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
