import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, act, cleanup } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { AIAssistance } from "./panel"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const destination = {
  label: "隔离模型",
  endpoint: "http://127.0.0.1:18093/v1/chat/completions",
  model: "fixture",
}
const preview = {
  id: "preview",
  digest: "a".repeat(64),
  expiresAt: new Date(Date.now() + 120000).toISOString(),
  observedAt: "2026-11-02T00:00:00Z",
  destination,
  request: {
    model: "fixture",
    messages: [
      { role: "system", content: "只读解释" },
      {
        role: "user",
        content: JSON.stringify({
          facts: [{ id: "states", label: "来源状态", value: "PENDING" }],
        }),
      },
    ],
  },
}
const answer = {
  operationId: "operation",
  observedAt: preview.observedAt,
  destination,
  answer: {
    summary: "等待状态的具体原因仍需核实。",
    observations: [{ text: "来源显示等待。", evidenceIds: ["states"] }],
    checks: [
      { audience: "admin", text: "人工核对资源。", evidenceIds: ["states"] },
    ],
    limitations: ["未提供日志。"],
  },
}
function role(permissions = ["ai.explain", "jobs.read"], id = "alice") {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id,
      email: `${id}@example.test`,
      name: id,
      username: id,
      authSource: "local",
      access: {
        platformPermissions: [],
        platformRoles: [],
        clusters: [{ cluster, roles: [], permissions }],
      },
    },
  })
}
function show() {
  return render(
    <QueryClientProvider client={client}>
      <AIAssistance
        clusterId="alpha"
        source="controller"
        previewPath="/clusters/alpha/jobs/42/diagnosis/ai-preview"
      />
    </QueryClientProvider>
  )
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  role()
  vi.mocked(api).mockReset()
})
afterEach(() => {
  cleanup()
  client.clear()
  useAuth.setState({ user: null, status: "anonymous" })
})
it("does not query AI for readers without transmission permission", () => {
  role(["jobs.read"])
  show()
  expect(api).not.toHaveBeenCalled()
  expect(screen.queryByLabelText("AI 运维辅助")).not.toBeInTheDocument()
})
it("keeps configured-off services read only without preparing or sending", async () => {
  vi.mocked(api).mockResolvedValue({ enabled: false })
  show()
  expect(await screen.findByText(/此集群尚未启用 AI 服务/)).toBeInTheDocument()
  expect(api).toHaveBeenCalledTimes(1)
  expect(
    screen.queryByRole("button", { name: "预览发送内容" })
  ).not.toBeInTheDocument()
})
it("requires reviewed payload, checkbox and exact confirmation for one explicit send", async () => {
  const u = userEvent.setup()
  vi.mocked(api)
    .mockResolvedValueOnce({ enabled: true, destination })
    .mockResolvedValueOnce(preview)
    .mockResolvedValueOnce(answer)
  show()
  await u.click(await screen.findByRole("button", { name: "预览发送内容" }))
  expect(await screen.findByLabelText("完整请求正文")).toHaveTextContent(
    "PENDING"
  )
  expect(api).toHaveBeenCalledTimes(2)
  const send = screen.getByRole("button", { name: "确认发送并生成建议" })
  expect(send).toBeDisabled()
  await u.click(screen.getByRole("checkbox"))
  expect(send).toBeDisabled()
  await u.type(screen.getByLabelText("输入 SEND AI 确认"), "SEND AI")
  await u.click(send)
  expect(await screen.findByLabelText("AI 生成建议")).toHaveTextContent(
    "未核实"
  )
  expect(api).toHaveBeenLastCalledWith(
    "/clusters/alpha/ai-assistance/preview/explain",
    {
      method: "POST",
      body: { confirmation: "SEND AI", digest: preview.digest },
    }
  )
  expect(api).toHaveBeenCalledTimes(3)
  expect(
    screen.queryByRole("button", { name: "确认发送并生成建议" })
  ).not.toBeInTheDocument()
  expect(screen.getByText("本次发送的证据（含引用标识）")).toBeInTheDocument()
  act(() => role(["jobs.read"]))
  expect(screen.queryByText(answer.answer.summary)).not.toBeInTheDocument()
})
it("cancels a preview without calling the provider", async () => {
  const u = userEvent.setup()
  vi.mocked(api)
    .mockResolvedValueOnce({ enabled: true, destination })
    .mockResolvedValueOnce(preview)
  show()
  await u.click(await screen.findByRole("button", { name: "预览发送内容" }))
  await u.click(await screen.findByRole("button", { name: "取消预览" }))
  expect(screen.queryByLabelText("完整请求正文")).not.toBeInTheDocument()
  expect(api).toHaveBeenCalledTimes(2)
})
it("discards uncertain results and never automatically resends", async () => {
  const u = userEvent.setup()
  vi.mocked(api)
    .mockResolvedValueOnce({ enabled: true, destination })
    .mockResolvedValueOnce(preview)
    .mockRejectedValueOnce(
      new ClientResponseError({
        status: 503,
        response: {
          error: { message: "请求可能已发送，结果未通过校验。" },
          requestId: "request-42",
        },
      })
    )
  show()
  await u.click(await screen.findByRole("button", { name: "预览发送内容" }))
  await u.click(await screen.findByRole("checkbox"))
  await u.type(screen.getByLabelText("输入 SEND AI 确认"), "SEND AI")
  await u.click(screen.getByRole("button", { name: "确认发送并生成建议" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("request-42")
  expect(screen.queryByLabelText("AI 生成建议")).not.toBeInTheDocument()
  expect(screen.queryByLabelText("完整请求正文")).not.toBeInTheDocument()
  expect(api).toHaveBeenCalledTimes(3)
})
it("blocks expired previews", async () => {
  const u = userEvent.setup()
  vi.mocked(api)
    .mockResolvedValueOnce({ enabled: true, destination })
    .mockResolvedValueOnce({ ...preview, expiresAt: "2020-01-01T00:00:00Z" })
  show()
  await u.click(await screen.findByRole("button", { name: "预览发送内容" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("预览已到期")
  expect(
    screen.getByRole("button", { name: "确认发送并生成建议" })
  ).toBeDisabled()
  expect(api).toHaveBeenCalledTimes(2)
})
it("renders model prose as text and clears it on identity switches", async () => {
  const u = userEvent.setup()
  vi.mocked(api)
    .mockResolvedValueOnce({ enabled: true, destination })
    .mockResolvedValueOnce(preview)
    .mockResolvedValueOnce({
      ...answer,
      answer: { ...answer.answer, summary: "[查看](https://example.test)" },
    })
    .mockResolvedValueOnce({ enabled: false })
  show()
  await u.click(await screen.findByRole("button", { name: "预览发送内容" }))
  await u.click(await screen.findByRole("checkbox"))
  await u.type(screen.getByLabelText("输入 SEND AI 确认"), "SEND AI")
  await u.click(screen.getByRole("button", { name: "确认发送并生成建议" }))
  expect(
    await screen.findByText("[查看](https://example.test)")
  ).toBeInTheDocument()
  expect(screen.queryByRole("link")).not.toBeInTheDocument()
  act(() => role(undefined, "bob"))
  expect(screen.queryByLabelText("AI 生成建议")).not.toBeInTheDocument()
})
