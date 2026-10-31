import { z } from "zod"
import { ClientResponseError } from "pocketbase"
import rawCatalog from "../../../../backend/internal/savedfilters/catalog.json"
import type { PlatformUser } from "@/stores/auth"
export type Conditions = Record<string, string>
type Field = {
  type: string
  max?: number
  maxRunes?: number
  values?: string[]
}
export type Definition = {
  label: string
  scoped: boolean
  permissions: string[][]
  fields: Record<string, Field>
}
export const catalog: Record<string, Definition> = Object.assign(
  Object.create(null),
  rawCatalog
)
export const savedFilterSchema = z.object({
  id: z.string(),
  module: z.string(),
  clusterId: z.string(),
  name: z.string(),
  conditions: z.record(z.string(), z.string()),
  isDefault: z.boolean(),
  revision: z.number().int().positive(),
  usable: z.boolean(),
  compatible: z.boolean(),
  updatedAt: z.string(),
})
export const savedFiltersSchema = z.object({
  items: z.array(savedFilterSchema).max(10),
  maxPerScope: z.number(),
  maxPerUser: z.number(),
})
export type SavedFilter = z.infer<typeof savedFilterSchema>
const contextKeys = ["cluster", "clusterPage", "clusterQ"]
export function explicitQuery(params: URLSearchParams, scoped: boolean) {
  return [...params.keys()].some((k) => !scoped || !contextKeys.includes(k))
}
export function canEnter(
  user: PlatformUser,
  module: string,
  clusterId: string
) {
  const d = catalog[module]
  if (!d) return false
  const a = user.access
  return d.permissions.some((group) =>
    group.every(
      (p) =>
        a.platformPermissions.includes(p) ||
        a.clusters.some(
          (c) =>
            (!d.scoped || c.cluster.id === clusterId) &&
            c.permissions.includes(p)
        )
    )
  )
}
export function conditionsFromURL(
  module: string,
  params: URLSearchParams
): Conditions {
  const fields = catalog[module]?.fields || {},
    out: Conditions = {}
  for (const k of Object.keys(fields))
    if (params.get(k)) out[k] = params.get(k)!
  if (
    (module === "accounts" && out.tab !== "associations") ||
    (module === "policies" && out.tab !== "fairshare")
  )
    for (const k of ["account", "user", "partition", "kind"]) delete out[k]
  return out
}
export function validConditions(module: string, input: Conditions): boolean {
  const d = catalog[module]
  if (!d || Object.keys(input).length > 20) return false
  for (const [k, v] of Object.entries(input)) {
    const f = Object.hasOwn(d.fields, k) ? d.fields[k] : undefined
    if (
      !f ||
      typeof v !== "string" ||
      Array.from(v).some((c) => {
        const n = c.codePointAt(0)!
        return n < 32 || (n >= 127 && n <= 159)
      }) ||
      (f.max && new TextEncoder().encode(v).length > f.max) ||
      (f.maxRunes && Array.from(v).length > f.maxRunes)
    )
      return false
    if (!v) continue
    if (f.type === "enum" && !f.values?.includes(v)) return false
    if (f.type === "state" && !/^[A-Z][A-Z0-9_]{0,63}$/.test(v)) return false
    if (
      f.type === "name" &&
      (!/^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$/.test(v) ||
        v.toUpperCase() === "ALL")
    )
      return false
    if (
      f.type === "jobId" &&
      (!/^[1-9][0-9]{0,9}$/.test(v) || Number(v) > 4294967295)
    )
      return false
    if (
      f.type === "unix" &&
      (!/^[1-9][0-9]*$/.test(v) ||
        !Number.isSafeInteger(Number(v)) ||
        Number(v) > 253402300799)
    )
      return false
    if (
      f.type === "date" &&
      (!/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(v) ||
        !Number.isFinite(Date.parse(v)))
    )
      return false
  }
  if (
    module === "history" &&
    (!!input.start !== !!input.end ||
      (input.start &&
        (Number(input.end) <= Number(input.start) ||
          Number(input.end) - Number(input.start) > 31 * 86400)))
  )
    return false
  if (
    module === "audits" &&
    input.from &&
    input.to &&
    Date.parse(input.to) <= Date.parse(input.from)
  )
    return false
  if (module === "accounts" || module === "policies") {
    const association =
      module === "accounts"
        ? input.tab === "associations"
        : input.tab === "fairshare"
    if (
      !association &&
      ["account", "user", "partition", "kind"].some((k) => !!input[k])
    )
      return false
    if (input.sort) {
      const choices =
        module === "accounts"
          ? association
            ? ["id", "account", "user", "partition"]
            : ["name"]
          : association
            ? ["account", "user", "factor", "id"]
            : ["name", "priority"]
      if (!choices.includes(input.sort)) return false
    }
  }
  return new TextEncoder().encode(JSON.stringify(input)).length <= 4096
}
export function appliedSearch(
  module: string,
  params: URLSearchParams,
  conditions: Conditions,
  mode = "custom"
) {
  const d = catalog[module],
    next = new URLSearchParams()
  if (d.scoped)
    for (const k of contextKeys) if (params.has(k)) next.set(k, params.get(k)!)
  if (!validConditions(module, conditions))
    throw new Error("Saved conditions are incompatible")
  for (const [k, v] of Object.entries(conditions)) if (v) next.set(k, v)
  next.set("filterMode", mode)
  return next
}
export function historyWindow(params: URLSearchParams, now = Date.now()) {
  if (params.has("start") || params.has("end")) return params
  const end = Math.floor(now / 60000) * 60,
    next = new URLSearchParams(params)
  next.set("start", String(end - 86400))
  next.set("end", String(end))
  return next
}
export function errorMessage(e: unknown) {
  if (e instanceof ClientResponseError) {
    if (e.status === 409)
      return "方案已变化、名称重复或达到数量限制，请刷新列表后重试。"
    if (e.status === 403) return "当前没有该模块或集群的方案访问权限。"
    return (
      e.response?.error?.message || "筛选方案暂不可用，当前使用地址中的条件。"
    )
  }
  return "筛选方案暂不可用，当前使用地址中的条件。"
}
