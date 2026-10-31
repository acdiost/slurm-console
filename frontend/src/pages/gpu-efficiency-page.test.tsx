import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { GPUEfficiencyPage } from "./gpu-efficiency-page"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import {
  trendSegments,
  type EfficiencyPage,
} from "@/features/gpu-efficiency/schema"
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
const stat = { samples: 48, coveragePercent: 80, mean: 10, p95: 10, peak: 10 }
const fixture: EfficiencyPage = {
  revision: 1,
  page: 1,
  perPage: 20,
  totalItems: 1,
  analysis: {
    status: "ok",
    source: "隔离监控源",
    range: "1h",
    start: 1793500000,
    end: 1793503600,
    step: 60,
    expectedSamples: 60,
    lowThresholdPercent: 20,
    minimumCoveragePercent: 80,
    minimumLowSamplePercent: 80,
    fetchedAt: "2026-10-31T06:00:00Z",
    cached: false,
    ignoredSeries: 1,
    summary: {
      observedDevices: 1,
      low: 1,
      active: 0,
      insufficient: 0,
      ambiguous: 0,
      validSamples: 48,
      meanUtilization: 10,
    },
    points: [
      { time: 1793500060, meanUtilization: 0, devices: 1 },
      { time: 1793500120, meanUtilization: null, devices: 0 },
      { time: 1793500180, meanUtilization: 10, devices: 1 },
    ],
    items: [
      {
        uuid: "GPU-test",
        node: "node1",
        model: "Test GPU",
        index: "0",
        status: "low",
        utilization: stat,
        memoryPercent: { ...stat, mean: 50 },
        powerW: {
          ...stat,
          mean: null,
          samples: 0,
          coveragePercent: 0,
          p95: null,
          peak: null,
        },
        lowSamples: 48,
        lowSamplePercent: 100,
      },
    ],
  },
}
function role(allowed = true) {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "alice",
      email: "alice@example.test",
      username: "alice",
      name: "Alice",
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
              ...(allowed ? ["metrics.read"] : []),
            ],
          },
        ],
      },
    },
  })
}
function mock(value = fixture) {
  vi.mocked(api).mockImplementation(async <T,>(p: string) => {
    if (p.startsWith("/clusters?"))
      return { items: [cluster], totalItems: 1, page: 1, perPage: 20 } as T
    return value as T
  })
}
function show() {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/gpu-efficiency?cluster=alpha"]}>
        <GPUEfficiencyPage />
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
it("shows physical telemetry evidence, independent field coverage, and read-only monitoring links", async () => {
  show()
  await screen.findByText("GPU-test")
  expect(screen.getAllByText("48 / 60 点")).toHaveLength(2)
  expect(screen.getByText("0 / 60 点")).toBeInTheDocument()
  expect(screen.getByText(/不代表作业产出/)).toBeInTheDocument()
  expect(screen.getByRole("link", { name: "查看趋势" })).toHaveAttribute(
    "href",
    "/gpu?cluster=alpha&node=node1&gpu=GPU-test&range=1h&filterMode=custom"
  )
  expect(vi.mocked(api).mock.calls.every(([, o]) => !o?.method)).toBe(true)
})
it("keeps unknown trend points disconnected while preserving zero", async () => {
  expect(trendSegments(fixture.analysis.points)).toHaveLength(2)
  show()
  await screen.findByText("GPU-test")
  await userEvent.click(screen.getByText("查看窗口采样明细与参与卡数"))
  expect(screen.getByText("0.0%")).toBeInTheDocument()
  expect(screen.getByRole("img", { name: /缺失点断开/ })).toBeInTheDocument()
})
it("sends approved window and table filters", async () => {
  show()
  await screen.findByText("GPU-test")
  await userEvent.selectOptions(screen.getByLabelText("GPU 效率窗口"), "24h")
  await waitFor(() =>
    expect(
      vi.mocked(api).mock.calls.some(([p]) => p.includes("range=24h"))
    ).toBe(true)
  )
  await userEvent.type(screen.getByLabelText("搜索 GPU 效率"), "missing")
  await userEvent.click(screen.getByRole("button", { name: "筛选" }))
  await waitFor(() =>
    expect(
      vi.mocked(api).mock.calls.some(([p]) => p.includes("q=missing"))
    ).toBe(true)
  )
  expect(screen.getByText("GPU-test")).toBeInTheDocument()
})
it("does not query telemetry without metrics.read", async () => {
  role(false)
  show()
  await screen.findByText("需要此集群的监控读取权限。")
  expect(vi.mocked(api).mock.calls.some(([p]) => p.includes("/metrics/"))).toBe(
    false
  )
})
it("hides cached analysis immediately when a refresh is forbidden", async () => {
  show()
  await screen.findByText("GPU-test")
  vi.mocked(api).mockRejectedValue(new ClientResponseError({ status: 403 }))
  await userEvent.click(screen.getByRole("button", { name: "刷新分析" }))
  await screen.findByRole("alert")
  expect(screen.queryByText("GPU-test")).not.toBeInTheDocument()
  expect(screen.queryByLabelText("GPU 效率概览")).not.toBeInTheDocument()
})
it("shows unconfigured source without inferred hardware or fake utilization", async () => {
  const f = structuredClone(fixture)
  f.analysis.status = "not_configured"
  f.analysis.items = []
  f.analysis.summary.meanUtilization = null
  mock(f)
  show()
  await screen.findByText(/尚未配置 Prometheus/)
  expect(screen.queryByText("GPU-test")).not.toBeInTheDocument()
  expect(screen.queryByRole("img")).not.toBeInTheDocument()
  expect(screen.getAllByText("—").length).toBeGreaterThan(2)
})
