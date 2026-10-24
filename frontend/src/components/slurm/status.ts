export type StatusTone = "success" | "info" | "warning" | "danger" | "neutral"
const jobs: Record<string, { label: string; tone: StatusTone }> = {
  RUNNING: { label: "运行中", tone: "success" },
  PENDING: { label: "排队中", tone: "warning" },
  COMPLETING: { label: "收尾中", tone: "info" },
  COMPLETED: { label: "已完成", tone: "info" },
  FAILED: { label: "失败", tone: "danger" },
  TIMEOUT: { label: "超时", tone: "danger" },
  OUT_OF_MEMORY: { label: "内存不足", tone: "danger" },
  NODE_FAIL: { label: "节点故障", tone: "danger" },
  CANCELLED: { label: "已取消", tone: "neutral" },
  SUSPENDED: { label: "已挂起", tone: "warning" },
  PREEMPTED: { label: "被抢占", tone: "warning" },
}
const nodes: Record<string, { label: string; tone: StatusTone }> = {
  IDLE: { label: "空闲", tone: "info" },
  ALLOCATED: { label: "已分配", tone: "success" },
  MIXED: { label: "部分分配", tone: "success" },
  DRAIN: { label: "排空", tone: "warning" },
  DRAINING: { label: "排空中", tone: "warning" },
  DRAINED: { label: "已排空", tone: "warning" },
  DOWN: { label: "离线", tone: "danger" },
  UNKNOWN: { label: "未知", tone: "neutral" },
}
export function resolveStatus(state: string, kind: "job" | "node" = "job") {
  const code = state.trim().toUpperCase()
  return {
    code: code || "UNKNOWN",
    ...((kind === "job" ? jobs : nodes)[code] ?? {
      label: "未知状态",
      tone: "neutral" as const,
    }),
  }
}
