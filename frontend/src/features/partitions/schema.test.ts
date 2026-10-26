import { expect, it } from "vitest"
import { limit, memoryLimit, partitionTime, stateHelp } from "./schema"
import { resolveStatus } from "@/components/slurm/status"
it("区分内存零、普通零、未设置及无限时限", () => {
  expect(memoryLimit({ value: 0, infinite: false })).toBe("不限（配置值 0）")
  expect(limit({ value: 0, infinite: false })).toBe("0")
  expect(memoryLimit({ value: null, infinite: false })).toBe("未设置/未返回")
  expect(partitionTime({ value: null, infinite: true })).toBe("不限时")
})
it("分区 DRAIN 拒绝新提交而允许已排队作业调度", () => {
  expect(stateHelp(["DRAIN"])).toContain("已排队作业仍可能被调度")
  expect(resolveStatus("DRAIN", "partition")).not.toEqual(
    resolveStatus("DRAIN", "node")
  )
})
