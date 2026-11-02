import { z } from "zod"
import { viewSchema, type BulkAction } from "@/features/bulk/schema"
export const actions: BulkAction[] = [
  "jobs.cancel",
  "nodes.drain",
  "nodes.resume",
]
export const templateSchema = z.object({
  id: z.string(),
  clusterId: z.string(),
  name: z.string(),
  action: z.enum(["jobs.cancel", "nodes.drain", "nodes.resume"]),
  description: z.string(),
  defaultReason: z.string(),
  checklist: z.array(z.string()).max(8),
  revision: z.number().int().positive(),
  updatedAt: z.string(),
})
export const templatesSchema = z.object({
  items: z.array(templateSchema).max(20),
  maxPerCluster: z.literal(20),
  maxPerUser: z.literal(100),
})
export const preparationSchema = z.object({
  template: templateSchema,
  prepared: viewSchema,
})
export type OperationTemplate = z.infer<typeof templateSchema>
export function targets(action: BulkAction, input: string) {
  const values = input
    .trim()
    .split(/[\s,]+/)
    .filter(Boolean)
  const valid =
    values.length > 0 &&
    values.length <= 10 &&
    new Set(values).size === values.length &&
    values.every((v) =>
      action === "jobs.cancel"
        ? /^[1-9][0-9]{0,9}$/.test(v) && Number(v) <= 4294967295
        : /^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$/.test(v) &&
          !["ALL", "all"].includes(v)
    )
  return valid ? values : null
}
