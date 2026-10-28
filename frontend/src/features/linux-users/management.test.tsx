import { beforeEach, afterEach, expect, it, vi } from "vitest"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { api } from "@/services/api"
import { AccountDialog } from "./management"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const onOperation = vi.fn()
const prepared = {
  allowed: true,
  message: "创建条件已核实。",
  proof: "opaque-confirmation",
  operationId: "operation_create_12345",
  expiresAt: "2099-01-01T00:00:00Z",
  action: "create",
  spec: {
    name: "worker",
    uid: 10001,
    comment: "Worker",
    shell: "/sbin/nologin",
  },
  account: null,
  expiry: null,
  minUid: 10000,
  maxUid: 59999,
}
const operation = {
  operationId: prepared.operationId,
  name: "worker",
  action: "create",
  status: "confirmed",
  message: "已读取主机并核实。",
  createdAt: "2026-10-28T00:00:00Z",
  updatedAt: "2026-10-28T00:00:01Z",
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
  vi.mocked(api).mockReset()
  onOperation.mockClear()
})
afterEach(() => client.clear())
function show(action = "create", name = "") {
  render(
    <QueryClientProvider client={client}>
      <AccountDialog
        clusterId="alpha"
        action={action}
        name={name}
        minimum={10001}
        onClose={vi.fn()}
        onOperation={onOperation}
      />
    </QueryClientProvider>
  )
}
async function fillCreation() {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText("本地用户名"), "worker")
  await user.click(screen.getByRole("button", { name: "校验创建条件" }))
  await screen.findByText("创建条件已核实。")
  return user
}
it("创建先校验，再要求用户名和原因，只发送一次实际写入", async () => {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string) =>
      (path.endsWith("/prepare") ? prepared : operation) as T
  )
  show()
  const user = await fillCreation()
  const confirm = screen.getByRole("button", { name: "确认创建本地账户" })
  expect(confirm).toBeDisabled()
  await user.type(screen.getByLabelText("操作原因"), "开发验证")
  await user.type(screen.getByLabelText("输入 worker 以确认"), "worker")
  await user.click(confirm)
  expect(await screen.findByText("已读取主机并核实。")).toBeVisible()
  expect(onOperation).toHaveBeenCalledWith(prepared.operationId)
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p.endsWith("/execute"))
  ).toHaveLength(1)
})
it("主机拒绝目录或保护账户时不提供确认提交", async () => {
  vi.mocked(api).mockResolvedValue({
    ...prepared,
    allowed: false,
    message: "受保护账户不可操作。",
    proof: "",
    operationId: "",
    expiresAt: "",
  })
  show("lock", "root")
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "校验最新账户状态" }))
  expect(await screen.findByText("受保护账户不可操作。")).toBeVisible()
  expect(
    screen.queryByRole("button", { name: "确认锁定本地账户" })
  ).not.toBeInTheDocument()
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p.endsWith("/execute"))
  ).toHaveLength(0)
})
it("网络丢失后保留操作编号并仅查询结果，不重发执行", async () => {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.endsWith("/prepare")) return prepared as T
    if (path.endsWith("/execute")) throw Error("connection lost")
    return { ...operation, status: "unknown", message: "请查询原操作。" } as T
  })
  show()
  const user = await fillCreation()
  await user.type(screen.getByLabelText("操作原因"), "开发验证")
  await user.type(screen.getByLabelText("输入 worker 以确认"), "worker")
  await user.click(screen.getByRole("button", { name: "确认创建本地账户" }))
  await screen.findByText(/请求结果不确定/)
  const panel = within(screen.getByRole("region", { name: "本地账户操作结果" }))
  await user.click(panel.getByRole("button", { name: "查询操作结果" }))
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p.endsWith("/execute"))
  ).toHaveLength(1)
})
it("过期的确认不允许提交", async () => {
  vi.mocked(api).mockResolvedValue({
    ...prepared,
    expiresAt: "2020-01-01T00:00:00Z",
  })
  show()
  const user = await fillCreation()
  await user.type(screen.getByLabelText("输入 worker 以确认"), "worker")
  expect(
    screen.getByRole("button", { name: "确认创建本地账户" })
  ).toBeDisabled()
  expect(screen.getByRole("alert")).toHaveTextContent("确认已过期")
})
