/// <reference lib="es2024.promise" />
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { act, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { createMemoryRouter, Outlet, RouterProvider } from "react-router"
import { api } from "@/services/api"
import type { SendOptions } from "pocketbase"
import { useAuth } from "@/stores/auth"
import { LinuxUsersPage } from "@/pages/linux-users-page"
import { FreeIPAPage } from "@/pages/freeipa-page"
import { ClusterPickerTestLayout } from "./cluster-picker.test-support"

vi.mock("@/services/api", () => ({ api: vi.fn() }))
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
)
const clusters = [
  {
    id: "alpha",
    name: "PAM cluster",
    slug: "pam",
    description: "",
    authenticationMethod: "pam",
  },
  {
    id: "beta",
    name: "LDAP cluster",
    slug: "ldap",
    description: "",
    authenticationMethod: "ldap",
  },
  {
    id: "gamma",
    name: "FreeIPA cluster",
    slug: "ipa",
    description: "",
    authenticationMethod: "freeipa",
  },
  {
    id: "delta",
    name: "Second FreeIPA cluster",
    slug: "ipa-two",
    description: "",
    authenticationMethod: "freeipa",
  },
] as const
const linuxMetadata = {
  fetchedAt: "2026-11-17T00:00:00Z",
  cached: false,
  sourceLabel: "Identity host",
  enumeration: "nss",
  uidMin: 1000,
  revision: 1,
}
const worker = {
  name: "worker",
  uid: 1001,
  gid: 1001,
  comment: "Worker",
  home: "/home/worker",
  shell: "/bin/bash",
  source: "local",
  kind: "regular",
}
const directoryUser = {
  username: "alice",
  firstName: "Alice",
  lastName: "Example",
  email: "alice@example.test",
  uid: "10001",
  gid: "10001",
  enabled: true,
  passwordExpired: false,
  accountExpired: false,
  groups: ["research"],
  version: "a".repeat(64),
}
let client: QueryClient
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "reader",
      name: "Reader",
      email: "reader@example.test",
      username: "",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: [
          "platform.directory.read",
          "platform.directory.manage",
        ],
        clusters: clusters.map((cluster) => ({
          cluster,
          roles: [],
          permissions: ["clusters.read", "linux_users.read"],
        })),
      },
    },
  })
  vi.mocked(api).mockReset()
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.startsWith("/clusters?"))
      return {
        items: clusters,
        page: 1,
        perPage: 20,
        totalItems: clusters.length,
      } as T
    const cluster = clusters.find((value) => path === `/clusters/${value.id}`)
    if (cluster) return cluster as T
    if (path.endsWith("/linux-user-controls"))
      return {
        enabled: false,
        actions: [],
        minUid: 0,
        maxUid: 0,
        message: "Read only",
      } as T
    if (path.endsWith("/linux-users/worker"))
      return { ...linuxMetadata, enumeration: "lookup", item: worker } as T
    if (path.includes("/linux-users?"))
      return {
        ...linuxMetadata,
        items: [worker],
        page: 1,
        perPage: 20,
        totalItems: 1,
      } as T
    if (path === "/directory")
      return {
        configured: true,
        domain: "example.test",
        realm: "EXAMPLE.TEST",
      } as T
    if (path.startsWith("/directory/users?"))
      return {
        items: [directoryUser],
        page: 1,
        perPage: 20,
        totalItems: 1,
      } as T
    if (path === "/directory/operations") return { items: [] } as T
    throw new Error(`Unexpected request ${path}`)
  })
})
afterEach(() => {
  client.clear()
  useAuth.setState({ user: null, status: "anonymous" })
})
function show(path: string) {
  const router = createMemoryRouter(
    [
      {
        element: (
          <ClusterPickerTestLayout>
            <Outlet />
          </ClusterPickerTestLayout>
        ),
        children: [
          { path: "/linux-users", element: <LinuxUsersPage /> },
          { path: "/directory", element: <FreeIPAPage /> },
        ],
      },
    ],
    { initialEntries: [path] }
  )
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return router
}
it("切换到另一种绑定时清除旧身份详情并进入对应视图", async () => {
  const router = show("/linux-users?cluster=alpha&username=worker&q=old")
  await screen.findByText("/home/worker")
  await act(async () => {
    await router.navigate("/linux-users?cluster=beta")
  })
  await screen.findByText(/身份认证方式：LDAP/)
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  expect(router.state.location.search).not.toContain("username=")
  const user = userEvent.setup()
  await user.click(
    screen.getByRole("button", { name: "选择身份集群：LDAP cluster" })
  )
  await user.click(
    await screen.findByRole("option", { name: "FreeIPA cluster" })
  )
  await screen.findByText("alice")
  expect(router.state.location.pathname).toBe("/directory")
  expect(router.state.location.search).toContain("cluster=gamma")
  expect(
    screen.queryByRole("region", { name: "Linux 用户列表" })
  ).not.toBeInTheDocument()
  expect(
    vi
      .mocked(api)
      .mock.calls.some(([path]) =>
        path.startsWith("/clusters/gamma/linux-users")
      )
  ).toBe(false)
  await user.click(
    screen.getByRole("button", { name: "选择身份集群：FreeIPA cluster" })
  )
  await user.click(
    await screen.findByRole("option", { name: "PAM cluster" })
  )
  await screen.findByText(/身份认证方式：PAM/)
  expect(router.state.location.pathname).toBe("/linux-users")
  expect(screen.queryByText("alice")).not.toBeInTheDocument()
})
it("同为 FreeIPA 的集群切换也丢弃未提交的目录操作确认", async () => {
  const router = show("/directory?cluster=gamma")
  await screen.findByText("alice")
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "停用" }))
  await user.type(
    screen.getByLabelText("操作原因"),
    "Do not carry to another cluster"
  )
  await user.type(screen.getByLabelText("输入 alice 确认"), "alice")
  expect(screen.getByRole("button", { name: "确认执行" })).toBeEnabled()
  await act(async () => {
    await router.navigate("/directory?cluster=delta")
  })
  await screen.findByText("alice")
  expect(screen.queryByLabelText("操作原因")).not.toBeInTheDocument()
  await user.click(screen.getByRole("button", { name: "停用" }))
  expect(screen.getByLabelText("操作原因")).toHaveValue("")
  expect(screen.getByLabelText("输入 alice 确认")).toHaveValue("")
  expect(screen.getByRole("button", { name: "确认执行" })).toBeDisabled()
  expect(
    vi.mocked(api).mock.calls.some(([path]) => path === "/directory/changes")
  ).toBe(false)
})

it("当前集群绑定后台刷新不清空未提交的目录草稿", async () => {
  show("/directory?cluster=gamma")
  await screen.findByText("alice")
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "停用" }))
  await user.type(screen.getByLabelText("操作原因"), "Keep this draft")
  const original = vi.mocked(api).getMockImplementation()!
  const { promise: pending, resolve: finish } = Promise.withResolvers<unknown>()
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: SendOptions) =>
      path === "/clusters/gamma"
        ? (pending as Promise<T>)
        : (original(path, options) as Promise<T>)
  )
  await act(async () => {
    void client.invalidateQueries({ queryKey: ["cluster-metadata", "gamma"] })
  })
  expect(screen.getByLabelText("操作原因")).toHaveValue("Keep this draft")
  await act(async () => {
    finish(clusters[2])
    await pending
  })
  expect(screen.getByLabelText("操作原因")).toHaveValue("Keep this draft")
  expect(
    vi.mocked(api).mock.calls.some(([path]) => path === "/directory/changes")
  ).toBe(false)
})
