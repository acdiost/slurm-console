import { fixtureOverview } from "@/features/overview/fixtures.test-support"
import type { FleetItem } from "./schema"
export function fixtureFleetItem(id = "alpha", name = "科研集群"): FleetItem {
  const o = fixtureOverview()
  return {
    cluster: { ...o.cluster, id, name },
    revision: 1,
    configuration: "enabled",
    state: "connected",
    permissions: [
      "clusters.read",
      "jobs.read",
      "nodes.read",
      "partitions.read",
    ],
    health: o.health,
    nodes: o.nodes,
    jobs: o.jobs,
  }
}
export function fixtureFleetPage() {
  return {
    items: [fixtureFleetItem()],
    page: 1,
    perPage: 12,
    totalItems: 25,
    generatedAt: "2026-10-27T00:00:00Z",
    canManage: false,
    pageSummary: {
      connected: 1,
      controllerDown: 0,
      unavailable: 0,
      disabled: 0,
      unconfigured: 0,
      pending: 0,
      partial: 0,
      needsAttention: 0,
    },
  }
}
