import { beforeEach, afterEach, expect, it, vi } from "vitest"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation, useNavigate } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { ClusterPickerTestLayout } from "@/features/clusters/cluster-picker.test-support"
import {
  fixtureJob,
  fixturePrepared,
  fixtureOperation,
} from "@/features/jobs/fixtures.test-support"
import { JobsPage } from "./jobs-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const beta = { ...cluster, id: "beta", name: "备用集群" }
const inventory = {
  items: [cluster, beta],
  page: 1,
  perPage: 20,
  totalItems: 2,
}
const jobs = {
  items: [fixtureJob()],
  page: 1,
  perPage: 20,
  totalItems: 25,
  fetchedAt: "2026-10-25T14:00:00Z",
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
  const location = useLocation()
  const navigate = useNavigate()
  return (
    <>
      <output aria-label="当前查询">{location.search}</output>
      <button onClick={() => navigate("/jobs?cluster=beta")}>
        切换测试集群
      </button>
    </>
  )
}
function show(path = "/jobs?cluster=alpha") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <ClusterPickerTestLayout>
          <JobsPage />
          <RouteState />
        </ClusterPickerTestLayout>
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock() {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.startsWith("/clusters?"))
      return {
        ...inventory,
        items:
          path.includes("page=2") || path.includes("q=beta")
            ? [beta]
            : inventory.items,
        page: path.includes("page=2") ? 2 : 1,
        totalItems: path.includes("q=beta") ? 1 : 21,
      } as T
    return (
      path.includes("/jobs?")
        ? jobs
        : path.endsWith("/jobs/42")
          ? { item: fixtureJob(), fetchedAt: jobs.fetchedAt, cached: false }
          : path === "/clusters/beta"
            ? beta
            : cluster
    ) as T
  })
}
it("筛选、排序和分页写入 URL 并发送给服务端，Viewer 隐藏取消操作", async () => {
  mock()
  show()
  expect(await screen.findByText("console-qa")).toBeVisible()
  expect(
    screen.queryByRole("button", { name: "取消作业 #42" })
  ).not.toBeInTheDocument()
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "下一页作业" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=2")
  )
  await user.type(screen.getByLabelText("账户"), "research")
  await user.click(screen.getByRole("button", { name: "应用筛选" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent(
      "account=research"
    )
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=1")
  await user.click(await screen.findByRole("button", { name: "按 CPU 排序" }))
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.some(
          ([path]) =>
            path.includes("account=research") && path.includes("sort=cpus")
        )
    ).toBe(true)
  )
})
it("详情 URL 展示资源、QOS、状态原因与时间，关闭保留筛选", async () => {
  mock()
  show("/jobs?cluster=alpha&account=research&job=42")
  expect(await screen.findByRole("heading", { name: "作业 #42" })).toBeVisible()
  expect(await screen.findByText("128 MiB")).toBeVisible()
  expect(screen.getAllByText("JobHeldUser").length).toBeGreaterThan(0)
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "Close" }))
  expect(screen.getByLabelText("当前查询")).toHaveTextContent(
    "account=research"
  )
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("job=")
})
it("403 刷新隐藏旧作业，切换集群不沿用旧数据", async () => {
  let denied = false
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.includes("/jobs?")) {
      if (denied) throw new ClientResponseError({ status: 403 })
      return jobs as T
    }
    return inventory as T
  })
  show()
  await screen.findByText("console-qa")
  denied = true
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "刷新作业" }))
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "你没有读取或操作该集群作业的权限"
  )
  expect(screen.queryByText("console-qa")).not.toBeInTheDocument()
  await user.click(screen.getByRole("button", { name: "切换测试集群" }))
  expect(screen.queryByText("console-qa")).not.toBeInTheDocument()
})
it("没有可访问集群时不发起作业请求，手动刷新也不会构造空集群路径", async () => {
  vi.mocked(api).mockResolvedValue({ ...inventory, items: [], totalItems: 0 })
  show("/jobs")
  expect(await screen.findByText("本页没有可访问的集群")).toBeVisible()
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "刷新作业" }))
  expect(
    vi.mocked(api).mock.calls.some(([path]) => path.includes("/jobs"))
  ).toBe(false)
})

