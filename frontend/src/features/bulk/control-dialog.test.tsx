import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError, type SendOptions } from "pocketbase"
import { api } from "@/services/api"
import { fixtureJob } from "@/features/jobs/fixtures.test-support"
import { BulkControlDialog } from "./control-dialog"
import { BulkOperations } from "./operations-panel"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const batch = "batch-confirmation-1234567890",
  ids = ["operation-42-1234567890", "operation-43-1234567890"]
const item = (target: string, i: number) => ({
  action: "jobs.cancel",
  target,
  operationId: ids[i],
  allowed: true,
  status: "ready",
  code: "",
  message: "已校验",
  states: ["PENDING"],
  job: { ...fixtureJob(), id: target },
  node: null,
})
const prepared = {
  id: batch,
  action: "jobs.cancel",
  status: "ready",
  allowed: true,
  expiresAt: "2099-01-01T00:00:00Z",
  items: [item("42", 0), item("43", 1)],
  counts: {
    ready: 2,
    processing: 0,
    confirmed: 0,
    accepted: 0,
    failed: 0,
    unknown: 0,
  },
}
const done = {
  ...prepared,
  status: "completed",
  allowed: false,
  items: prepared.items.map((x, i) => ({
    ...x,
    status: i === 0 ? "confirmed" : "failed",
    message: i === 0 ? "已取消" : "状态变化，未发送",
  })),
  counts: {
    ready: 0,
    processing: 0,
    confirmed: 1,
    accepted: 0,
    failed: 1,
    unknown: 0,
  },
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "actor",
      email: "actor@example.test",
      username: "actor",
      name: "test",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: [],
        clusters: [
          {
            cluster,
            roles: [],
            permissions: ["clusters.read", "jobs.read", "jobs.cancel"],
          },
        ],
      },
    },
  })
})
afterEach(() => client.clear())
function show(
  props: Partial<React.ComponentProps<typeof BulkControlDialog>> = {}
) {
  render(
    <QueryClientProvider client={client}>
      <BulkControlDialog
        clusterId="alpha"
        action="jobs.cancel"
        targets={["42", "43"]}
        onClose={vi.fn()}
        onStart={vi.fn()}
        onResult={vi.fn()}
        {...props}
      />
    </QueryClientProvider>
  )
}
function mock() {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: SendOptions) => {
      if (path.endsWith("/prepare")) return prepared as T
      if (options?.method === "POST") return done as T
      return done as T
    }
  )
}
it("requires a fresh complete selection, count phrase and reason before one batch execution", async () => {
  mock()
  const start = vi.fn()
  show({ onStart: start })
  await screen.findAllByText("已校验")
  const confirm = screen.getByRole("button", { name: "确认批量取消作业" })
  expect(confirm).toBeDisabled()
  await userEvent.type(screen.getByLabelText("统一操作原因"), "批量测试")
  await userEvent.type(
    screen.getByLabelText("输入 CANCEL 2 以确认"),
    "CANCEL 1"
  )
  expect(confirm).toBeDisabled()
  await userEvent.clear(screen.getByLabelText("输入 CANCEL 2 以确认"))
  await userEvent.type(
    screen.getByLabelText("输入 CANCEL 2 以确认"),
    "CANCEL 2"
  )
  await userEvent.click(confirm)
  await screen.findByText("状态变化，未发送")
  expect(start).toHaveBeenCalledWith(ids, batch)
  const posts = vi
    .mocked(api)
    .mock.calls.filter(([p]) => p.endsWith("/execute"))
  expect(posts).toHaveLength(1)
  expect(posts[0][1]?.body).toEqual({
    confirmation: "CANCEL 2",
    reason: "批量测试",
  })
  expect(
    screen.queryByRole("button", { name: "确认批量取消作业" })
  ).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "只读刷新结果" }))
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p.endsWith("/execute"))
  ).toHaveLength(1)
})
it("blocks the entire selection when a member is disallowed", async () => {
  vi.mocked(api).mockResolvedValue({
    ...prepared,
    id: "",
    allowed: false,
    status: "blocked",
    items: [
      prepared.items[0],
      {
        ...prepared.items[1],
        allowed: false,
        status: "failed",
        message: "root 作业不可取消",
      },
    ],
  })
  show()
  await screen.findByText("root 作业不可取消")
  expect(
    screen.getByRole("button", { name: "确认批量取消作业" })
  ).toBeDisabled()
  expect(screen.getByLabelText("统一操作原因")).toBeDisabled()
})
it("does not retry execute after a lost response and reads the known group", async () => {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: SendOptions) => {
      if (path.endsWith("/prepare")) return prepared as T
      if (options?.method === "POST")
        throw new ClientResponseError({ status: 0 })
      return done as T
    }
  )
  show()
  await screen.findAllByText("已校验")
  await userEvent.type(screen.getByLabelText("统一操作原因"), "响应丢失测试")
  await userEvent.type(
    screen.getByLabelText("输入 CANCEL 2 以确认"),
    "CANCEL 2"
  )
  await userEvent.click(
    screen.getByRole("button", { name: "确认批量取消作业" })
  )
  await screen.findByText("状态变化，未发送")
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p.endsWith("/execute"))
  ).toHaveLength(1)
  expect(
    vi.mocked(api).mock.calls.some(([p, o]) => p.endsWith(batch) && !o?.method)
  ).toBe(true)
})
it("restores multiple durable results through GET only", async () => {
  vi.mocked(api).mockResolvedValue(done)
  render(
    <QueryClientProvider client={client}>
      <BulkOperations
        clusterId="alpha"
        ids={ids}
        resource="job"
        automatic={false}
      />
    </QueryClientProvider>
  )
  await screen.findByText("已核实 1")
  expect(screen.getByText("未完成 1")).toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "只读刷新记录" }))
  await waitFor(() => expect(vi.mocked(api).mock.calls.length).toBe(2))
  expect(vi.mocked(api).mock.calls.every(([, o]) => !o?.method)).toBe(true)
})
it("hides result access without read permission", async () => {
  useAuth.setState({ user: null })
  render(
    <QueryClientProvider client={client}>
      <BulkOperations
        clusterId="alpha"
        ids={ids}
        resource="job"
        automatic={false}
      />
    </QueryClientProvider>
  )
  expect(screen.getByRole("alert")).toHaveTextContent("没有读取")
  expect(api).not.toHaveBeenCalled()
})

it("keeps a known pre-intent rejection when reloading a completed batch context", async () => {
  vi.mocked(api).mockResolvedValue(done)
  render(
    <QueryClientProvider client={client}>
      <BulkOperations
        clusterId="alpha"
        ids={ids}
        resource="job"
        automatic={false}
        batchId={batch}
      />
    </QueryClientProvider>
  )
  await screen.findByText("状态变化，未发送")
  expect(screen.getByText("未完成 1")).toBeInTheDocument()
  expect(vi.mocked(api).mock.calls[0][0]).toContain(batch)
})
it("falls back to durable operation IDs after the in-memory context is gone", async () => {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.endsWith(batch)) throw new ClientResponseError({ status: 404 })
    return done as T
  })
  render(
    <QueryClientProvider client={client}>
      <BulkOperations
        clusterId="alpha"
        ids={ids}
        resource="job"
        automatic={false}
        batchId={batch}
      />
    </QueryClientProvider>
  )
  await screen.findByText("已核实 1")
  expect(
    vi.mocked(api).mock.calls.some(([p]) => p.includes("/operations?"))
  ).toBe(true)
  expect(vi.mocked(api).mock.calls.every(([, o]) => !o?.method)).toBe(true)
})
