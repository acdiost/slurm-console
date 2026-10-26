import type { Audit } from "./schema"
export function fixtureAudit(operationId = "op-one"): Audit {
  return {
    operationId,
    actorId: "actor-old",
    actorLabel: "历史操作人",
    actorSource: "platform",
    clusterId: "cluster-old",
    clusterName: "历史科研集群",
    clusterSlug: "historic",
    clusterRevision: 3,
    action: "nodes.drain",
    targetType: "node",
    targetId: "node1",
    requestId: "request-one",
    status: "unknown",
    recordedStatus: "processing",
    message:
      "记录尚未完成，可能仍在处理或执行已中断；请核对目标状态，勿重复提交。",
    reason: "安排维护 <script>not executable</script>",
    states: ["MIXED", "DRAIN"],
    changes: [],
    createdAt: "2026-10-20 01:00:00.000Z",
    updatedAt: "2026-10-20 01:00:01.000Z",
  }
}
