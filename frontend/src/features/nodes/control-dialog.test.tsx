import { beforeEach, afterEach, expect, it, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError, type SendOptions } from "pocketbase"
import { api } from "@/services/api"
import { NodeControlDialog } from "./control-dialog"
import { fixtureContext, fixtureOperation } from "./fixtures.test-support"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const onStart = vi.fn(),
  onResult = vi.fn()
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  onStart.mockClear()
  onResult.mockClear()
})
afterEach(() => client.clear())
function show(names = ["node1"]) {
  render(
    <QueryClientProvider client={client}>
      <NodeControlDialog
        clusterId="alpha"
        names={names}
        action="drain"
        onClose={vi.fn()}
        onStart={onStart}
        onResult={onResult}
      />
    </QueryClientProvider>
  )
}
it("单节点要求原因及完整名称，提交一次后只显示结果", async () => {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string) =>
      (path.includes("control-context")
        ? fixtureContext()
        : fixtureOperation()) as T
  )
  show()
  await screen.findByText("确认有效至", { exact: false })
  const confirm = screen.getByRole("button", { name: "确认Drain 排空" })
  expect(confirm).toBeDisabled()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText("操作原因"), "维护验证")
  await user.type(screen.getByLabelText("输入 node1 以确认"), "node1")
  await user.click(confirm)
  expect(await screen.findByText("已核实")).toBeVisible()
  expect(
    vi
      .mocked(api)
      .mock.calls.filter(([, options]) => options?.method === "POST")
  ).toHaveLength(1)
  expect(onStart).toHaveBeenCalledWith(["operation-node1-drain"])
  expect(
    screen.queryByRole("button", { name: "确认Drain 排空" })
  ).not.toBeInTheDocument()
})
it("批量确认绑定动作和数量，混合结果逐节点展示且不重试写入", async () => {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: SendOptions) => {
      const name = path.includes("node2") ? "node2" : "node1"
      if (path.includes("control-context")) return fixtureContext(name) as T
      if (options?.method === "POST" && name === "node2")
        throw new ClientResponseError({ status: 0 })
      return fixtureOperation(
        name,
        name === "node2" ? "unknown" : "confirmed"
      ) as T
    }
  )
  show(["node1", "node2"])
  await screen.findAllByText("确认有效至", { exact: false })
  const user = userEvent.setup()
  await user.type(screen.getByLabelText("操作原因"), "维护验证")
  await user.type(screen.getByLabelText("输入 DRAIN 2 以确认"), "DRAIN 1")
  expect(screen.getByRole("button", { name: "确认Drain 排空" })).toBeDisabled()
  await user.clear(screen.getByLabelText("输入 DRAIN 2 以确认"))
  await user.type(screen.getByLabelText("输入 DRAIN 2 以确认"), "DRAIN 2")
  await user.click(screen.getByRole("button", { name: "确认Drain 排空" }))
  expect(await screen.findByText("结果待核实")).toBeVisible()
  expect(screen.getByText("已核实")).toBeVisible()
  expect(
    vi
      .mocked(api)
      .mock.calls.filter(([, options]) => options?.method === "POST")
  ).toHaveLength(2)
  expect(vi.mocked(api)).toHaveBeenCalledWith(
    "/clusters/alpha/node-operations/operation-node2-drain"
  )
  await waitFor(() => expect(onResult).toHaveBeenCalledOnce())
})
it("任一节点未通过校验时禁止整个批量提交", async () => {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string) =>
      (path.includes("node2")
        ? {
            ...fixtureContext("node2"),
            allowed: false,
            message: "节点已在维护",
          }
        : fixtureContext()) as T
  )
  show(["node1", "node2"])
  expect(await screen.findByRole("alert")).toHaveTextContent("节点已在维护")
  expect(screen.getByRole("button", { name: "确认Drain 排空" })).toBeDisabled()
})