it("列表直接取消时只打开一个确认层，关闭后展示详情及持久结果", async () => {
  useAuth.setState({
    user: {
      id: "actor",
      email: "actor@example.com",
      name: "Actor",
      username: "",
      authSource: "local",
      access: {
        platformRoles: ["super_admin"],
        platformPermissions: ["jobs.cancel"],
        clusters: [],
      },
    },
  })
  vi.mocked(api).mockImplementation(
    async <T,>(path: string) =>
      (path.endsWith("cancel-context")
        ? fixturePrepared()
        : path.endsWith("/cancel") || path.includes("job-operations")
          ? fixtureOperation()
          : path.includes("/jobs?")
            ? jobs
            : path.endsWith("/jobs/42")
              ? { item: fixtureJob(), fetchedAt: jobs.fetchedAt, cached: false }
              : inventory) as T
  )
  show()
  const user = userEvent.setup()
  await user.click(await screen.findByRole("button", { name: "取消作业 #42" }))
  await screen.findByRole("heading", { name: "取消作业 #42" })
  expect(
    screen.queryByRole("heading", { name: "作业 #42" })
  ).not.toBeInTheDocument()
  await user.type(await screen.findByLabelText("取消原因"), "功能验证")
  await user.type(screen.getByLabelText("输入 Job ID 42 以确认"), "42")
  await user.click(screen.getByRole("button", { name: "确认取消作业" }))
  await screen.findByText("已核实取消")
  await user.click(screen.getByRole("button", { name: "关闭" }))
  expect(await screen.findByRole("heading", { name: "作业 #42" })).toBeVisible()
  expect(await screen.findByText("取消操作记录")).toBeVisible()
})
it("集群路由切换丢弃未提交的取消确认，不能向新集群提交旧作业操作", async () => {
  useAuth.setState({
    user: {
      id: "actor",
      email: "actor@example.test",
      name: "Actor",
      username: "actor",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: ["jobs.cancel"],
        clusters: [],
      },
    },
  })
  mock()
  const fallback = vi.mocked(api).getMockImplementation()!
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.endsWith("cancel-context")) return fixturePrepared() as T
    return fallback(path) as Promise<T>
  })
  show()
  const user = userEvent.setup()
  await user.click(await screen.findByRole("button", { name: "取消作业 #42" }))
  await user.type(await screen.findByLabelText("取消原因"), "旧集群的取消理由")
  await user.type(screen.getByLabelText("输入 Job ID 42 以确认"), "42")
  expect(screen.getByRole("button", { name: "确认取消作业" })).toBeEnabled()
  // A route change can arrive from browser history while the modal owns focus.
  fireEvent.click(screen.getByText("切换测试集群"))
  await waitFor(() =>
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument()
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("cluster=beta")
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("job=")
  expect(screen.queryByLabelText("取消原因")).not.toBeInTheDocument()
  expect(
    vi.mocked(api).mock.calls.some(([path]) => path.endsWith("/cancel"))
  ).toBe(false)
})

it("批量选择仅限当前页，翻页和切换集群后清空", async () => {
  useAuth.setState({
    user: {
      id: "actor",
      email: "actor@example.test",
      name: "Actor",
      username: "alice",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: [],
        clusters: [
          { cluster, roles: [], permissions: ["jobs.read", "jobs.cancel"] },
        ],
      },
    },
  })
  mock()
  show("/jobs?cluster=alpha&account=research")
  await screen.findByText("console-qa")
  const select = screen.getByRole("checkbox", { name: "选择作业 #42" })
  await userEvent.click(select)
  expect(screen.getByText("已选 1 / 10 · 仅本页选择")).toBeInTheDocument()
  expect(screen.getByRole("button", { name: "批量取消作业" })).toBeEnabled()
  await userEvent.click(screen.getByRole("button", { name: /^选择作业集群/ }))
  await userEvent.click(screen.getByRole("button", { name: "下一组作业集群" }))
  await screen.findByRole("option", { name: "备用集群" })
  expect(screen.getByText("已选 1 / 10 · 仅本页选择")).toBeInTheDocument()
  expect(screen.getByLabelText("当前查询")).toHaveTextContent(
    "account=research"
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("cluster=alpha")
  await userEvent.type(screen.getByLabelText("搜索作业可选集群"), "beta")
  await userEvent.click(screen.getByRole("button", { name: "搜索集群" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("clusterQ=beta")
  )
  expect(screen.getByText("已选 1 / 10 · 仅本页选择")).toBeInTheDocument()
  expect(screen.getByLabelText("当前查询")).toHaveTextContent(
    "account=research"
  )
  await userEvent.click(screen.getByRole("button", { name: "清除集群搜索" }))
  await userEvent.click(
    await screen.findByRole("option", { name: cluster.name })
  )
  expect(screen.getByText("已选 1 / 10 · 仅本页选择")).toBeInTheDocument()
  expect(screen.getByLabelText("当前查询")).toHaveTextContent(
    "account=research"
  )
  await userEvent.click(screen.getByRole("button", { name: "下一页作业" }))
  expect(screen.getByText("已选 0 / 10 · 仅本页选择")).toBeInTheDocument()
  expect(screen.getByRole("button", { name: "批量取消作业" })).toBeDisabled()
  await userEvent.click(screen.getByRole("button", { name: "上一页作业" }))
  expect(screen.getByText("已选 0 / 10 · 仅本页选择")).toBeInTheDocument()
  await userEvent.click(screen.getByRole("checkbox", { name: "选择作业 #42" }))
  expect(screen.getByText("已选 1 / 10 · 仅本页选择")).toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: /^选择作业集群/ }))
  await userEvent.click(await screen.findByRole("option", { name: "备用集群" }))
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("cluster=beta")
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("account=")
  expect(screen.getByLabelText("当前查询").textContent).not.toMatch(/[?&]page=/)
  expect(
    screen.queryByRole("checkbox", { name: "选择作业 #42" })
  ).not.toBeInTheDocument()
})
