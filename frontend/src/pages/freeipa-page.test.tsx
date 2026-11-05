import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import type { SendOptions } from "pocketbase"
import { MemoryRouter } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { api } from "@/services/api"
import { useAuth, refreshAccess } from "@/stores/auth"
import { FreeIPAPage } from "./freeipa-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
vi.mock("@/stores/auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/stores/auth")>()),
  refreshAccess: vi.fn().mockResolvedValue(undefined),
}))
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
let denied = false
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  denied = false
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "actor",
      name: "Admin",
      email: "admin@example.test",
      username: "",
      authSource: "local",
      access: {
        platformRoles: ["Super Admin"],
        platformPermissions: [
          "platform.directory.read",
          "platform.directory.manage",
          "platform.roles.manage",
        ],
        clusters: [],
      },
    },
  })
  vi.mocked(api).mockReset()
  vi.mocked(refreshAccess).mockClear()
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path === "/directory")
      return {
        configured: true,
        domain: "example.test",
        realm: "EXAMPLE.TEST",
      } as T
    if (path.startsWith("/directory/users")) {
      if (denied) throw Error("revoked")
      return {
        items: [directoryUser],
        totalItems: 1,
        page: 1,
        perPage: 20,
      } as T
    }
    if (path === "/directory/operations") return { items: [] } as T
    if (path === "/directory/role-mappings") return { items: [] } as T
    if (path === "/platform-role-catalog")
      return {
        roles: [
          { code: "viewer", name: "Viewer", scope: "cluster", permissions: [] },
        ],
        clusters: [{ id: "aaaaaaaaaaaaaaa", name: "科研集群" }],
      } as T
    if (path === "/directory/changes")
      return {
        operationId: "example-operation",
        action: "directory.user.disable",
        target: "alice",
        status: "succeeded",
        message: "已完成目录操作",
        updatedAt: "2026-11-04T00:00:00Z",
      } as T
    return {} as T
  })
})
afterEach(() => client.clear())
function show() {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <FreeIPAPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
it("requires configuration before querying the directory", async () => {
  vi.mocked(api).mockResolvedValue({ configured: false, domain: "", realm: "" })
  show()
  expect(await screen.findByText("尚未接入 FreeIPA")).toBeVisible()
  expect(
    vi.mocked(api).mock.calls.some(([p]) => p.startsWith("/directory/users"))
  ).toBe(false)
})
it("confirms the exact user and submits the server version without passwords", async () => {
  show()
  await screen.findByText("alice")
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "停用" }))
  const submit = screen.getByRole("button", { name: "确认执行" })
  expect(submit).toBeDisabled()
  await user.type(screen.getByLabelText("操作原因"), "停用测试账户")
  await user.type(screen.getByLabelText("输入 alice 确认"), "alice")
  await user.click(submit)
  expect(await screen.findByText("已完成目录操作")).toBeVisible()
  const call = vi
    .mocked(api)
    .mock.calls.find(([p]) => p === "/directory/changes")
  expect(call?.[1]?.body).toMatchObject({
    action: "user.disable",
    target: "alice",
    version: directoryUser.version,
    confirmation: "alice",
    reason: "停用测试账户",
  })
  expect(call?.[1]?.body).not.toHaveProperty("password")
})
it("hides directory users when a refreshed query is denied", async () => {
  show()
  await screen.findByText("alice")
  denied = true
  await userEvent.click(screen.getByRole("button", { name: "刷新目录" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("目录查询未完成")
  expect(screen.queryByText("alice")).not.toBeInTheDocument()
})
it("locks uncertain writes and exposes the original operation lookup", async () => {
  const original = vi.mocked(api).getMockImplementation()!
  vi.mocked(api).mockImplementation(async <T,>(p: string, o?: SendOptions) => {
    if (p === "/directory/changes") throw Error("lost response")
    return original(p, o) as Promise<T>
  })
  show()
  await screen.findByText("alice")
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "停用" }))
  await user.type(screen.getByLabelText("操作原因"), "停用测试账户")
  await user.type(screen.getByLabelText("输入 alice 确认"), "alice")
  await user.click(screen.getByRole("button", { name: "确认执行" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("请求未确认")
  expect(screen.getByRole("button", { name: "确认执行" })).toBeDisabled()
  expect(screen.getByRole("button", { name: "查询此操作结果" })).toBeVisible()
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p === "/directory/changes")
  ).toHaveLength(1)
})
it("requires a cluster and typed confirmation when mapping a cluster role", async () => {
  show()
  await screen.findByText("alice")
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "组角色映射" }))
  await screen.findByText("尚未配置组角色映射。")
  await user.type(screen.getByLabelText("目录组名称"), "research")
  await user.selectOptions(screen.getByLabelText("平台角色"), "viewer")
  const submit = screen.getByRole("button", { name: "添加组角色映射" })
  expect(submit).toBeDisabled()
  await user.selectOptions(screen.getByLabelText("授权集群"), "aaaaaaaaaaaaaaa")
  await user.type(
    screen.getByLabelText("输入 CHANGE ACCESS 确认授权变更"),
    "CHANGE ACCESS"
  )
  await user.click(submit)
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.some(
          ([p, o]) => p === "/directory/role-mappings" && o?.method === "POST"
        )
    ).toBe(true)
  )
  const call = vi
    .mocked(api)
    .mock.calls.find(
      ([p, o]) => p === "/directory/role-mappings" && o?.method === "POST"
    )
  expect(call?.[1]?.body).toEqual({
    group: "research",
    roleCode: "viewer",
    clusterId: "aaaaaaaaaaaaaaa",
    confirmation: "CHANGE ACCESS",
  })
})
