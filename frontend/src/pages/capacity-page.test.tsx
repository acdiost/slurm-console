import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError, type SendOptions } from "pocketbase"
import { CapacityPage } from "./capacity-page"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { segments } from "@/features/capacity/trend"
import { percentage, type History } from "@/features/capacity/schema"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
)
let client: QueryClient
const cpu = { total: 12, allocated: 2, idle: 4, unavailable: 6 },
  mem = { total: 1500, allocated: 250, idle: 750, unavailable: 500 },
  gpu = {
    total: 2,
    allocated: null,
    idle: null,
    unavailable: null,
    unknownNodes: 1,
  }
const frame = {
  version: 1 as const,
  nodes: 2,
  unavailableNodes: 1,
  cpus: cpu,
  memoryMiB: mem,
  gpus: gpu,
}
const report = {
  revision: 1,
  generatedAt: "2026-11-01T00:00:00Z",
  source: { fetchedAt: "2026-11-01T00:00:00Z", cached: false },
  report: {
    totals: frame,
    headroom: {
      stateAvailableNodes: 1,
      largestCpuIdle: 4,
      largestMemoryIdleMiB: 750,
      cpuIdleBuckets: [
        { label: "0", nodes: 0 },
        { label: "1–7", nodes: 1 },
        { label: "8–31", nodes: 0 },
        { label: "32–63", nodes: 0 },
        { label: "64+", nodes: 0 },
      ],
    },
    partitions: [{ name: "cpu", states: ["UP"], frame }],
    page: 1,
    perPage: 20,
    totalItems: 1,
  },
  partitionSource: { status: "ok", code: "", message: "" },
}
const stat = {
  knownSamples: 2,
  weightedAllocatedPercent: 20,
  peakAllocatedPercent: 40,
  minimumIdle: 0,
}
const history: History = {
  start: 1793490000,
  end: 1793493600,
  stepSeconds: 300,
  expectedSamples: 12,
  availableSamples: 2,
  points: Array.from({ length: 12 }, (_, i) => ({
    bucket: 1793490000 + i * 300,
    observedAt:
      i === 0 || i === 2
        ? new Date((1793490000 + i * 300 + 10) * 1000).toISOString()
        : "",
    frame: i === 0 || i === 2 ? frame : null,
  })),
  cpus: stat,
  memoryMiB: stat,
  gpus: {
    knownSamples: 0,
    weightedAllocatedPercent: null,
    peakAllocatedPercent: null,
    minimumIdle: null,
  },
}
function role(node = true, cap = true) {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "alice",
      email: "alice@example.test",
      name: "Alice",
      username: "alice",
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
              ...(node ? ["nodes.read"] : []),
              ...(cap ? ["capacity.read"] : []),
            ],
          },
        ],
      },
    },
  })
}
function mock() {
  vi.mocked(api).mockImplementation(async <T,>(p: string) => {
    if (p.startsWith("/clusters?"))
      return { items: [cluster], totalItems: 1, page: 1, perPage: 20 } as T
    if (p.includes("/capacity/history")) return { revision: 1, history } as T
    if (p.includes("/capacity")) return report as T
    return {} as T
  })
}
function show(url = "/capacity?cluster=alpha") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[url]}>
        <CapacityPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  role()
  mock()
})
afterEach(() => client.clear())
it("shows capacity and headroom separately from utilization with nullable GPU counts", async () => {
  show()
  await screen.findByText("状态可用节点的 CPU 余量分布")
  expect(screen.getByText(/1 个节点的 GPU/)).toBeInTheDocument()
  expect(screen.getByText(/两个最大值分别计算/)).toBeInTheDocument()
  expect(screen.getByText(/共享节点会出现在多个分区/)).toBeInTheDocument()
  expect(screen.getByRole("link", { name: "查看节点" })).toHaveAttribute(
    "href",
    "/nodes?cluster=alpha&partition=cpu&filterMode=custom"
  )
  expect(vi.mocked(api).mock.calls.every(([, o]) => !o?.method)).toBe(true)
})
it("shows explicit coverage and actual gaps rather than zero filled samples", async () => {
  show()
  await screen.findByRole("img", { name: /2\/12 个有效样本/ })
  expect(screen.getByText(/覆盖 2\/12 个完整/)).toBeInTheDocument()
  await userEvent.click(screen.getByText("查看采样明细"))
  expect(screen.getAllByText("缺失")).toHaveLength(10)
  expect(segments(history, "cpu")).toHaveLength(2)
  expect(segments(history, "gpu")).toHaveLength(0)
  expect(percentage(0, 0)).toBeNull()
  expect(percentage(null, 2)).toBeNull()
})
it("switches history resource and window using only reviewed query parameters", async () => {
  show()
  await screen.findByRole("img", { name: /资源分配趋势/ })
  await userEvent.selectOptions(screen.getByLabelText("容量趋势窗口"), "7d")
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.some(([p]) => p.includes("capacity/history?range=7d"))
    ).toBe(true)
  )
  await userEvent.selectOptions(screen.getByLabelText("容量趋势资源"), "gpu")
  expect(
    await screen.findByText("该窗口暂无可计算分配比例的样本。")
  ).toBeInTheDocument()
  expect(
    vi.mocked(api).mock.calls.every(([p]) => !p.includes("resource="))
  ).toBe(true)
})
it("sends partition filters only to current report and resets table pagination", async () => {
  show("/capacity?cluster=alpha&page=3")
  await screen.findByText("分区容量对比")
  await userEvent.type(screen.getByLabelText("检索分区名称"), "cpu")
  await userEvent.click(screen.getByRole("button", { name: "检索分区" }))
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.some(
          ([p]) => p.includes("/capacity?page=1") && p.includes("q=cpu")
        )
    ).toBe(true)
  )
  expect(
    vi
      .mocked(api)
      .mock.calls.filter(([p]) => p.includes("capacity/history"))
      .every(([p]) => !p.includes("q="))
  ).toBe(true)
})
it("never accesses capacity sources without both explicit read permissions", async () => {
  role(true, false)
  show()
  await screen.findByRole("alert")
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p.includes("/capacity"))
  ).toHaveLength(0)
})
it("hides both data sets when either endpoint reports revoked access", async () => {
  const fallback = vi.mocked(api).getMockImplementation()!
  vi.mocked(api).mockImplementation(async <T,>(p: string, o?: SendOptions) => {
    if (p.includes("capacity/history"))
      throw new ClientResponseError({ status: 403 })
    return fallback(p, o) as Promise<T>
  })
  show()
  await screen.findByRole("alert")
  expect(
    screen.queryByText("状态可用节点的 CPU 余量分布")
  ).not.toBeInTheDocument()
  expect(screen.queryByRole("img")).not.toBeInTheDocument()
})
it("keeps authorized historical data when current Slurm capacity is unavailable", async () => {
  const fallback = vi.mocked(api).getMockImplementation()!
  vi.mocked(api).mockImplementation(async <T,>(p: string, o?: SendOptions) => {
    if (p.includes("/capacity?") && !p.includes("history"))
      throw new ClientResponseError({ status: 503 })
    return fallback(p, o) as Promise<T>
  })
  show()
  await screen.findByRole("alert")
  expect(
    await screen.findByRole("img", { name: /资源分配趋势/ })
  ).toBeInTheDocument()
  expect(
    screen.queryByText("状态可用节点的 CPU 余量分布")
  ).not.toBeInTheDocument()
})
it("does not confuse absent partition config permission with absent node capacity", async () => {
  const fallback = vi.mocked(api).getMockImplementation()!
  vi.mocked(api).mockImplementation(async <T,>(p: string, o?: SendOptions) =>
    p.includes("/capacity?")
      ? ({
          ...report,
          partitionSource: {
            status: "forbidden",
            code: "FORBIDDEN",
            message: "没有分区配置读取权限。",
          },
          report: {
            ...report.report,
            partitions: [{ ...report.report.partitions[0], states: null }],
          },
        } as T)
      : (fallback(p, o) as Promise<T>)
  )
  show()
  await screen.findByText("没有分区配置读取权限。")
  expect(screen.getByText("状态可用节点的 CPU 余量分布")).toBeInTheDocument()
  expect(screen.queryByText("UP")).not.toBeInTheDocument()
})
