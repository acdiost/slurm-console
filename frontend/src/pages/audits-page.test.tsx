import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { fixtureAudit } from "@/features/audits/fixtures.test-support"
import { AuditsPage } from "./audits-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const page = {
  items: [fixtureAudit()],
  page: 1,
  perPage: 20,
  totalItems: 25,
  fetchedAt: "2026-10-26T10:00:00Z",
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  useAuth.setState({ status: "authenticated", user: null, message: "" })
})
afterEach(() => {
  client.clear()
  vi.restoreAllMocks()
})
function Location() {
  return <output aria-label="当前查询">{useLocation().search}</output>
}
function show(path = "/audits") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <AuditsPage />
        <Location />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock() {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string) =>
      (path.startsWith("/operation-audits?")
        ? page
        : { item: fixtureAudit(), fetchedAt: page.fetchedAt }) as T
  )
}
it("服务端分页、筛选及时间排序同步 URL", async () => {
  mock()
  show()
  await screen.findByText("历史操作人")
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "下一页审计" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=2")
  )
  await user.type(screen.getByLabelText("搜索审计"), "request-one")
  await user.type(screen.getByLabelText("集群 ID"), "cluster-old")
  await user.click(screen.getByRole("button", { name: "应用筛选" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("q=request-one")
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=1")
  expect(screen.getByLabelText("当前查询")).toHaveTextContent(
    "cluster=cluster-old"
  )
  await user.click(
    await screen.findByRole("button", { name: "按创建时间排序" })
  )
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("order=asc")
  )
})
it("审计权限不依赖集群读取，详情显示历史快照和未完成意图，不调用 Slurm", async () => {
  mock()
  show("/audits?operation=op-one")
  expect(
    await screen.findByRole("heading", { name: "操作审计详情" })
  ).toBeVisible()
  expect(
    await screen.findByText(
      "持久状态为 processing，不能据此判断原进程仍在执行。"
    )
  ).toBeVisible()
  expect(
    screen.getByText("安排维护 <script>not executable</script>")
  ).toBeVisible()
  expect(
    screen.queryByRole("link", { name: /查看当前资源/ })
  ).not.toBeInTheDocument()
  expect(
    vi
      .mocked(api)
      .mock.calls.every(([path]) => path.startsWith("/operation-audits"))
  ).toBe(true)
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "按此操作人筛选" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent(
      "actor=actor-old"
    )
  )
  expect(
    screen.queryByRole("heading", { name: "操作审计详情" })
  ).not.toBeInTheDocument()
})
it("刷新收到 403 会隐藏旧列表和打开的详情，禁用导出", async () => {
  let denied = false
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (denied) throw new ClientResponseError({ status: 403 })
    return (
      path.includes("?")
        ? page
        : { item: fixtureAudit(), fetchedAt: page.fetchedAt }
    ) as T
  })
  show()
  await screen.findByText("历史操作人")
  denied = true
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "刷新审计" }))
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "你没有查看平台审计的权限"
  )
  expect(screen.queryByText("历史操作人")).not.toBeInTheDocument()
  expect(
    screen.queryByRole("button", { name: "导出当前页 JSON" })
  ).not.toBeInTheDocument()
})
it("详情刷新失败后隐藏旧原因和状态", async () => {
  let denied = false
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.includes("?")) return page as T
    if (denied) throw new ClientResponseError({ status: 403 })
    return { item: fixtureAudit(), fetchedAt: page.fetchedAt } as T
  })
  show("/audits?operation=op-one")
  await screen.findByText("安排维护 <script>not executable</script>")
  denied = true
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "刷新审计详情" }))
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "你没有查看平台审计的权限"
  )
  expect(
    screen.queryByText("安排维护 <script>not executable</script>")
  ).not.toBeInTheDocument()
})
it("空页可返回首页，读取失败与没有记录分别展示", async () => {
  vi.mocked(api).mockResolvedValue({ ...page, items: [] })
  show("/audits?page=10")
  expect(await screen.findByText("本页没有审计记录")).toBeVisible()
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "返回第一页" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=1")
  )
})
it("当前页导出使用已读取的安全 DTO，不查询额外记录", async () => {
  mock()
  const create = vi.fn().mockReturnValue("blob:test-audit"),
    revoke = vi.fn()
  Object.defineProperty(URL, "createObjectURL", {
    configurable: true,
    value: create,
  })
  Object.defineProperty(URL, "revokeObjectURL", {
    configurable: true,
    value: revoke,
  })
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(() => {})
  show()
  await screen.findByText("历史操作人")
  const count = vi.mocked(api).mock.calls.length
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "导出当前页 JSON" }))
  expect(create).toHaveBeenCalledOnce()
  expect(click).toHaveBeenCalledOnce()
  expect(revoke).toHaveBeenCalledWith("blob:test-audit")
  expect(vi.mocked(api).mock.calls).toHaveLength(count)
})
