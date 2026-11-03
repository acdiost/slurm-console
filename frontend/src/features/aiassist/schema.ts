import { z } from "zod"
const destination = z.object({
  label: z.string(),
  endpoint: z.string(),
  model: z.string(),
})
export const statusSchema = z.discriminatedUnion("enabled", [
  z.object({ enabled: z.literal(false) }),
  z.object({ enabled: z.literal(true), destination }),
])
export const previewSchema = z.object({
  id: z.string(),
  digest: z.string(),
  expiresAt: z.string(),
  observedAt: z.string(),
  destination,
  request: z
    .object({
      messages: z.array(z.object({ role: z.string(), content: z.string() })),
    })
    .passthrough(),
})
export type Preview = z.infer<typeof previewSchema>
const cited = z.object({ text: z.string(), evidenceIds: z.array(z.string()) })
export const explanationSchema = z.object({
  operationId: z.string(),
  observedAt: z.string(),
  destination,
  answer: z.object({
    summary: z.string(),
    observations: z.array(cited),
    checks: z.array(cited.extend({ audience: z.enum(["user", "admin"]) })),
    limitations: z.array(z.string()),
  }),
})
export type Explanation = z.infer<typeof explanationSchema>
