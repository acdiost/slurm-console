import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useNavigate } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import {
  cluster,
  fixtureOverview,
} from "@/features/overview/fixtures.test-support"
import { OverviewPage } from "./overview-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const inventory = { items: [cluster], page: 1, perPage: 20, totalItems: 1 }
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  useAuth.setState({ status: "authenticated", user: null, message: "" })
  vi.mocked(api).mockReset()
})
afterEach(() => client.clear())
function show(path = "/overview", extra?: React.ReactNode) {
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <OverviewPage />
        {extra}
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mockResponses(overview = fixtureOverview()) {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string) =>
      (path.includes("/overview") ? overview : inventory) as T
  )
}
function card(name: string) {
  const section = screen.getByRole("heading", { name }).closest("section")
  if (!section) throw new Error("missing metric card")
  return within(section)
}
describe("集群总览", () => {
  it("展示真实汇总、不可用资源、无限时分区和需关注节点", async () => {
    mockResponses()
    show()
    expect(await screen.findByText("控制器在线")).toBeVisible()
    expect(card("CPU 容量").getByText("16")).toBeVisible()
    expect(
      screen.getByRole("img", {
        name: /CPU 容量：总量 16，已分配 0，空闲 8，不可用或预留 8/,
      })
    ).toBeVisible()
    expect(
      card("GPU 调度资源").getByText("当前节点未配置 GPU 调度资源。")
    ).toBeVisible()
    expect(screen.getByText("计划维护")).toBeVisible()
    expect(screen.getByText("不限时")).toBeVisible()
    expect(
      screen.getByRole("progressbar", { name: "DOWN 节点比例" })
    ).toHaveAttribute("aria-valuenow", "50")
  })
  it("分项失败保留其他数据，失败容量显示未知而非零", async () => {
    const overview = fixtureOverview()
    overview.nodes = {
      status: "error",
      code: "SLURM_TIMEOUT",
      message: "Slurm 请求超时。",
      item: null,
      fetchedAt: "",
      cached: false,
    }
    mockResponses(overview)
    show()
    expect(await screen.findByText("节点数据暂不可用")).toBeVisible()
    expect(screen.getByText("控制器在线")).toBeVisible()
    expect(card("CPU 容量").getByText("—")).toBeVisible()
    expect(card("CPU 容量").queryByText("0")).not.toBeInTheDocument()
    expect(screen.getByText("运行中")).toBeVisible()
  })
  it("刷新收到 403 后立即隐藏曾成功读取的指标", async () => {
    let denied = false
    vi.mocked(api).mockImplementation(async <T,>(path: string) => {
      if (path.includes("/overview") && denied)
        throw new ClientResponseError({ status: 403 })
      return (path.includes("/overview") ? fixtureOverview() : inventory) as T
    })
    const user = userEvent.setup()
    show()
    await screen.findByText("控制器在线")
    denied = true
    await user.click(screen.getByRole("button", { name: "刷新总览" }))
    expect(
      await screen.findByText("当前账户没有访问此集群总览的权限。")
    ).toBeVisible()
    expect(
      screen.queryByRole("heading", { name: "CPU 容量" })
    ).not.toBeInTheDocument()
    expect(screen.queryByText("控制器在线")).not.toBeInTheDocument()
  })
  it("切换集群期间清空旧指标，延迟旧响应不能覆盖新集群", async () => {
    let delayOld = false
    let release: (() => void) | undefined
    vi.mocked(api).mockImplementation(async <T,>(path: string) => {
      if (!path.includes("/overview")) return inventory as T
      if (path.includes("alpha") && delayOld)
        await new Promise<void>((resolve) => {
          release = resolve
        })
      if (path.includes("beta")) {
        const overview = fixtureOverview()
        overview.cluster = { ...cluster, id: "beta", name: "备用集群" }
        overview.health = {
          ...overview.health,
          status: "ok",
          item: {
            ...fixtureOverview().health.item!,
            serverVersion: "second-version",
          },
        }
        return overview as T
      }
      return fixtureOverview() as T
    })
    function Switch() {
      const navigate = useNavigate()
      return (
        <button onClick={() => navigate("/overview?cluster=beta")}>
          切换测试集群
        </button>
      )
    }
    const user = userEvent.setup()
    show("/overview?cluster=alpha", <Switch />)
    await screen.findByText("控制器在线")
    delayOld = true
    await user.click(screen.getByRole("button", { name: "刷新总览" }))
    await waitFor(() => expect(release).toBeDefined())
    await user.click(screen.getByRole("button", { name: "切换测试集群" }))
    expect(await screen.findByText("RESTD second-version")).toBeVisible()
    release?.()
    await waitFor(() =>
      expect(screen.queryByText("RESTD 25.11.8")).not.toBeInTheDocument()
    )
    expect(screen.getByText("RESTD second-version")).toBeVisible()
  })
  it("无授权集群时显示空状态且不发起总览请求", async () => {
    vi.mocked(api).mockResolvedValue({ ...inventory, items: [], totalItems: 0 })
    show()
    expect(await screen.findByText("暂无可访问的集群")).toBeVisible()
    expect(
      vi.mocked(api).mock.calls.some(([path]) => path.includes("/overview"))
    ).toBe(false)
    expect(screen.getByRole("link", { name: "查看我的权限" })).toHaveAttribute(
      "href",
      "/access"
    )
  })
  it("空页保留返回第一页入口，避免误报没有授权集群", async () => {
    vi.mocked(api).mockImplementation(async <T,>(path: string) => {
      if (path.includes("/overview")) return fixtureOverview() as T
      return (
        path.includes("page=3")
          ? { ...inventory, page: 3, items: [] }
          : inventory
      ) as T
    })
    const user = userEvent.setup()
    show("/overview?page=3")
    expect(await screen.findByText("本页暂无集群")).toBeVisible()
    await user.click(screen.getByRole("button", { name: "返回第一页" }))
    expect(await screen.findByText("控制器在线")).toBeVisible()
  })
})
