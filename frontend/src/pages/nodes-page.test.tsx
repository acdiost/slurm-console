import { beforeEach, afterEach, expect, it, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation, useNavigate } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import {
  fixtureNode,
  fixtureContext,
  fixtureOperation,
} from "@/features/nodes/fixtures.test-support"
import { NodesPage } from "./nodes-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const inventory = { items: [cluster], page: 1, perPage: 20, totalItems: 1 }
const nodes = {
  items: [fixtureNode()],
  page: 1,
  perPage: 20,
  totalItems: 25,
  fetchedAt: "2026-10-25T16:00:00Z",
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
  const location = useLocation(),
    navigate = useNavigate()
  return (
    <>
      <output aria-label="当前查询">{location.search}</output>
      <button onClick={() => navigate("/nodes?cluster=beta")}>
        切换测试集群
      </button>
    </>
  )
}
function show(path = "/nodes?cluster=alpha") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <NodesPage />
        <RouteState />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock() {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string) =>
      (path.includes("control-context")
        ? fixtureContext()
        : path.includes("/nodes?")
          ? nodes
          : path.endsWith("/nodes/node1")
            ? { item: fixtureNode(), fetchedAt: nodes.fetchedAt, cached: false }
            : inventory) as T
  )
}
function operator() {
  useAuth.setState({
    user: {
      id: "actor",
      email: "actor@example.com",
      name: "Actor",
      username: "",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: ["nodes.drain", "nodes.resume"],
        clusters: [],
      },
    },
  })
}
it("筛选、分页及矩阵视图同步 URL，Viewer 不显示控制入口", async () => {
  mock()
  show()
  expect(await screen.findByRole("button", { name: "node1" })).toBeVisible()
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument()
  expect(
    screen.queryByRole("button", { name: "Drain 节点 node1" })
  ).not.toBeInTheDocument()
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "下一页节点" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=2")
  )
  await user.type(screen.getByLabelText("分区"), "normal")
  await user.click(screen.getByRole("button", { name: "应用筛选" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent(
      "partition=normal"
    )
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=1")
  await user.click(await screen.findByRole("button", { name: "节点状态矩阵" }))
  expect(await screen.findByLabelText("当前页节点状态矩阵")).toBeVisible()
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("view=matrix")
  expect(screen.getByText("矩阵仅展示当前页", { exact: false })).toBeVisible()
})
it("详情显示真实备注、硬件和资源，控制确认层与详情不重叠", async () => {
  operator()
  mock()
  show("/nodes?cluster=alpha&node=node1")
  expect(
    await screen.findByRole("heading", { name: "节点 node1" })
  ).toBeVisible()
  expect(await screen.findByText("待安排维护")).toBeVisible()
  expect(screen.getByText("x86_64")).toBeVisible()
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "Drain 排空" }))
  expect(
    await screen.findByRole("heading", { name: "Drain 排空 · 1 个节点" })
  ).toBeVisible()
  expect(
    screen.queryByRole("heading", { name: "节点 node1" })
  ).not.toBeInTheDocument()
  expect(await screen.findByLabelText("操作原因")).toBeVisible()
})
it("本页批量选择最多十个节点，切换集群不保留选择或旧数据", async () => {
  operator()
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.includes("/nodes?"))
      return {
        ...nodes,
        items: path.includes("beta")
          ? []
          : Array.from({ length: 12 }, (_, i) => fixtureNode(`node${i + 1}`)),
      } as T
    if (path === "/clusters/beta") return { ...cluster, id: "beta" } as T
    return inventory as T
  })
  show()
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole("checkbox", { name: "选择本页前 10 个节点" })
  )
  expect(screen.getByText("已选 10 / 10 · 仅本页选择")).toBeVisible()
  expect(
    screen.getByRole("checkbox", { name: "选择节点 node11" })
  ).toBeDisabled()
  await user.click(screen.getByRole("button", { name: "切换测试集群" }))
  expect(await screen.findByText("已选 0 / 10 · 仅本页选择")).toBeVisible()
  expect(
    screen.queryByRole("button", { name: "node1" })
  ).not.toBeInTheDocument()
})
it("刷新收到 403 时隐藏旧节点，缺少集群时不构造空节点路径", async () => {
  let denied = false
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.includes("/nodes?")) {
      if (denied) throw new ClientResponseError({ status: 403 })
      return nodes as T
    }
    return inventory as T
  })
  show()
  await screen.findByRole("button", { name: "node1" })
  denied = true
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "刷新节点" }))
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "你没有读取或控制该集群节点的权限"
  )
  expect(
    screen.queryByRole("button", { name: "node1" })
  ).not.toBeInTheDocument()
})
it("无授权集群展示空状态，手动刷新不查询空集群", async () => {
  vi.mocked(api).mockResolvedValue({ ...inventory, items: [], totalItems: 0 })
  show("/nodes")
  await screen.findByText("本页没有可访问的集群")
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "刷新节点" }))
  expect(
    vi.mocked(api).mock.calls.some(([path]) => path.includes("/nodes"))
  ).toBe(false)
})

it("操作核实成功后立即刷新节点快照，不继续展示旧的等待注册标记", async () => {
  let ready = false
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.includes("node-operations"))
      return fixtureOperation(
        "node1",
        ready ? "confirmed" : "accepted",
        "resume"
      ) as T
    if (path.includes("/nodes?")) {
      const n = fixtureNode()
      if (!ready) n.states.push("NOT_RESPONDING")
      return { ...nodes, items: [n] } as T
    }
    return inventory as T
  })
  show("/nodes?cluster=alpha&operations=operation-node1-resume")
  expect(
    await within(await screen.findByRole("table")).findByText("NOT_RESPONDING")
  ).toBeVisible()
  await screen.findByText("已受理，待核实")
  ready = true
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "刷新记录" }))
  expect(await screen.findByText("已核实")).toBeVisible()
  await waitFor(() =>
    expect(
      within(screen.getByRole("table")).queryByText("NOT_RESPONDING")
    ).not.toBeInTheDocument()
  )
})
