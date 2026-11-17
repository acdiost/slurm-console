import { z } from "zod"
import { connectionFormSchema } from "@/features/clusters/schema"

export const pamStatusSchema = z.object({
  revision: z.number().int(),
  configured: z.boolean(),
  available: z.boolean(),
  supported: z.boolean(),
  helperInstalled: z.boolean(),
  policyInstalled: z.boolean(),
  message: z.string(),
})
export type PAMStatus = z.infer<typeof pamStatusSchema>
export const setupStatusSchema = z.object({
  needsSetup: z.boolean(),
  pam: pamStatusSchema,
  slurm: z.object({
    configured: z.boolean(),
    profileCount: z.number(),
    clusterCount: z.number(),
  }),
})
export type SetupStatus = z.infer<typeof setupStatusSchema>

export const slurmSetupSchema = z
  .object({
    name: connectionFormSchema.shape.name,
    slug: connectionFormSchema.shape.slug,
    authenticationMethod: connectionFormSchema.shape.authenticationMethod,
    endpoint: z.string().trim().min(1, "请输入 slurmrestd 地址"),
    authType: z.enum(["local", "jwt"]),
    username: z.string().trim().max(63, "用户名最多 63 个字符"),
    token: z.string().max(8192, "JWT 内容过长"),
    caPem: z.string().max(8192, "CA PEM 内容过长"),
    apiVersion: z.enum([
      "auto",
      "v0.0.44",
      "v0.0.43",
      "v0.0.42",
      "v0.0.41",
      "v0.0.40",
    ]),
    timeoutSeconds: connectionFormSchema.shape.timeoutSeconds,
  })
  .superRefine((values, ctx) => {
    const issue = (
      path: "endpoint" | "username" | "token" | "caPem",
      message: string
    ) => ctx.addIssue({ code: "custom", path: [path], message })
    if (values.authType === "local") {
      if (
        !/^unix:\/\/\/[^?#\s]+$/.test(values.endpoint) ||
        values.endpoint
          .slice(7)
          .split("/")
          .some((part) => part === "." || part === "..")
      )
        issue(
          "endpoint",
          "请输入 Unix socket 绝对路径，例如 unix:///run/slurmrestd/slurmrestd.sock"
        )
    } else {
      try {
        const url = new URL(values.endpoint)
        if (
          url.protocol !== "https:" ||
          url.username ||
          url.password ||
          url.search ||
          url.hash ||
          url.pathname !== "/"
        )
          issue(
            "endpoint",
            "请输入 HTTPS 源地址，不含路径、账号、查询参数或片段"
          )
      } catch {
        issue("endpoint", "请输入有效的 HTTPS 地址")
      }
      if (!/^[a-z][a-z0-9_-]{0,62}$/.test(values.username))
        issue(
          "username",
          "Slurm 用户名需以小写字母开头，只含小写字母、数字、下划线或连字符"
        )
      if (!values.token.trim()) issue("token", "请输入有效的 Slurm JWT")
      if (
        values.caPem.trim() &&
        !values.caPem.includes("-----BEGIN CERTIFICATE-----")
      )
        issue("caPem", "请输入 PEM 格式的 CA 证书，或留空使用系统信任库")
    }
  })
export type SlurmSetupValues = z.infer<typeof slurmSetupSchema>
