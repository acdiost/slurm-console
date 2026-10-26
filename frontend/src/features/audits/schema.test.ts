import { expect, it } from "vitest"
import { auditSchema, resourceLink, localInput, dateTime } from "./schema"
import { fixtureAudit } from "./fixtures.test-support"
it("资源跳转仍需各资源读取权限，审计权限不隐含读取", () => {
  const item = fixtureAudit()
  expect(resourceLink(item, ["platform.audit.read"])).toBeUndefined()
  expect(resourceLink(item, ["nodes.read"])).toBe(
    "/nodes?cluster=cluster-old&node=node1"
  )
})
it("不把额外持久字段暴露给页面和导出", () => {
  const item = auditSchema.parse({
    ...fixtureAudit(),
    input_hash: "private",
    fingerprint: "private",
  })
  expect(item).not.toHaveProperty("input_hash")
  expect(item).not.toHaveProperty("fingerprint")
})
it("历史 PB UTC 时间可显示，时区转换往返保持时间", () => {
  const value = "2026-10-20T01:00:00Z"
  expect(new Date(localInput(value)).toISOString()).toBe(
    "2026-10-20T01:00:00.000Z"
  )
  expect(dateTime("2026-10-20 01:00:00.000Z")).not.toBe("—")
})
