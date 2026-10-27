import { z } from "zod"
import { ClientResponseError } from "pocketbase"
export const linuxUserSchema = z.object({
  name: z.string(),
  uid: z.number().int().nonnegative(),
  gid: z.number().int().nonnegative(),
  comment: z.string(),
  home: z.string(),
  shell: z.string(),
  source: z.enum(["local", "nss", "unknown"]),
  kind: z.enum(["privileged", "system", "regular", "unknown"]),
})
export type LinuxUser = z.infer<typeof linuxUserSchema>
const metadata = {
  fetchedAt: z.string(),
  cached: z.boolean(),
  sourceLabel: z.string(),
  enumeration: z.enum(["nss", "lookup"]),
  uidMin: z.number().int().nonnegative().nullable(),
  revision: z.number(),
}
export const linuxUserPageSchema = z.object({
  items: z.array(linuxUserSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
  ...metadata,
})
export const linuxUserDetailSchema = z.object({
  item: linuxUserSchema,
  ...metadata,
})
export const sources = {
  local: "本地匹配",
  nss: "NSS 非本地匹配",
  unknown: "来源未确认",
}
export const kinds = {
  privileged: "UID 0",
  system: "系统范围",
  regular: "普通范围",
  unknown: "范围未确认",
}
export function errorMessage(error: unknown) {
  if (error instanceof ClientResponseError) {
    if (error.status === 403) return "你没有此集群的 Linux 用户读取权限。"
    if (error.status === 404) return "用户不存在或对此来源不可见。"
    return (
      error.response?.error?.message || "Linux 用户查询未完成，请稍后重试。"
    )
  }
  return "暂时无法读取 Linux 用户，请检查网络后重试。"
}
