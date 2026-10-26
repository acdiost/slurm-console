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
  NOT_RESPONDING: { label: "等待响应", tone: "warning" },
  COMPLETING: { label: "作业收尾中", tone: "info" },
  MAINTENANCE: { label: "维护中", tone: "warning" },
  RESERVED: { label: "已预留", tone: "info" },
  FAIL: { label: "故障", tone: "danger" },
  INVALID_REG: { label: "注册异常", tone: "danger" },
  FUTURE: { label: "待配置", tone: "neutral" },
  POWERED_DOWN: { label: "已下电", tone: "neutral" },
  POWERING_UP: { label: "上电中", tone: "warning" },
  POWERING_DOWN: { label: "下电中", tone: "warning" },
  REBOOT_REQUESTED: { label: "待重启", tone: "warning" },
  REBOOT_ISSUED: { label: "重启中", tone: "warning" },
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
