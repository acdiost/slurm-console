import { beforeEach, afterEach, expect, it, vi } from "vitest"
import { render, screen, waitFor, within, act } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, useLocation } from "react-router"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import { cluster } from "@/features/overview/fixtures.test-support"
import { LinuxUsersPage } from "./linux-users-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const metadata = {
  fetchedAt: "2026-10-27T03:00:00Z",
  cached: false,
  sourceLabel: "Alpha NSS",
  enumeration: "nss",
  uidMin: 1000,
  revision: 1,
}
const worker = {
  name: "worker",
  uid: 1001,
  gid: 1001,
  comment: "Local worker",
  home: "/home/worker",
  shell: "/bin/bash",
  source: "local",
  kind: "regular",
}
function page(items = [worker]) {
  return { ...metadata, items, page: 1, perPage: 20, totalItems: 21 }
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
})
afterEach(() => client.clear())
function Location() {
  return <output aria-label="当前查询">{useLocation().search}</output>
}
function show(path = "/linux-users?cluster=alpha") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <LinuxUsersPage />
        <Location />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mockSources(source: (path: string) => unknown | Promise<unknown>) {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.endsWith("/linux-user-controls"))
      return {
        enabled: false,
        actions: [],
        minUid: 0,
        maxUid: 0,
        message: "此来源未启用本地管理。",
      } as T
    if (path.startsWith("/clusters?"))
      return { items: [cluster], page: 1, perPage: 20, totalItems: 1 } as T
    if (path === "/clusters/alpha") return cluster as T
    return (await source(path)) as T
  })
}
it("枚举查询分页、筛选和排序使用 URL，明确显示来源及 UID 范围", async () => {
  mockSources(() => page())
  show()
  const user = userEvent.setup()
  const table = await screen.findByRole("region", { name: "Linux 用户列表" })
  expect(within(table).getByText("worker")).toBeVisible()
  expect(within(table).getByText("本地匹配")).toBeVisible()
  expect(within(table).getByText(/Alpha NSS/)).toHaveTextContent("UID_MIN 1000")
  await user.click(screen.getByRole("button", { name: "下一页 Linux 用户" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=2")
  )
  await user.type(screen.getByLabelText("搜索枚举结果"), "worker")
  await user.click(screen.getByRole("combobox", { name: "身份来源" }))
  await user.click(await screen.findByRole("option", { name: "本地匹配" }))
  await user.click(screen.getByRole("button", { name: "查询用户" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("source=local")
  )
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("q=worker")
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=1")
  await user.click(
    await screen.findByRole("button", { name: "切换 Linux 用户排序方向" })
  )
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("order=desc")
  )
})
it("未枚举的目录用户可按完整用户名打开详情，路径被编码", async () => {
  mockSources((path) =>
    path.includes("/linux-users/")
      ? {
          ...metadata,
          enumeration: "lookup",
          item: {
            ...worker,
            name: "directory@realm",
            source: "nss",
            home: "/directory/home",
          },
        }
      : page()
  )
  show()
  const user = userEvent.setup()
  await screen.findByText("worker")
  await user.type(screen.getByLabelText("精确查询用户名"), "directory@realm")
  await user.click(screen.getByRole("button", { name: "查询用户详情" }))
  const dialog = await screen.findByRole("dialog")
  expect(await within(dialog).findByText("/directory/home")).toBeVisible()
  expect(within(dialog).getByText("NSS 非本地匹配")).toBeVisible()
  expect(
    vi
      .mocked(api)
      .mock.calls.some(
        ([path]) => path === "/clusters/alpha/linux-users/directory%40realm"
      )
  ).toBe(true)
  expect(screen.getByLabelText("当前查询")).toHaveTextContent(
    "username=directory%40realm"
  )
})
it("刷新失去权限后清除旧列表和来源信息", async () => {
  let denied = false
  mockSources(() => {
    if (denied) throw new ClientResponseError({ status: 403 })
    return page()
  })
  show()
  await screen.findByText("worker")
  denied = true
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "刷新用户" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("没有此集群")
  expect(
    screen.queryByRole("region", { name: "Linux 用户列表" })
  ).not.toBeInTheDocument()
  expect(screen.queryByText("worker", { exact: true })).not.toBeInTheDocument()
})
it("详情刷新不存在后不显示上次的身份记录", async () => {
  let gone = false
  mockSources((path) => {
    if (path.includes("/linux-users/")) {
      if (gone) throw new ClientResponseError({ status: 404 })
      return { ...metadata, enumeration: "lookup", item: worker }
    }
    return page()
  })
  show("/linux-users?cluster=alpha&username=worker")
  const dialog = await screen.findByRole("dialog")
  await within(dialog).findByText("/home/worker")
  gone = true
  await userEvent
    .setup()
    .click(within(dialog).getByRole("button", { name: "刷新用户详情" }))
  expect(await within(dialog).findByRole("alert")).toHaveTextContent(
    "用户不存在"
  )
  expect(within(dialog).queryByText("/home/worker")).not.toBeInTheDocument()
})
it("旧筛选的迟到结果不能覆盖新查询", async () => {
  let finish: (value: unknown) => void = () => {}
  const delayed = new Promise((resolve) => {
    finish = resolve
  })
  mockSources((path) =>
    path.includes("q=old") ? delayed : page([{ ...worker, name: "newuser" }])
  )
  show("/linux-users?cluster=alpha&q=old")
  const user = userEvent.setup()
  await user.clear(screen.getByLabelText("搜索枚举结果"))
  await user.type(screen.getByLabelText("搜索枚举结果"), "new")
  await user.click(screen.getByRole("button", { name: "查询用户" }))
  await screen.findByText("newuser")
  await act(async () => {
    finish(page([{ ...worker, name: "olduser" }]))
    await delayed
  })
  expect(screen.queryByText("olduser")).not.toBeInTheDocument()
  expect(screen.getByText("newuser")).toBeVisible()
})
it("无数据与未知 UID_MIN 保持未知，不假定普通用户范围", async () => {
  mockSources(() => ({ ...page([]), totalItems: 0, uidMin: null }))
  show()
  expect(await screen.findByText("本页没有匹配的枚举记录")).toBeVisible()
  expect(screen.getByText(/UID_MIN 未知/)).toBeVisible()
  expect(screen.getByText(/目录服务可能关闭枚举/)).toBeVisible()
})
