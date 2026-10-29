import { z } from "zod"
import { ClientResponseError } from "pocketbase"
export const auditSchema = z.object({
  operationId: z.string(),
  actorId: z.string(),
  actorLabel: z.string(),
  actorSource: z.string(),
  clusterId: z.string(),
  clusterName: z.string(),
  clusterSlug: z.string(),
  clusterRevision: z.number(),
  action: z.string(),
  targetType: z.string(),
  targetId: z.string(),
  requestId: z.string(),
  status: z.string(),
  recordedStatus: z.string(),
  message: z.string(),
  reason: z.string(),
  states: z.array(z.string()),
  changes: z.array(
    z.object({ field: z.string(), before: z.string(), after: z.string() })
  ),
  createdAt: z.string(),
  updatedAt: z.string(),
})
export type Audit = z.infer<typeof auditSchema>
export const auditPageSchema = z.object({
  items: z.array(auditSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  fetchedAt: z.string(),
})
export const auditDetailSchema = z.object({
  item: auditSchema,
  fetchedAt: z.string(),
})
export const actions: Record<string, string> = {
  "qos.tres.update": "配置 QOS TRES 限额",
  "associations.tres.update": "配置 Association TRES 限额",
  "qos.create": "创建 QOS",
  "qos.update": "编辑 QOS",
  "jobs.cancel": "取消作业",
  "accounts.create": "创建 Slurm Account",
  "accounts.update": "编辑 Slurm Account",
  "slurm_users.create": "创建 Slurm 用户",
  "associations.create": "创建 Association",
  "associations.remove": "移除 Association",
  "nodes.drain": "Drain 节点",
  "nodes.resume": "Resume 节点",
  "linux_users.create": "创建本地账户",
  "linux_users.lock": "锁定本地账户",
  "linux_users.unlock": "解锁本地账户",
  "clusters.create": "创建连接",
  "clusters.update": "更新连接",
  "clusters.disable": "停用连接",
  "clusters.register": "登记集群",
  "users.create": "创建账户",
  "users.link": "切换认证来源",
  "access.grant": "授予角色",
  "access.revoke": "撤销角色",
  "identities.link": "关联 Slurm 身份",
  "identities.unlink": "解除 Slurm 身份",
}
export const statuses: Record<string, string> = {
  succeeded: "已完成",
  confirmed: "已核实",
  accepted: "已受理，待核实",
  failed: "未完成",
  unknown: "结果待核实",
}
export const fields: Record<string, string> = {
  quotaTargetId: "限额对象 ID",
  tresResource: "TRES 资源",
  tresBucket: "限额类型",
  tresLimit: "本层限额",
  tresIdentity: "对象身份",
  account: "Slurm Account",
  slurmCluster: "Slurm 集群",
  partition: "分区",
  associationId: "Association ID",
  description: "描述",
  organization: "组织",
  qosName: "QOS 名称",
  qosPriority: "QOS 优先级",
  usageFactor: "计费用量因子",
  maxJobsPerUser: "每用户运行作业上限",
  maxSubmitJobsPerUser: "每用户提交作业上限",
  maxWallMinutes: "单作业时限（分钟）",
  home: "主目录",
  shell: "登录 Shell",
  accountExpiry: "账户到期日（UTC 天数，-1 为无到期日）",
  name: "名称",
  slug: "标识",
  enabled: "启用连接",
  apiVersion: "API 版本",
  connectionRef: "受保护连接方式引用",
  timeoutSeconds: "超时（秒）",
  revision: "配置版本",
  authSource: "认证来源",
  username: "系统用户名",
  uid: "Slurm UID",
  scope: "作业操作范围",
  role: "角色",
}
export function dateTime(value: string) {
  const date = new Date(value.replace(" ", "T"))
  return Number.isNaN(date.valueOf())
    ? "—"
    : date.toLocaleString("zh-CN", { hour12: false })
}
export function localInput(value: string) {
  if (!value) return ""
  const d = new Date(value.replace(" ", "T"))
  if (Number.isNaN(d.valueOf())) return value
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
export function errorMessage(error: unknown) {
  if (error instanceof ClientResponseError) {
    if (error.status === 403) return "你没有查看平台审计的权限。"
    if (error.status === 404) return "审计记录不存在。"
    return error.response?.error?.message || "审计查询未完成，请稍后刷新。"
  }
  return "暂时无法连接平台服务，请检查网络后重试。"
}
export function resourceLink(item: Audit, permissions: string[]) {
  if (!item.clusterId) return undefined
  const p = new URLSearchParams({ cluster: item.clusterId })
  if (
    item.action.endsWith(".tres.update") &&
    permissions.includes("tres.read")
  ) {
    const id = item.changes.find((v) => v.field === "quotaTargetId")?.after
    if (id) {
      p.set("target", item.targetType === "slurm_qos" ? "qos" : "association")
      p.set("item", id)
      p.set("operation", item.operationId)
      return `/tres?${p}`
    }
  }
  if (
    item.targetType === "slurm_qos" &&
    permissions.includes("qos.read") &&
    permissions.includes("associations.read")
  ) {
    p.set("item", item.targetId)
    if (permissions.includes("platform.qos.read"))
      p.set("operation", item.operationId)
    return `/policies?${p}`
  }
  if (item.targetType === "job" && permissions.includes("jobs.read")) {
    p.set("job", item.targetId)
    return `/jobs?${p}`
  }
  if (item.targetType === "node" && permissions.includes("nodes.read")) {
    p.set("node", item.targetId)
    return `/nodes?${p}`
  }
  if (
    ["slurm_account", "slurm_user", "association"].includes(item.targetType)
  ) {
    const permission =
      item.targetType === "slurm_account"
        ? "accounts.read"
        : item.targetType === "slurm_user"
          ? "slurm_users.read"
          : "associations.read"
    if (permissions.includes(permission)) {
      p.set(
        "tab",
        item.targetType === "slurm_account"
          ? "accounts"
          : item.targetType === "slurm_user"
            ? "slurm-users"
            : "associations"
      )
      p.set("operation", item.operationId)
      if (item.targetType !== "association") p.set("item", item.targetId)
      return `/accounts?${p}`
    }
  }
  return undefined
}
