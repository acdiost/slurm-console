import { z } from "zod"
export const identitySchema = z.object({
  name: z.string().trim().min(2).max(64),
  description: z.string().trim().max(240),
  loginNotice: z.string().trim().max(1000),
  supportContact: z.string().trim().max(160),
})
export type Identity = z.infer<typeof identitySchema>
export const defaults: Identity = {
  name: "Slurm Console",
  description:
    "连接集群、洞察资源、管理调度。\n为团队提供清晰、一致的算力运维工作空间。",
  loginNotice: "",
  supportContact: "",
}
export const settingsSchema = identitySchema.extend({
  revision: z.number().int().positive(),
  updatedAt: z.string(),
})
export const aiConfigSchema = z.object({
  enabled: z.boolean(),
  label: z.string().trim().min(1).max(100),
  endpoint: z.string().trim().max(2048),
  model: z.string().trim().max(120),
  clusterIds: z.array(z.string().regex(/^[a-z0-9]{15}$/)).max(100),
  timeoutSeconds: z.number().int().min(5).max(60),
  maxOutputTokens: z.number().int().min(512).max(4096),
  tokenParameter: z.enum(["max_completion_tokens", "max_tokens"]),
})
export const aiSettingsSchema = aiConfigSchema.extend({
  revision: z.number().int().positive(),
  source: z.enum(["server", "platform"]),
  keyConfigured: z.boolean(),
  caConfigured: z.boolean(),
})
export type AISettings = z.infer<typeof aiSettingsSchema>
export const runtimeSchema = z.object({
  runtime: z.object({
    startedAt: z.string(),
    goVersion: z.string(),
    os: z.string(),
    arch: z.string(),
    revision: z.string(),
    modified: z.boolean(),
    runAsRoot: z.boolean(),
    database: z.string(),
  }),
  integrations: z.array(
    z.object({
      id: z.string(),
      name: z.string(),
      configured: z.boolean(),
      count: z.number(),
    })
  ),
})
export const platformUserSchema = z.object({
  id: z.string(),
  email: z.string(),
  name: z.string(),
  username: z.string(),
  authSource: z.enum(["local", "pam", "ldap", "freeipa"]),
  active: z.boolean(),
  revision: z.number().int().nonnegative(),
  updatedAt: z.string(),
})
export type PlatformUser = z.infer<typeof platformUserSchema>
export const usersSchema = z.object({
  items: z.array(platformUserSchema),
  page: z.number(),
  perPage: z.number(),
  totalItems: z.number(),
})
export const catalogSchema = z.object({
  roles: z.array(
    z.object({
      code: z.string(),
      name: z.string(),
      scope: z.enum(["platform", "cluster"]),
      permissions: z.array(z.object({ code: z.string(), name: z.string() })),
    })
  ),
  clusters: z.array(z.object({ id: z.string(), name: z.string() })),
})
export const userRolesSchema = z.object({
  user: platformUserSchema,
  roles: z.array(
    z.object({
      roleCode: z.string(),
      roleName: z.string(),
      scope: z.enum(["platform", "cluster"]),
      clusterId: z.string(),
      clusterName: z.string(),
    })
  ),
})
