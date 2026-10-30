import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { GPUPage } from "./gpu-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const sample = (value: number | null, status = "fresh") => ({
  value,
  status,
  timestamp: 1900000000,
})
const target = {
  uuid: "GPU-test",
  node: "node1",
  index: "0",
  model: "Test GPU",
  status: "partial",
  utilization: sample(0),
  memoryUsedMiB: sample(0),
  memoryFreeMiB: sample(40960),
  memoryTotalMiB: sample(40960),
  temperatureC: sample(null, "invalid"),
  powerW: sample(100),
  totalSource: "used_plus_free",
}
function role(metrics = true, nodes = true) {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "actor",
      email: "actor@example.test",
      username: "actor",
      name: "管理员",
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
              ...(metrics ? ["metrics.read"] : []),
              ...(nodes ? ["nodes.read"] : []),
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
  vi.mocked(api).mockReset()
  role()
})
afterEach(() => client.clear())
function Location() {
  return <output aria-label="当前 GPU 查询">{useLocation().search}</output>
}
function show(url = "/gpu?cluster=alpha") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[url]}>
        <GPUPage />
        <Location />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock(state = "ok") {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.startsWith("/clusters?page=") || path === "/clusters?perPage=1")
      return { items: [cluster], page: 1, perPage: 20, totalItems: 1 } as T
    if (path.includes("/history?"))
      return {
        uuid: "GPU-test",
        node: "node1",
        range: "1h",
        start: 1900000000,
        end: 1900000120,
        step: 60,
        cached: false,
        fetchedAt: "2030-03-17T17:46:40Z",
        series: {
          utilization: [
            { time: 1900000000, value: 0 },
            { time: 1900000060, value: null },
            { time: 1900000120, value: 50 },
          ],
        },
      } as T
    return {
      monitoring: {
        status: state,
        message:
          state === "not_configured" ? "尚未配置 Prometheus/DCGM 监控源。" : "",
        source: state === "ok" ? "隔离协议数据" : "",
        fetchedAt: "2030-03-17T17:46:40Z",
        cached: false,
        ignoredSeries: 0,
        items: [],
      },
      allocation: {
        status: "ok",
        message: "",
        fetchedAt: "2030-03-17T17:46:40Z",
        cached: false,
        item: {
          gpus: {
            total: 4,
            allocated: 2,
            idle: 2,
            unavailable: 0,
            unknownNodes: 0,
          },
        },
      },
      items: state === "ok" ? [target] : [],
      summary: { fresh: 0, partial: 1, stale: 0, ambiguous: 0 },
      page: 1,
      perPage: 20,
      totalItems: state === "ok" ? 25 : 0,
      revision: 1,
    } as T
  })
}
it("分开显示分配量和真实指标，保留零值", async () => {
  mock()
  show()
  await screen.findByRole("button", { name: "GPU-test" })
  expect(
    within(
      screen.getByRole("region", { name: "GPU 资源与观测概况" })
    ).getByText("Slurm 已分配 GPU")
  ).toBeInTheDocument()
  expect(screen.getByText("0 %")).toBeInTheDocument()
  expect(screen.getByText("已用 + 空闲合计")).toBeInTheDocument()
  expect(screen.getByText(/无效值/)).toBeInTheDocument()
})
it("未配置来源不伪造物理卡数", async () => {
  mock("not_configured")
  show()
  await screen.findByText("未配置监控源")
  expect(
    screen.queryByRole("button", { name: "GPU-test" })
  ).not.toBeInTheDocument()
  expect(
    within(
      screen.getByRole("region", { name: "GPU 资源与观测概况" })
    ).getAllByText("未知")
  ).toHaveLength(2)
})
it("无权限不请求指标", async () => {
  role(false)
  mock()
  show()
  await screen.findByRole("alert")
  expect(
    vi.mocked(api).mock.calls.some(([path]) => path.includes("/metrics/gpu"))
  ).toBe(false)
})
it("趋势保留零与断点，切换固定窗口", async () => {
  mock()
  show()
  const u = userEvent.setup()
  await u.click(await screen.findByRole("button", { name: "GPU-test" }))
  expect(
    await screen.findByRole("img", { name: /2 个有效点，1 个缺失点/ })
  ).toBeInTheDocument()
  await u.click(screen.getByRole("combobox", { name: "GPU 趋势窗口" }))
  await u.click(screen.getByRole("option", { name: "最近 6 小时" }))
  expect(screen.getByLabelText("当前 GPU 查询")).toHaveTextContent("range=6h")
})
it("节点权限独立，分页保留集群", async () => {
  role(true, false)
  mock()
  show()
  const u = userEvent.setup()
  await screen.findByRole("button", { name: "GPU-test" })
  expect(
    screen.getByText(/未授予 nodes.read，未查询节点分配量。/)
  ).toBeInTheDocument()
  await u.click(screen.getByRole("button", { name: "下一页 GPU" }))
  expect(screen.getByLabelText("当前 GPU 查询")).toHaveTextContent(
    "cluster=alpha"
  )
  expect(screen.getByLabelText("当前 GPU 查询")).toHaveTextContent("page=2")
})
