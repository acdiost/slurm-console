import type { Action, Node, Operation } from "./schema"
const number = (value: number | null) => ({ value, infinite: false })
export function fixtureNode(name = "node1"): Node {
  return {
    name,
    hostname: name,
    address: "10.0.0.1",
    version: "25.11.8",
    architecture: "x86_64",
    operatingSystem: "Linux",
    states: ["MIXED"],
    cpus: 8,
    effectiveCpus: 8,
    allocatedCpus: 2,
    memoryMiB: 8192,
    allocatedMemoryMiB: 512,
    freeMemoryMiB: number(1024),
    partitions: ["normal"],
    reason: "",
    reasonUser: "",
    comment: "待安排维护",
    reservation: "",
    features: ["avx2"],
    activeFeatures: ["avx2"],
    bootTime: number(1700000000),
    slurmdStartTime: number(1700000001),
    reasonChangedAt: number(null),
    resumeAfter: number(null),
    gres: "gpu:a100:2",
    gresUsed: "gpu:a100:1(IDX:0)",
    resources: {
      cpus: { total: 8, allocated: 2, idle: 6, unavailable: 0 },
      memoryMiB: { total: 8192, allocated: 512, idle: 7680, unavailable: 0 },
      gpus: {
        total: 2,
        allocated: 1,
        idle: 1,
        unavailable: 0,
        unknownNodes: 0,
      },
    },
  }
}
export function fixtureContext(name = "node1", action: Action = "drain") {
  const n = fixtureNode(name)
  if (action === "resume") n.states = ["IDLE", "DRAIN"]
  return {
    allowed: true,
    message: "请核对节点状态",
    node: n,
    action,
    proof: `proof-${name}-${action}`,
    operationId: `operation-${name}-${action}`,
    expiresAt: "2026-10-25T17:00:00Z",
  }
}
export function fixtureOperation(
  name = "node1",
  status: Operation["status"] = "confirmed",
  action: Action = "drain"
): Operation {
  return {
    operationId: `operation-${name}-${action}`,
    nodeName: name,
    action,
    status,
    message:
      status === "confirmed"
        ? "已核实节点状态。"
        : "请核对节点状态，勿重复提交。",
    reason: "维护验证",
    states: action === "drain" ? ["MIXED", "DRAIN"] : ["MIXED"],
    createdAt: "2026-10-25T16:00:00Z",
    updatedAt: "2026-10-25T16:00:01Z",
  }
}
