import { beforeEach, afterEach, expect, it, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError, type SendOptions } from "pocketbase"
import { api } from "@/services/api"
import { CancelJobDialog } from "./cancel-dialog"
import { fixturePrepared, fixtureOperation } from "./fixtures.test-support"
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
function show() {
  render(
    <QueryClientProvider client={client}>
      <CancelJobDialog
        clusterId="alpha"
        jobId="42"
        onClose={vi.fn()}
        onStart={onStart}
        onResult={onResult}
      />
    </QueryClientProvider>
  )
}
async function fill() {
  const user = userEvent.setup()
  await screen.findByText("请核对最新归属")
  await user.type(screen.getByLabelText("取消原因"), "功能验证")
  await user.type(screen.getByLabelText("输入 Job ID 42 以确认"), "42")
  return user
}
it("最新归属检查通过后，原因和完整 Job ID 同时正确才能提交一次", async () => {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string) =>
      (path.endsWith("cancel-context")
        ? fixturePrepared()
        : fixtureOperation()) as T
  )
  show()
  await screen.findByText("请核对最新归属")
  const confirm = screen.getByRole("button", { name: "确认取消作业" })
  expect(confirm).toBeDisabled()
  const user = await fill()
  expect(confirm).toBeEnabled()
  await user.click(confirm)
  expect(await screen.findByText("已核实取消")).toBeVisible()
  const posts = vi
    .mocked(api)
    .mock.calls.filter(([, options]) => options?.method === "POST")
  expect(posts).toHaveLength(1)
  expect(posts[0][1]?.body).toEqual({
    proof: "signed-proof",
    confirmation: "42",
    reason: "功能验证",
  })
  expect(onStart).toHaveBeenCalledWith("test-operation")
  expect(
    screen.queryByRole("button", { name: "确认取消作业" })
  ).not.toBeInTheDocument()
})
it("没有 Slurm 身份关联时禁止提交", async () => {
  vi.mocked(api).mockResolvedValue({
    ...fixturePrepared(),
    allowed: false,
    proof: "",
    message: "未关联 Slurm 身份",
  })
  show()
  expect(await screen.findByText("未关联 Slurm 身份")).toBeVisible()
  expect(screen.getByRole("button", { name: "确认取消作业" })).toBeDisabled()
  expect(screen.queryByLabelText("取消原因")).not.toBeInTheDocument()
})
it("丢失响应只查询操作结果，不重试取消 POST", async () => {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: SendOptions) => {
      if (options?.method === "POST")
        throw new ClientResponseError({ status: 0 })
      return (
        path.endsWith("cancel-context")
          ? fixturePrepared()
          : fixtureOperation("unknown")
      ) as T
    }
  )
  show()
  const user = await fill()
  await user.click(screen.getByRole("button", { name: "确认取消作业" }))
  expect(await screen.findByText("结果待核实")).toBeVisible()
  expect(
    vi
      .mocked(api)
      .mock.calls.filter(([, options]) => options?.method === "POST")
  ).toHaveLength(1)
  expect(vi.mocked(api)).toHaveBeenCalledWith(
    "/clusters/alpha/job-operations/test-operation"
  )
})
it("过期或状态变化的确认要求重新校验，不显示虚假的未知执行结果", async () => {
  vi.mocked(api).mockImplementation(
    async <T,>(_path: string, options?: SendOptions) => {
      if (options?.method === "POST")
        throw new ClientResponseError({
          status: 409,
          response: { error: { message: "作业状态已变化，请重新校验。" } },
        })
      return fixturePrepared() as T
    }
  )
  show()
  const user = await fill()
  await user.click(screen.getByRole("button", { name: "确认取消作业" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("作业状态已变化")
  expect(screen.getByRole("button", { name: "确认取消作业" })).toBeDisabled()
  expect(screen.queryByText("结果待核实")).not.toBeInTheDocument()
  await user.click(screen.getByRole("button", { name: "重新校验" }))
  await waitFor(() =>
    expect(screen.getByLabelText("输入 Job ID 42 以确认")).toHaveValue("")
  )
  expect(
    vi
      .mocked(api)
      .mock.calls.filter(([path]) => path.includes("job-operations"))
  ).toHaveLength(0)
})
