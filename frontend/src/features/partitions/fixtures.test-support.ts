import { partitionSchema } from "./schema"
const unset = { value: null, infinite: false }
export function fixturePartition(name = "normal") {
  const strings = Object.fromEntries(
    "allowedAccounts deniedAccounts allowedGroups allowedQos deniedQos assignedQos allowedAllocationNodes alternate configuredTres billingWeights nodeSets topology defaultJob"
      .split(" ")
      .map((k) => [k, ""])
  )
  const numbers = Object.fromEntries(
    "defaultTimeMinutes defaultMemoryPerCpuMiB maxMemoryPerCpuMiB maxMemoryPerNodeMiB minNodes maxNodes maxCpusPerNode maxCpusPerSocket priorityTier priorityJobFactor graceTimeSeconds overTimeLimitMinutes oversubscribeJobs"
      .split(" ")
      .map((k) => [k, unset])
  )
  return partitionSchema.parse({
    ...strings,
    ...numbers,
    name,
    states: ["UP"],
    nodes: "node[1-2]",
    nodeCount: 2,
    cpus: 8,
    defaultMemoryPerNodeMiB: { value: 0, infinite: false },
    maxTimeMinutes: { value: null, infinite: true },
    selectType: [],
    oversubscribeFlags: [],
  })
}
export function fixtureDetail() {
  return {
    item: fixturePartition(),
    fetchedAt: "2026-10-26T08:00:00Z",
    cached: false,
    revision: 1,
    resources: {
      status: "forbidden",
      code: "FORBIDDEN",
      message: "未授予该项读取权限。",
      fetchedAt: "",
      cached: false,
      item: null,
    },
  }
}
