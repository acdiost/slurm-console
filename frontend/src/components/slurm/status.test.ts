import { describe, expect, it } from "vitest"
import { resolveStatus } from "./status"

describe("Slurm 状态边界", () => {
  it("区分资源分配、等待、失败和取消", () => {
    expect(resolveStatus("RUNNING").tone).toBe("success")
    expect(resolveStatus("PENDING").tone).toBe("warning")
    expect(resolveStatus("FAILED").tone).toBe("danger")
    expect(resolveStatus("CANCELLED").tone).toBe("neutral")
    expect(resolveStatus("DRAIN", "node").tone).toBe("warning")
    expect(resolveStatus("DOWN", "node").tone).toBe("danger")
  })
  it("不会将未知或复合状态错误地显示为正常", () => {
    expect(resolveStatus("FUTURE_STATE")).toMatchObject({
      code: "FUTURE_STATE",
      tone: "neutral",
    })
    expect(resolveStatus("IDLE+DRAIN", "node").tone).toBe("neutral")
    expect(resolveStatus("").code).toBe("UNKNOWN")
  })
})
