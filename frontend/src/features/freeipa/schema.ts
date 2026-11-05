import { z } from "zod"
export const infoSchema = z.object({
  configured: z.boolean(),
  domain: z.string(),
  realm: z.string(),
})
export const directoryUserSchema = z.object({
  username: z.string(),
  firstName: z.string(),
  lastName: z.string(),
  email: z.string(),
  uid: z.string(),
  gid: z.string(),
  enabled: z.boolean(),
  passwordExpired: z.boolean(),
  accountExpired: z.boolean(),
  groups: z.array(z.string()),
  version: z.string(),
})
export const directoryGroupSchema = z.object({
  name: z.string(),
  description: z.string(),
  gid: z.string(),
  members: z.array(z.string()),
  version: z.string(),
})
export const userPageSchema = z.object({
  items: z.array(directoryUserSchema),
  totalItems: z.number(),
  page: z.number(),
  perPage: z.number(),
})
export const groupPageSchema = z.object({
  items: z.array(directoryGroupSchema),
  totalItems: z.number(),
  page: z.number(),
  perPage: z.number(),
})
export const mappingSchema = z.object({
  id: z.string(),
  group: z.string(),
  roleCode: z.string(),
  roleName: z.string(),
  clusterId: z.string(),
  updatedAt: z.string(),
})
export const mappingsSchema = z.object({ items: z.array(mappingSchema) })
export const operationSchema = z.object({
  operationId: z.string(),
  action: z.string(),
  target: z.string(),
  status: z.string(),
  message: z.string(),
  updatedAt: z.string(),
})
export const operationsSchema = z.object({ items: z.array(operationSchema) })
export type DirectoryUser = z.infer<typeof directoryUserSchema>
export type DirectoryGroup = z.infer<typeof directoryGroupSchema>
export type Mapping = z.infer<typeof mappingSchema>
export type Operation = z.infer<typeof operationSchema>
export type Draft = {
  action: string
  target: string
  member: string
  firstName: string
  lastName: string
  email: string
  description: string
  operationId: string
  version: string
  confirmation: string
  reason: string
}
export function draft(action: string, target = "", version = ""): Draft {
  return {
    action,
    target,
    version,
    member: "",
    firstName: "",
    lastName: "",
    email: "",
    description: "",
    operationId: crypto.randomUUID(),
    confirmation: "",
    reason: "",
  }
}
export const actionNames: Record<string, string> = {
  "user.create": "创建目录用户",
  "user.enable": "启用目录用户",
  "user.disable": "停用目录用户",
  "group.create": "创建目录组",
  "group.add-member": "添加组成员",
  "group.remove-member": "移除组成员",
}
