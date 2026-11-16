import { beforeEach, afterEach, expect, it, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { api } from "@/services/api"
import { cluster } from "@/features/overview/fixtures.test-support"
import {
  ClusterPicker,
  ClusterPickerNavigationProvider,
  useAccessibleClusters,
} from "./cluster-picker"
import { ClusterPickerTestLayout } from "./cluster-picker.test-support"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const beta = { ...cluster, id: "beta", name: "备用集群" },
  switched = vi.fn()
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  switched.mockReset()
})
afterEach(() => client.clear())
function Harness({ legacy = false }: { legacy?: boolean }) {
  const selection = useAccessibleClusters(legacy)
  return (
    <>
      <ClusterPicker selection={selection} label="作业" onSwitch={switched} />
      <output aria-label="选中集群">{selection.id}</output>
      <output aria-label="当前查询">{useLocation().search}</output>
    </>
  )
}
function show(path: string, legacy = false, navigation = true) {
  const content = <Harness legacy={legacy} />
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        {navigation ? (
          <ClusterPickerTestLayout>{content}</ClusterPickerTestLayout>
        ) : (
          <ClusterPickerNavigationProvider>
            {content}
          </ClusterPickerNavigationProvider>
        )}
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock() {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path === "/clusters/alpha") return cluster as T
    if (path === "/clusters/beta") return beta as T
    return {
      items:
        path.includes("q=beta") || path.includes("page=2") ? [beta] : [cluster],
      page: path.includes("page=2") ? 2 : 1,
      perPage: 20,
      totalItems: path.includes("q=beta") ? 1 : 21,
    } as T
  })
}
it("顶部导航展开候选集群，浏览分页和搜索保留当前集群及作业状态，选择后才清空", async () => {
  mock()
  show("/jobs?cluster=alpha&job=42&operation=old&q=training")
  const user = userEvent.setup()
  const trigger = await screen.findByRole("button", {
    name: `选择作业集群：${cluster.name}`,
  })
  expect(
    within(screen.getByRole("banner")).getByRole("button", {
      name: `选择作业集群：${cluster.name}`,
    })
  ).toBe(trigger)
  expect(screen.queryByLabelText("搜索作业可选集群")).not.toBeInTheDocument()
  await user.click(trigger)
  await screen.findByText("21 个匹配集群", { exact: false })
  await user.click(screen.getByRole("button", { name: "下一组作业集群" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("clusterPage=2")
  )
  await screen.findByRole("option", { name: "备用集群" })
  expect(screen.getByLabelText("选中集群")).toHaveTextContent("alpha")
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("job=42")
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("operation=old")
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("q=training")
  await user.type(screen.getByLabelText("搜索作业可选集群"), "beta")
  await user.click(screen.getByRole("button", { name: "搜索集群" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("clusterQ=beta")
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("clusterPage=1")
  expect(screen.getByLabelText("选中集群")).toHaveTextContent("alpha")
  expect(
    screen.getByRole("button", {
      name: `选择作业集群：${cluster.name}`,
    })
  ).toBeInTheDocument()
  expect(
    screen.queryByRole("option", { name: cluster.name })
  ).not.toBeInTheDocument()
  expect(switched).not.toHaveBeenCalled()
  await user.click(await screen.findByRole("option", { name: "备用集群" }))
  await waitFor(() =>
    expect(screen.getByLabelText("选中集群")).toHaveTextContent("beta")
  )
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("job=")
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("operation=")
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("q=training")
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("clusterQ=beta")
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument()
  expect(switched).toHaveBeenCalledOnce()
})
it("明确 URL 指定的页外集群显示在触发器中，候选列表只显示当前匹配项", async () => {
  mock()
  show("/jobs?cluster=beta&job=42&q=training")
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole("button", { name: "选择作业集群：备用集群" })
  )
  expect(
    await screen.findByRole("option", { name: cluster.name })
  ).toBeInTheDocument()
  expect(
    screen.queryByRole("option", { name: "备用集群" })
  ).not.toBeInTheDocument()
  expect(screen.getByLabelText("选中集群")).toHaveTextContent("beta")
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("job=42")
  expect(switched).not.toHaveBeenCalled()
})
it("重新选择当前集群保留业务筛选、详情和操作状态，不触发切换回调", async () => {
  mock()
  const path = "/jobs?cluster=alpha&job=42&operation=old&q=training&page=4"
  show(path)
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole("button", { name: `选择作业集群：${cluster.name}` })
  )
  await user.click(await screen.findByRole("option", { name: cluster.name }))
  expect(screen.getByLabelText("当前查询").textContent).toBe(
    path.slice(path.indexOf("?"))
  )
  expect(switched).not.toHaveBeenCalled()
})
it("键盘打开后 Escape 关闭下拉框并将焦点返回当前集群", async () => {
  mock()
  show("/jobs?cluster=alpha")
  const user = userEvent.setup()
  const trigger = await screen.findByRole("button", {
    name: `选择作业集群：${cluster.name}`,
  })
  trigger.focus()
  await user.keyboard("{Enter}")
  expect(await screen.findByLabelText("搜索作业可选集群")).toBeVisible()
  await user.keyboard("{Escape}")
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument()
  expect(trigger).toHaveFocus()
  expect(switched).not.toHaveBeenCalled()
})
it("键盘浏览候选项不切换，按 Enter 后才更新当前集群", async () => {
  vi.mocked(api).mockResolvedValue({
    items: [cluster, beta],
    page: 1,
    perPage: 20,
    totalItems: 2,
  })
  show("/jobs?cluster=alpha&job=42")
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole("button", { name: `选择作业集群：${cluster.name}` })
  )
  const current = await screen.findByRole("option", { name: cluster.name })
  expect(current).toHaveAttribute("aria-selected", "true")
  current.focus()
  await user.keyboard("{ArrowDown}")
  expect(screen.getByRole("option", { name: "备用集群" })).toHaveFocus()
  expect(screen.getByLabelText("选中集群")).toHaveTextContent("alpha")
  expect(switched).not.toHaveBeenCalled()
  await user.keyboard("{Enter}")
  await waitFor(() =>
    expect(screen.getByLabelText("选中集群")).toHaveTextContent("beta")
  )
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("job=")
  expect(switched).toHaveBeenCalledOnce()
})
it("候选请求等待时显示读取状态，不提供虚假的可选集群", async () => {
  vi.mocked(api).mockReturnValue(Promise.race([]))
  show("/jobs")
  await userEvent.click(screen.getByRole("button", { name: /^选择作业集群/ }))
  expect(
    await within(
      screen.getByRole("dialog", { name: "作业集群选择" })
    ).findByRole("status")
  ).toBeVisible()
  expect(screen.queryByRole("option")).not.toBeInTheDocument()
})
it("无可访问集群时保留可打开的空状态，不触发切换", async () => {
  vi.mocked(api).mockResolvedValue({
    items: [],
    page: 1,
    perPage: 20,
    totalItems: 0,
  })
  show("/jobs")
  await userEvent.click(screen.getByRole("button", { name: /^选择作业集群/ }))
  await waitFor(() =>
    expect(
      within(screen.getByRole("dialog", { name: "作业集群选择" })).getByRole(
        "status"
      )
    ).toBeVisible()
  )
  expect(screen.queryByRole("option")).not.toBeInTheDocument()
  expect(screen.getByLabelText("选中集群")).toBeEmptyDOMElement()
  expect(switched).not.toHaveBeenCalled()
})
it("搜索无匹配时保留当前集群和业务筛选，可以清除集群搜索", async () => {
  const fallback = async <T,>(path: string) => {
    if (path === "/clusters/alpha") return cluster as T
    return {
      items: path.includes("q=missing") ? [] : [cluster],
      page: 1,
      perPage: 20,
      totalItems: path.includes("q=missing") ? 0 : 1,
    } as T
  }
  vi.mocked(api).mockImplementation(fallback)
  show("/jobs?cluster=alpha&q=training")
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole("button", { name: `选择作业集群：${cluster.name}` })
  )
  await user.type(screen.getByLabelText("搜索作业可选集群"), "missing")
  await user.click(screen.getByRole("button", { name: "搜索集群" }))
  await waitFor(() =>
    expect(
      within(screen.getByRole("dialog", { name: "作业集群选择" })).getByRole(
        "status"
      )
    ).toBeVisible()
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("q=training")
  expect(screen.getByLabelText("选中集群")).toHaveTextContent("alpha")
  await user.click(screen.getByRole("button", { name: "清除集群搜索" }))
  expect(
    await screen.findByRole("option", { name: cluster.name })
  ).toBeInTheDocument()
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("clusterQ=")
  expect(switched).not.toHaveBeenCalled()
})
it("集群列表失败显示可重试错误，重试恢复候选但不改变当前集群", async () => {
  vi.mocked(api).mockRejectedValue(new Error("unavailable"))
  show("/jobs")
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: /^选择作业集群/ }))
  expect(await screen.findByRole("alert")).toHaveTextContent("无法读取可选集群")
  expect(screen.queryByRole("option")).not.toBeInTheDocument()
  mock()
  await user.click(screen.getByRole("button", { name: "重试" }))
  expect(
    await screen.findByRole("option", { name: cluster.name })
  ).toBeInTheDocument()
  expect(switched).not.toHaveBeenCalled()
})
it("未挂载导航目标时不回退到业务页内的集群卡片", async () => {
  mock()
  show("/jobs?cluster=alpha", false, false)
  await waitFor(() =>
    expect(screen.getByLabelText("选中集群")).toHaveTextContent("alpha")
  )
  expect(
    screen.queryByRole("button", { name: /^选择作业集群/ })
  ).not.toBeInTheDocument()
  expect(screen.queryByLabelText("搜索作业可选集群")).not.toBeInTheDocument()
})
it("旧总览分页链接会规范化为明确集群，不依赖进入其他业务页时的默认选择", async () => {
  mock()
  show("/overview?page=2", true)
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("cluster=beta")
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("clusterPage=2")
  expect(screen.getByLabelText("当前查询").textContent).not.toMatch(/[?&]page=/)
})
