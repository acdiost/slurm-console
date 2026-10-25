import type { Overview } from "./schema"
export const cluster = {
  id: "alpha",
  name: "科研集群",
  slug: "research",
  description: "生产计算环境",
}
const source = <T>(item: T) => ({
  status: "ok" as const,
  code: "",
  message: "",
  fetchedAt: "2026-10-25T00:00:00Z",
  cached: false,
  item,
})
export function fixtureOverview(): Overview {
  return {
    cluster,
    revision: 1,
    generatedAt: "2026-10-25T00:00:00Z",
    health: source({
      status: "connected",
      serverVersion: "25.11.8",
      apiVersion: "v0.0.43",
      cluster: "production",
      controllers: [{ hostname: "controller", state: "UP", role: "primary" }],
      capabilities: ["jobs.read", "nodes.read", "partitions.read"],
    }),
    nodes: source({
      total: 2,
      idle: 1,
      allocated: 0,
      mixed: 0,
      unavailable: 1,
      cpus: { total: 16, allocated: 0, idle: 8, unavailable: 8 },
      memoryMiB: {
        total: 32768,
        allocated: 0,
        idle: 16384,
        unavailable: 16384,
      },
      gpus: {
        total: 0,
        allocated: 0,
        idle: 0,
        unavailable: 0,
        unknownNodes: 0,
      },
      states: [
        { state: "IDLE", count: 1 },
        { state: "DOWN", count: 1 },
      ],
      issues: [{ name: "node-down", states: ["DOWN"], reason: "计划维护" }],
    }),
    jobs: source({
      total: 8,
      running: 3,
      pending: 2,
      failed: 1,
      completing: 0,
      completed: 1,
      cancelled: 1,
      other: 0,
      states: [],
    }),
    partitions: source({
      total: 1,
      up: 1,
      items: [
        {
          name: "normal",
          states: ["UP"],
          nodes: "node[1-2]",
          nodeCount: 2,
          cpus: 16,
          defaultTimeMinutes: { value: null, infinite: false },
          maxTimeMinutes: { value: null, infinite: true },
        },
      ],
    }),
  }
}
