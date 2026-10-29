import { z } from "zod"
import {
  preparedSchema,
  operationSchema,
  specSchema,
} from "@/features/accounting/schema"
export { errorMessage, statuses } from "@/features/accounting/schema"

export const numberSchema = z.object({
  value: z.number().nullable(),
  infinite: z.boolean(),
})
export const qosSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string(),
  priority: numberSchema,
  usageFactor: numberSchema,
  maxJobsPerUser: numberSchema,
  maxSubmitJobsPerUser: numberSchema,
  maxWallMinutes: numberSchema,
  flags: z.array(z.string()),
  preemptModes: z.array(z.string()),
})
export const fairshareSchema = z.object({
  id: z.string(),
  cluster: z.string(),
  account: z.string(),
  user: z.string(),
  parent: z.string(),
  partition: z.string(),
  shares: numberSchema,
  normalizedShares: numberSchema,
  rawUsage: z.string().regex(/^\d+$/),
  normalizedUsage: numberSchema,
  effectiveUsage: numberSchema,
  factor: numberSchema,
  level: numberSchema,
})
const metadata = { fetchedAt: z.string(), cached: z.boolean() }
const page = {
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  ...metadata,
}
export const qosPageSchema = z.object({
  items: z.array(qosSchema),
  ...page,
  slurmCluster: z.string(),
  catalogScope: z.enum(["global", "referenced"]),
})
export const fairsharePageSchema = z.object({
  items: z.array(fairshareSchema),
  ...page,
})
export const qosDetailSchema = z.object({ item: qosSchema, ...metadata })
export type QOS = z.infer<typeof qosSchema>
export type Fairshare = z.infer<typeof fairshareSchema>
export type PolicyNumber = z.infer<typeof numberSchema>
export const qosSpecSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string(),
  priority: z.number(),
  usageFactor: z.number(),
  maxJobsPerUser: z.number().nullable(),
  maxSubmitJobsPerUser: z.number().nullable(),
  maxWallMinutes: z.number().nullable(),
})
export const policySpecSchema = specSchema.extend({ qos: qosSpecSchema })
export const policyPreparedSchema = preparedSchema.extend({
  spec: policySpecSchema,
})
export const policyOperationSchema = operationSchema.extend({
  spec: policySpecSchema,
})
export type Prepared = z.infer<typeof policyPreparedSchema>
export type Operation = z.infer<typeof policyOperationSchema>

export function formatNumber(v: PolicyNumber) {
  if (v.infinite) return "无限"
  if (v.value === null) return "未提供"
  if (v.value !== 0 && v.value < 0.0001) return v.value.toExponential(4)
  return v.value.toLocaleString("zh-CN", { maximumFractionDigits: 6 })
}
const integerText = z
  .string()
  .refine(
    (v) =>
      v === "" ||
      (/^\d+$/.test(v) &&
        Number.isSafeInteger(Number(v)) &&
        Number(v) <= 2147483647),
    "使用 0–2147483647 的整数；留空为无限制"
  )
export const qosFormSchema = z.object({
  name: z
    .string()
    .regex(
      /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$/,
      "使用 1–64 位字母、数字、下划线、点或连字符"
    )
    .refine((v) => v.toLowerCase() !== "all", "不能使用保留名称 ALL"),
  description: z
    .string()
    .trim()
    .min(1, "请填写描述")
    .max(200, "最多 200 字")
    .regex(/^[^\r\n\0]*$/, "请使用单行描述"),
  priority: integerText.refine((v) => v !== "", "请填写优先级"),
  usageFactor: z
    .string()
    .refine(
      (v) =>
        v.trim() !== "" &&
        Number.isFinite(Number(v)) &&
        Number(v) >= 0 &&
        Number(v) <= 1000000,
      "使用 0–1000000 的有限数值"
    ),
  maxJobsPerUser: integerText,
  maxSubmitJobsPerUser: integerText,
  maxWallMinutes: integerText,
})
export const confirmationSchema = z.object({
  confirmation: z.string().min(1, "请输入确认文本"),
  reason: z
    .string()
    .trim()
    .min(2, "至少 2 字")
    .max(300, "最多 300 字")
    .regex(/^[^\r\n\0]*$/, "请使用单行原因"),
})
