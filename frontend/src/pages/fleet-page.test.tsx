import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import {
  fixtureFleetItem,
  fixtureFleetPage,
} from "@/features/fleet/fixtures.test-support"
import { FleetPage } from "./fleet-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
})
afterEach(() => client.clear())
function Location() {
  const l = useLocation()
  return <output aria-label="当前查询">{l.search}</output>
}
function show(path = "/fleet") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <FleetPage />
        <Location />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
it("工作台分页、筛选和排序由 URL 驱动，资源链接绑定集群", async () => {
  vi.mocked(api).mockResolvedValue(fixtureFleetPage())
  show()
  await screen.findByRole("article", { name: "集群 科研集群" })
  const user = userEvent.setup()
  expect(
    within(screen.getByRole("article", { name: "集群 科研集群" })).getByRole(
      "link",
      { name: "节点" }
    )
  ).toHaveAttribute("href", "/nodes?cluster=alpha")
  await user.click(screen.getByRole("button", { name: "下一页集群" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=2")
  )
  await user.type(screen.getByLabelText("搜索集群"), "research")
  await user.click(screen.getByRole("button", { name: "查询集群" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("q=research")
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=1")
  await user.click(
    await screen.findByRole("button", { name: "切换集群排序方向" })
  )
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("order=desc")
  )
})
it("多个集群保持独立，未知节点容量与无权作业不显示假零", async () => {
  const a = fixtureFleetItem(),
    b = fixtureFleetItem("beta", "备用集群")
  b.state = "unavailable"
  b.health = {
    status: "error",
    code: "SLURM_TIMEOUT",
    message: "Slurm 请求超时。",
    item: null,
    fetchedAt: "",
    cached: false,
  }
  b.nodes = { ...b.health }
  b.jobs = {
    status: "forbidden",
    code: "FORBIDDEN",
    message: "未授予该项读取权限。",
    item: null,
    fetchedAt: "",
    cached: false,
  }
  b.permissions = ["clusters.read"]
  vi.mocked(api).mockResolvedValue({
    ...fixtureFleetPage(),
    items: [a, b],
    totalItems: 2,
  })
  show()
  const first = within(
      await screen.findByRole("article", { name: "集群 科研集群" })
    ),
    second = within(screen.getByRole("article", { name: "集群 备用集群" }))
  expect(first.getByText("控制器连通")).toBeVisible()
  expect(second.getByText("容量 —")).toBeVisible()
  expect(second.queryByRole("progressbar")).not.toBeInTheDocument()
  expect(second.getByText("未授予作业读取权限。")).toBeVisible()
  expect(second.queryByRole("link", { name: "节点" })).not.toBeInTheDocument()
})
it("刷新遇到授权变化后隐藏旧卡片和汇总", async () => {
  let denied = false
  vi.mocked(api).mockImplementation(async <T,>() => {
    if (denied)
      throw new ClientResponseError({
        status: 409,
        response: { error: { message: "授权范围已变化，请重新读取工作台。" } },
      })
    return fixtureFleetPage() as T
  })
  show()
  await screen.findByRole("article", { name: "集群 科研集群" })
  denied = true
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "刷新工作台" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("授权范围已变化")
  expect(
    screen.queryByRole("article", { name: "集群 科研集群" })
  ).not.toBeInTheDocument()
  expect(screen.queryByLabelText("本页集群统计")).not.toBeInTheDocument()
})
it("迟到的旧筛选响应不覆盖当前工作台", async () => {
  let release: (() => void) | undefined,
    delay = false
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.includes("q=beta"))
      return {
        ...fixtureFleetPage(),
        items: [fixtureFleetItem("beta", "备用集群")],
      } as T
    if (delay)
      await new Promise<void>((resolve) => {
        release = resolve
      })
    return fixtureFleetPage() as T
  })
  show()
  await screen.findByRole("article", { name: "集群 科研集群" })
  delay = true
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "刷新工作台" }))
  await user.type(screen.getByLabelText("搜索集群"), "beta")
  await user.click(screen.getByRole("button", { name: "查询集群" }))
  await screen.findByRole("article", { name: "集群 备用集群" })
  release?.()
  await waitFor(() =>
    expect(
      screen.queryByRole("article", { name: "集群 科研集群" })
    ).not.toBeInTheDocument()
  )
})
it("空授权范围不显示伪造集群，页码越界可回到第一页", async () => {
  vi.mocked(api).mockResolvedValue({
    ...fixtureFleetPage(),
    items: [],
    totalItems: 0,
  })
  show("/fleet?page=9")
  expect(await screen.findByText("没有符合条件的可访问集群")).toBeVisible()
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "返回第一页" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=1")
  )
})
