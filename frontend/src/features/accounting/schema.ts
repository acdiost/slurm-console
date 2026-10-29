import { z } from "zod"
import { ClientResponseError } from "pocketbase"
export const accountSchema = z.object({
  name: z.string(),
  description: z.string(),
  organization: z.string(),
  associationCount: z.number().int().nonnegative(),
  userCount: z.number().int().nonnegative(),
})
export const userSchema = z.object({
  name: z.string(),
  defaultAccount: z.string(),
  defaultAccountScope: z.enum(["cluster", "outside_cluster", "unset"]),
  administratorLevel: z.array(z.string()),
  associationCount: z.number().int().nonnegative(),
})
export const associationSchema = z.object({
  id: z.string(),
  cluster: z.string(),
  account: z.string(),
  user: z.string(),
  partition: z.string(),
  parentAccount: z.string(),
  isDefault: z.boolean(),
  comment: z.string(),
})
export type Account = z.infer<typeof accountSchema>
export type SlurmUser = z.infer<typeof userSchema>
export type Association = z.infer<typeof associationSchema>
export type Kind = "accounts" | "slurm-users" | "associations"
export type Action =
  | "account_create"
  | "account_update"
  | "user_create"
  | "association_create"
  | "association_remove"
export const actions: Record<Action, string> = {
  account_create: "创建 Account",
  account_update: "编辑 Account",
  user_create: "创建 Slurm 用户",
  association_create: "创建 Association",
  association_remove: "移除 Association",
}
export const kinds: Record<Kind, string> = {
  accounts: "Account",
  "slurm-users": "Slurm 用户",
  associations: "Association",
}
const metadata = { fetchedAt: z.string(), cached: z.boolean() }
export function pageSchema(kind: Kind) {
  return z.object({
    items: z.array(
      kind === "accounts"
        ? accountSchema
        : kind === "slurm-users"
          ? userSchema
          : associationSchema
    ),
    page: z.number(),
    perPage: z.number(),
    totalItems: z.number(),
    ...metadata,
  })
}
export function detailSchema(kind: Kind) {
  return z.object({
    item:
      kind === "accounts"
        ? accountSchema
        : kind === "slurm-users"
          ? userSchema
          : associationSchema,
    ...metadata,
  })
}
export const specSchema = z.object({
  account: z.string(),
  user: z.string(),
  partition: z.string(),
  associationId: z.string(),
  description: z.string(),
  organization: z.string(),
})
export type Spec = z.infer<typeof specSchema>
export const emptySpec: Spec = {
  account: "",
  user: "",
  partition: "",
  associationId: "",
  description: "",
  organization: "",
}
export const preparedSchema = z.object({
  allowed: z.boolean(),
  message: z.string(),
  action: z.string(),
  spec: specSchema,
  slurmCluster: z.string(),
  global: z.boolean(),
  confirmation: z.string(),
  proof: z.string(),
  operationId: z.string(),
  expiresAt: z.string(),
})
export type Prepared = z.infer<typeof preparedSchema>
export const operationSchema = z.object({
  operationId: z.string(),
  action: z.string(),
  spec: specSchema,
  slurmCluster: z.string(),
  status: z.string(),
  message: z.string(),
  reason: z.string(),
  createdAt: z.string(),
  updatedAt: z.string(),
})
export type Operation = z.infer<typeof operationSchema>
export const statuses: Record<string, string> = {
  processing: "处理中",
  confirmed: "已核实",
  accepted: "已受理，待核实",
  failed: "未完成",
  unknown: "结果待核实",
}
export function errorMessage(error: unknown) {
  if (error instanceof ClientResponseError)
    return (
      error.response?.error?.message ||
      (error.status === 403
        ? "没有该集群的记账操作权限。"
        : "记账请求未完成，请稍后查询。")
    )
  if (error instanceof z.ZodError)
    return "服务返回的记账数据格式异常，请联系管理员。"
  return "无法连接平台，请检查网络；已发起的操作应按操作编号核实，勿重复提交。"
}
const name = z
  .string()
  .regex(
    /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$/,
    "使用 1–64 位字母、数字、下划线、点或连字符"
  )
const text = z
  .string()
  .trim()
  .min(1, "请填写此项")
  .max(200, "最多 200 字")
  .regex(/^[^\r\n\0]*$/, "请使用单行内容")
export function formSchema(action: Action) {
  return specSchema.superRefine((v, ctx) => {
    if (action === "account_create" && v.account !== v.account.toLowerCase())
      ctx.addIssue({
        code: "custom",
        path: ["account"],
        message: "新 Account 请使用小写名称",
      })
    if (action === "user_create" && v.user !== v.user.toLowerCase())
      ctx.addIssue({
        code: "custom",
        path: ["user"],
        message: "新 Slurm 用户请使用小写名称",
      })
    const fields = action.startsWith("account_")
      ? (["account", "description", "organization"] as const)
      : (["account", "user"] as const)
    for (const key of fields) {
      const result = (
        key === "description" || key === "organization" ? text : name
      ).safeParse(v[key])
      if (!result.success)
        ctx.addIssue({
          code: "custom",
          path: [key],
          message: result.error.issues[0]?.message || "请检查内容",
        })
    }
    if (v.partition && !name.safeParse(v.partition).success)
      ctx.addIssue({
        code: "custom",
        path: ["partition"],
        message: "请填写有效的单个分区名称",
      })
  })
}
