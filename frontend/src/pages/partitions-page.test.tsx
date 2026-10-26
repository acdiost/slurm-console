import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import {
  fixturePartition,
  fixtureDetail,
} from "@/features/partitions/fixtures.test-support"
import { fixtureNode } from "@/features/nodes/fixtures.test-support"
import { PartitionsPage } from "./partitions-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const inventory = { items: [cluster], page: 1, perPage: 20, totalItems: 1 }
const page = {
  items: [fixturePartition()],
  page: 1,
  perPage: 20,
  totalItems: 25,
  fetchedAt: "2026-10-26T08:00:00Z",
  cached: false,
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  useAuth.setState({ status: "authenticated", user: null, message: "" })
  vi.mocked(api).mockReset()
})
afterEach(() => client.clear())
function RouteState() {
  return <output aria-label="当前查询">{useLocation().search}</output>
}
function show(path = "/partitions?cluster=alpha") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <PartitionsPage />
        <RouteState />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock() {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string) =>
      (path.includes("/partitions?")
        ? page
        : path.endsWith("/partitions/normal")
          ? fixtureDetail()
          : inventory) as T
  )
}
it("服务端分页、名称筛选和排序同步 URL", async () => {
  mock()
  show()
  await screen.findByRole("button", { name: "normal" })
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "下一页分区" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=2")
  )
  await user.type(screen.getByLabelText("搜索分区"), "normal")
  await user.click(screen.getByRole("button", { name: "应用筛选" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("q=normal")
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=1")
  await user.click(
    await screen.findByRole("button", { name: "按最长时限排序" })
  )
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("sort=maxTime")
  )
})
it("仅分区权限时展示约束，不读取关联节点或推断零资源", async () => {
  mock()
  show("/partitions?cluster=alpha&partition=normal")
  await screen.findByText("未授予节点读取权限，仅展示分区配置容量。")
  expect(screen.getByText("不限（配置值 0）")).toBeVisible()
  expect(screen.getAllByText("未设置/未返回").length).toBeGreaterThan(0)
  expect(
    screen.queryByRole("link", { name: /查看分区作业/ })
  ).not.toBeInTheDocument()
  expect(
    vi.mocked(api).mock.calls.some(([path]) => path.includes("/nodes"))
  ).toBe(false)
})
it("资源覆盖缺失不显示假零，关联节点支持独立分页和跳转", async () => {
  const node = fixtureNode(),
    detail = fixtureDetail()
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.includes("/nodes?"))
      return {
        items: [node],
        page: 1,
        perPage: 10,
        totalItems: 12,
        fetchedAt: page.fetchedAt,
        cached: false,
      } as T
    if (path.endsWith("/partitions/normal"))
      return {
        ...detail,
        resources: {
          status: "ok",
          code: "",
          message: "",
          fetchedAt: page.fetchedAt,
          cached: false,
          item: {
            configuredNodes: 2,
            complete: false,
            summary: {
              ...node.resources,
              total: 0,
              idle: 0,
              allocated: 0,
              mixed: 0,
              unavailable: 0,
              states: [],
              issues: [],
            },
          },
        },
      } as T
    return (path.includes("/partitions?") ? page : inventory) as T
  })
  show("/partitions?cluster=alpha&partition=normal")
  expect(
    await screen.findByText("未取得可见关联节点，无法统计其资源。")
  ).toBeVisible()
  expect(screen.getByText(/资源统计不完整/)).toBeVisible()
  const link = await screen.findByRole("link", { name: "node1" })
  expect(link).toHaveAttribute(
    "href",
    "/nodes?cluster=alpha&partition=normal&node=node1"
  )
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "下一页关联节点" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("nodesPage=2")
  )
})
it("刷新被拒绝时隐藏已有分区，不继续展示缓存", async () => {
  let denied = false
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.includes("/partitions?")) {
      if (denied) throw new ClientResponseError({ status: 403 })
      return page as T
    }
    return inventory as T
  })
  show()
  await screen.findByRole("button", { name: "normal" })
  denied = true
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "刷新分区" }))
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "你没有读取该集群分区的权限"
  )
  expect(
    screen.queryByRole("button", { name: "normal" })
  ).not.toBeInTheDocument()
})
it("无授权集群时不会构造分区请求", async () => {
  vi.mocked(api).mockResolvedValue({ ...inventory, items: [], totalItems: 0 })
  show("/partitions")
  await screen.findByText("本页没有可访问的集群")
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "刷新分区" }))
  expect(
    vi.mocked(api).mock.calls.some(([path]) => path.includes("/partitions"))
  ).toBe(false)
})
