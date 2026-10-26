import { z } from "zod"
import { ClientResponseError } from "pocketbase"
import { numberSchema } from "@/features/jobs/schema"
import { nodeViewSchema } from "@/features/nodes/schema"
import { timeLimit } from "@/features/overview/schema"
export const partitionSchema = z.object({
  name: z.string(),
  states: z.array(z.string()),
  nodes: z.string(),
  nodeCount: z.number(),
  cpus: z.number(),
  defaultTimeMinutes: numberSchema,
  maxTimeMinutes: numberSchema,
  allowedAccounts: z.string(),
  deniedAccounts: z.string(),
  allowedGroups: z.string(),
  allowedQos: z.string(),
  deniedQos: z.string(),
  assignedQos: z.string(),
  allowedAllocationNodes: z.string(),
  alternate: z.string(),
  configuredTres: z.string(),
  billingWeights: z.string(),
  nodeSets: z.string(),
  topology: z.string(),
  selectType: z.array(z.string()),
  defaultJob: z.string(),
  defaultMemoryPerCpuMiB: numberSchema,
  defaultMemoryPerNodeMiB: numberSchema,
  maxMemoryPerCpuMiB: numberSchema,
  maxMemoryPerNodeMiB: numberSchema,
  minNodes: numberSchema,
  maxNodes: numberSchema,
  maxCpusPerNode: numberSchema,
  maxCpusPerSocket: numberSchema,
  priorityTier: numberSchema,
  priorityJobFactor: numberSchema,
  graceTimeSeconds: numberSchema,
  overTimeLimitMinutes: numberSchema,
  oversubscribeJobs: numberSchema,
  oversubscribeFlags: z.array(z.string()),
})
export type Partition = z.infer<typeof partitionSchema>
export const partitionPageSchema = z.object({
  items: z.array(partitionSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  fetchedAt: z.string(),
  cached: z.boolean(),
})
const summary = nodeViewSchema.shape.resources.extend({
  total: z.number(),
  idle: z.number(),
  allocated: z.number(),
  mixed: z.number(),
  unavailable: z.number(),
  states: z.array(z.object({ state: z.string(), count: z.number() })),
  issues: z.array(
    z.object({
      name: z.string(),
      states: z.array(z.string()),
      reason: z.string(),
    })
  ),
})
const source = {
  code: z.string(),
  message: z.string(),
  fetchedAt: z.string(),
  cached: z.boolean(),
}
export const partitionDetailSchema = z.object({
  item: partitionSchema,
  fetchedAt: z.string(),
  cached: z.boolean(),
  revision: z.number(),
  resources: z.discriminatedUnion("status", [
    z.object({
      ...source,
      status: z.literal("ok"),
      item: z.object({
        summary,
        configuredNodes: z.number(),
        complete: z.boolean(),
      }),
    }),
    z.object({
      ...source,
      status: z.enum(["forbidden", "error"]),
      item: z.null(),
    }),
  ]),
})
export type Detail = z.infer<typeof partitionDetailSchema>
export function errorMessage(error: unknown): string {
  if (error instanceof ClientResponseError) {
    if (error.status === 403) return "你没有读取该集群分区的权限。"
    if (error.status === 404) return "分区不存在或对连接身份不可见。"
    return error.response?.error?.message || "分区请求未完成，请刷新后重试。"
  }
  return "暂时无法连接平台服务，请检查网络后重试。"
}
export function limit(value: z.infer<typeof numberSchema>, unit = "") {
  return value.infinite
    ? "不限"
    : value.value === null
      ? "未设置/未返回"
      : `${value.value.toLocaleString("zh-CN")}${unit}`
}
export function memoryLimit(value: z.infer<typeof numberSchema>) {
  return value.value === 0 && !value.infinite
    ? "不限（配置值 0）"
    : limit(value, " MiB")
}
export function partitionTime(value: z.infer<typeof numberSchema>) {
  return value.value === null && !value.infinite
    ? "未设置/未返回"
    : timeLimit(value)
}
export function stateHelp(states: string[]) {
  const help: Record<string, string> = {
    UP: "允许新作业提交和调度，实际能否运行仍取决于节点状态和资源约束。",
    DOWN: "允许新作业提交，暂停排队作业调度；已运行作业继续执行。",
    DRAIN: "拒绝新作业提交，已排队作业仍可能被调度运行。",
    INACTIVE: "拒绝新作业提交，并暂停排队作业调度。",
  }
  return states
    .map((state) => help[state] || `${state}：当前版本未定义此状态的说明。`)
    .join(" ")
}
