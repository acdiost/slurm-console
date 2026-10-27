import { beforeEach, afterEach, expect, it, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { api } from "@/services/api"
import { cluster } from "@/features/overview/fixtures.test-support"
import { ClusterPicker, useAccessibleClusters } from "./cluster-picker"
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
function show(path: string, legacy = false) {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Harness legacy={legacy} />
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
      page: 1,
      perPage: 20,
      totalItems: 21,
    } as T
  })
}
it("浏览候选分页和搜索保持当前集群及作业详情，选择后才清空旧操作状态", async () => {
  mock()
  show("/jobs?cluster=alpha&job=42&operation=old&q=training")
  const user = userEvent.setup()
  await screen.findByText("21 个匹配集群", { exact: false })
  await user.click(screen.getByRole("button", { name: "下一组作业集群" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("clusterPage=2")
  )
  expect(screen.getByLabelText("选中集群")).toHaveTextContent("alpha")
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("job=42")
  await user.type(screen.getByLabelText("搜索作业可选集群"), "beta")
  await user.click(screen.getByRole("button", { name: "搜索集群" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("clusterQ=beta")
  )
  expect(screen.getByLabelText("选中集群")).toHaveTextContent("alpha")
  await user.click(
    await screen.findByRole("combobox", { name: "选择作业集群" })
  )
  await user.click(await screen.findByRole("option", { name: "备用集群" }))
  await waitFor(() =>
    expect(screen.getByLabelText("选中集群")).toHaveTextContent("beta")
  )
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("job=")
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("operation=")
  expect(screen.getByLabelText("当前查询")).not.toHaveTextContent("q=training")
  expect(switched).toHaveBeenCalledOnce()
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
