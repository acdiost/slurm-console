import { z } from "zod"
import { numberSchema } from "@/features/jobs/schema"
const n = z.number().nonnegative().nullable()
export const diagnosisSchema = z.object({
  revision: z.number(),
  cached: z.boolean(),
  report: z.object({
    version: z.literal(1),
    source: z.enum(["controller", "accounting"]),
    observedAt: z.string(),
    generatedAt: z.string(),
    identity: z.object({
      jobId: z.string(),
      recordKey: z.string(),
      name: z.string(),
      user: z.string(),
      account: z.string(),
      partition: z.string(),
      qos: z.string(),
      nodes: z.string(),
      states: z.array(z.string()),
      arrayJobId: numberSchema,
      arrayTaskId: numberSchema,
      hetJobId: numberSchema,
      hetJobOffset: numberSchema,
    }),
    category: z.string(),
    headline: z.string(),
    timeline: z.object({
      sinceSubmitSeconds: n,
      submitToStartSeconds: n,
      observedRunSeconds: n,
      reportedElapsedSeconds: n,
    }),
    metrics: z.object({
      allocatedCpus: n,
      totalCpuSeconds: n,
      cpuAccountingPercent: z.number().min(0).max(100).nullable(),
    }),
    coverage: z.object({
      totalSteps: z.number(),
      abnormalSteps: z.number(),
      displayedSteps: z.number(),
    }),
    evidence: z
      .array(
        z.object({
          id: z.string(),
          field: z.string(),
          label: z.string(),
          value: z.string(),
        })
      )
      .max(64),
    findings: z
      .array(
        z.object({
          code: z.string(),
          category: z.string(),
          severity: z.enum(["info", "warning"]),
          confidence: z.enum(["high", "medium", "low"]),
          kind: z.enum(["fact", "inference"]),
          title: z.string(),
          explanation: z.string(),
          evidenceIds: z.array(z.string()),
          checks: z.array(
            z.object({ audience: z.enum(["user", "admin"]), text: z.string() })
          ),
        })
      )
      .max(32),
    limitations: z.array(z.string()).max(16),
  }),
})
export type Diagnosis = z.infer<typeof diagnosisSchema>
