import type { Job, Operation } from "./schema"
const number = (value: number | null) => ({ value, infinite: false })
export function fixtureJob(): Job {
  return {
    id: "42",
    name: "console-qa",
    user: "alice",
    userId: 1001,
    account: "research",
    partition: "normal",
    qos: "normal",
    states: ["PENDING"],
    reason: "JobHeldUser",
    nodes: "",
    cpus: number(2),
    nodeCount: number(1),
    timeLimitMinutes: number(3),
    submitTime: number(1700000000),
    startTime: number(0),
    endTime: number(0),
    arrayJobId: number(0),
    arrayTaskId: number(null),
    arrayTaskString: "",
    hetJobId: number(0),
    hetJobOffset: number(0),
    hetJobIdSet: "",
    restartCount: 0,
    priority: number(0),
    memoryPerNodeMiB: number(128),
    memoryPerCpuMiB: number(null),
  }
}
export function fixturePrepared() {
  return {
    allowed: true,
    message: "请核对最新归属",
    job: fixtureJob(),
    identity: { username: "alice", uid: 1001, scope: "own" },
    proof: "signed-proof",
    operationId: "test-operation",
    expiresAt: "2026-10-25T15:00:00Z",
  }
}
export function fixtureOperation(
  status: Operation["status"] = "confirmed"
): Operation {
  return {
    operationId: "test-operation",
    jobId: "42",
    status,
    message: "已核实作业当前为 CANCELLED。",
    reason: "功能验证",
    states: ["CANCELLED"],
    createdAt: "2026-10-25T14:00:00Z",
    updatedAt: "2026-10-25T14:00:01Z",
  }
}
