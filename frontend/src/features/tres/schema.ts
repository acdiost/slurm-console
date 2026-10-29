import { z } from "zod"
import {
  specSchema,
  preparedSchema,
  operationSchema,
} from "@/features/accounting/schema"
export { errorMessage, statuses } from "@/features/accounting/schema"
export const resourceSchema = z.object({
  id: z.string(),
  type: z.string(),
  name: z.string(),
  key: z.string(),
  unit: z.string(),
  editable: z.boolean(),
})
export const amountSchema = z.object({
  id: z.string(),
  value: z.string(),
  state: z.enum(["finite", "infinite", "unset"]),
})
export const targetSchema = z.object({
  kind: z.enum(["qos", "association"]),
  id: z.string(),
  name: z.string(),
  cluster: z.string(),
  account: z.string(),
  user: z.string(),
  partition: z.string(),
  relative: z.boolean(),
  limits: z.record(z.string(), z.array(amountSchema)),
})
const meta = { fetchedAt: z.string(), cached: z.boolean() }
export const pageSchema = z.object({
  items: z.array(targetSchema),
  resources: z.array(resourceSchema),
  slurmCluster: z.string(),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  ...meta,
})
export const detailSchema = z.object({
  item: targetSchema,
  resources: z.array(resourceSchema),
  ...meta,
})
export const tresSpecSchema = z.object({
  kind: z.enum(["qos", "association"]),
  targetId: z.string(),
  name: z.string(),
  account: z.string(),
  user: z.string(),
  partition: z.string(),
  resourceId: z.string(),
  bucket: z.string(),
  mode: z.enum(["set", "clear"]),
  value: z.string(),
})
const spec = specSchema.extend({ tres: tresSpecSchema })
export const tresPreparedSchema = preparedSchema.extend({ spec })
export const tresOperationSchema = operationSchema.extend({ spec })
export type Resource = z.infer<typeof resourceSchema>
export type Target = z.infer<typeof targetSchema>
export type Prepared = z.infer<typeof tresPreparedSchema>
export const buckets: Record<
  string,
  {
    label: string
    description: string
    qos: boolean
    association: boolean
    minutes: boolean
  }
> = {
  group_total: {
    label: "总并发资源 · GrpTRES",
    description: "对象及其适用下级关联的运行资源总量",
    qos: true,
    association: true,
    minutes: false,
  },
  per_job: {
    label: "单作业 · MaxTRESPerJob",
    description: "每个作业可申请的资源上限",
    qos: true,
    association: true,
    minutes: false,
  },
  per_user: {
    label: "每用户 · MaxTRESPerUser",
    description: "QOS 下每个用户的运行资源上限",
    qos: true,
    association: false,
    minutes: false,
  },
  per_account: {
    label: "每账户 · MaxTRESPerAccount",
    description: "QOS 下每个账户的运行资源上限",
    qos: true,
    association: false,
    minutes: false,
  },
  per_node: {
    label: "单节点 · MaxTRESPerNode",
    description: "每个作业在每节点的资源上限",
    qos: true,
    association: true,
    minutes: false,
  },
  group_minutes: {
    label: "累计额度 · GrpTRESMins",
    description: "累计 TRES 分钟预算，受站点衰减及执行策略影响",
    qos: true,
    association: true,
    minutes: true,
  },
  run_minutes: {
    label: "运行额度 · GrpTRESRunMins",
    description: "运行作业按时限申请的 TRES 分钟总量",
    qos: true,
    association: true,
    minutes: true,
  },
  job_minutes: {
    label: "单作业额度 · MaxTRESMinsPerJob",
    description: "每作业可申请的 TRES 分钟预算",
    qos: true,
    association: true,
    minutes: true,
  },
}
export function targetName(t: Target) {
  return t.kind === "qos"
    ? t.name
    : `${t.account} / ${t.user || "Account 层级"}${t.partition ? ` / ${t.partition}` : ""}`
}
export function limitText(v: z.infer<typeof amountSchema> | undefined) {
  return !v || v.state === "unset"
    ? "本层未设置"
    : v.state === "infinite"
      ? "显式无限"
      : BigInt(v.value).toLocaleString("zh-CN")
}
export function unit(t: Target, r: Resource | undefined, bucket: string) {
  return t.relative && !buckets[bucket]?.minutes
    ? "%"
    : `${r?.unit || "原始单位"}${buckets[bucket]?.minutes ? "·分钟" : ""}`
}
export function protectedTarget(t: Target) {
  return (
    t.kind === "association" &&
    [t.account, t.user].some((name) =>
      ["root", "slurm-console", "slurm", "munge", "nobody"].includes(
        name.toLowerCase()
      )
    )
  )
}
export const editSchema = z
  .object({
    resourceId: z.string().min(1, "请选择资源"),
    bucket: z.string().min(1, "请选择限额类型"),
    mode: z.enum(["set", "clear"]),
    value: z.string(),
  })
  .superRefine((v, c) => {
    if (
      v.mode === "set" &&
      (!/^(0|[1-9][0-9]{0,18})$/.test(v.value) ||
        BigInt(v.value) > 9223372036854775807n)
    )
      c.addIssue({
        code: "custom",
        path: ["value"],
        message: "使用 0–9223372036854775807 的标准十进制整数",
      })
  })
