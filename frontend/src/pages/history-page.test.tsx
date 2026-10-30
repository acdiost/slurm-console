import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { HistoryPage } from "./history-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const n = (value: number | null) => ({ value, infinite: false })
const exit = {
  status: ["SUCCESS"],
  code: n(0),
  signal: n(null),
  signalName: "",
}
const job = {
  key: "a".repeat(64),
  id: "42",
  cluster: "fixture",
  name: "first run",
  user: "alice",
  account: "research",
  partition: "normal",
  qos: "normal",
  states: ["COMPLETED"],
  reason: "None",
  nodes: "node1",
  submitTime: n(1700000010),
  startTime: n(null),
  endTime: n(1700000030),
  elapsedSeconds: n(0),
  suspendedSeconds: n(0),
  timeLimitMinutes: { value: null, infinite: true },
  totalCpuSeconds: 0,
  arrayJobId: n(null),
  arrayTaskId: n(null),
  hetJobId: n(null),
  hetJobOffset: n(null),
  exit,
  derivedExit: exit,
  allocatedTres: [{ id: "2", type: "mem", name: "", count: "1024" }],
  requestedTres: [],
  steps: [],
}
const detail = {
  ...job,
  steps: [
    {
      id: "42.batch",
      name: "batch",
      states: ["COMPLETED"],
      nodes: "node1",
      tasks: n(1),
      startTime: n(null),
      endTime: n(1700000030),
      elapsedSeconds: n(0),
      totalCpuSeconds: 0,
      exit,
      allocatedTres: [],
      consumedAverage: [],
      consumedTotal: [],
    },
  ],
}
function role(allowed = true) {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "actor",
      email: "actor@example.test",
      username: "actor",
      name: "测试",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: [],
        clusters: [
          {
            cluster,
            roles: [],
            permissions: [
              "clusters.read",
              ...(allowed ? ["job_history.read"] : []),
            ],
          },
        ],
      },
    },
  })
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  role()
  vi.mocked(api).mockReset()
})
afterEach(() => client.clear())
function Location() {
  return <output aria-label="历史查询 URL">{useLocation().search}</output>
}
function show(url = "/history?cluster=alpha&start=1700000000&end=1700086400") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[url]}>
        <HistoryPage />
        <Location />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock(empty = false) {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.startsWith("/clusters?page="))
      return { items: [cluster], page: 1, perPage: 20, totalItems: 1 } as T
    if (path.includes(`/job-history/${job.key}?`))
      return {
        item: detail,
        start: 1700000000,
        end: 1700086400,
        fetchedAt: "2026-10-30T13:00:00Z",
        cached: true,
      } as T
    return {
      items: empty
        ? []
        : [job, { ...job, key: "b".repeat(64), name: "retry run" }],
      page: 1,
      perPage: 20,
      totalItems: empty ? 0 : 41,
      cluster: "fixture",
      start: 1700000000,
      end: 1700086400,
      fetchedAt: "2026-10-30T13:00:00Z",
      cached: false,
    } as T
  })
}
it("keeps repeated Job IDs as separate records and exposes zero CPU and memory units", async () => {
  mock()
  show()
  const table = await screen.findByRole("table", { name: "历史作业记录" })
  expect(within(table).getAllByRole("button", { name: "#42" })).toHaveLength(2)
  expect(within(table).getByText("first run")).toBeInTheDocument()
  expect(within(table).getAllByText("0 CPU 秒")).toHaveLength(2)
  expect(within(table).getAllByText("mem=1024 MiB")).toHaveLength(2)
  const call = vi
    .mocked(api)
    .mock.calls.find(([p]) => p.includes("/job-history?"))![0]
  expect(call).toContain("start=1700000000")
  expect(call).toContain("end=1700086400")
})
it("loads record detail without treating Job ID as unique and keeps step totals separate", async () => {
  mock()
  show()
  await userEvent.click(
    (await screen.findAllByRole("button", { name: "#42" }))[0]
  )
  expect(await screen.findByText("42.batch · batch")).toBeInTheDocument()
  expect(screen.getByText("作业步骤（1）")).toBeInTheDocument()
  expect(
    vi
      .mocked(api)
      .mock.calls.some(([p]) =>
        p.includes(`/job-history/${job.key}?start=1700000000`)
      )
  ).toBe(true)
  expect(screen.getByText(/步骤单独展示/)).toBeInTheDocument()
  expect(screen.getAllByText("未发生/未设置").length).toBeGreaterThan(0)
})
it("does not query history without its independent permission", async () => {
  role(false)
  mock()
  show()
  expect(await screen.findByRole("alert")).toHaveTextContent("历史作业读取权限")
  expect(
    vi.mocked(api).mock.calls.some(([p]) => p.includes("job-history"))
  ).toBe(false)
})
it("preserves window when paging and does not combine steps into rows", async () => {
  mock()
  show()
  await screen.findByRole("table")
  await userEvent.click(screen.getByRole("button", { name: "下一页历史" }))
  expect(screen.getByLabelText("历史查询 URL")).toHaveTextContent(
    "start=1700000000"
  )
  expect(screen.getByLabelText("历史查询 URL")).toHaveTextContent("page=2")
  expect(vi.mocked(api).mock.calls.some(([p]) => p.includes("page=2"))).toBe(
    true
  )
})
it("rejects a broad time window before querying and explains empty results", async () => {
  mock(true)
  show()
  expect(
    await screen.findByText("当前提交时间范围与筛选条件下没有可见记账记录。")
  ).toBeInTheDocument()
  const from = screen.getByLabelText("提交时间起（本地时间）"),
    to = screen.getByLabelText("提交时间止（不含）")
  await userEvent.clear(from)
  await userEvent.type(from, "2026-01-01T00:00")
  await userEvent.clear(to)
  await userEvent.type(to, "2026-03-01T00:00")
  await userEvent.click(screen.getByRole("button", { name: "查询历史" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("最多 31 天")
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p.includes("/job-history?"))
      .length
  ).toBe(1)
})
