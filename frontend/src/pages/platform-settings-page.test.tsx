import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, act, cleanup } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { PlatformSettingsPage } from "./platform-settings-page"
import { LoginPage } from "./login-page"
import { MemoryRouter } from "react-router"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { usePlatformIdentity } from "@/features/platform-settings/identity"
import { defaults } from "@/features/platform-settings/schema"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const settings = { ...defaults, revision: 1, updatedAt: "2026-11-03T00:00:00Z" }
const ai = {
  enabled: false,
  label: "AI 服务",
  endpoint: "",
  model: "",
  clusterIds: [],
  timeoutSeconds: 30,
  maxOutputTokens: 2048,
  tokenParameter: "max_completion_tokens",
  revision: 1,
  source: "server",
  keyConfigured: false,
  caConfigured: false,
}
const runtime = {
  runtime: {
    startedAt: "2026-11-03T00:00:00Z",
    goVersion: "go1.27.3",
    os: "linux",
    arch: "arm64",
    revision: "fixture-commit",
    modified: false,
    runAsRoot: true,
    database: "SQLite",
  },
  integrations: [
    { id: "pam", name: "PAM 系统认证", configured: true, count: 0 },
    { id: "ai", name: "AI 运维辅助", configured: false, count: 0 },
  ],
}
const target = {
  id: "targetuser00001",
  email: "target@example.test",
  name: "普通用户",
  username: "",
  authSource: "local",
  active: true,
  revision: 1,
  updatedAt: "2026-11-03T00:00:00Z",
}
const catalog = {
  roles: [
    {
      code: "viewer",
      name: "Viewer",
      scope: "cluster",
      permissions: [{ code: "jobs.read", name: "读取作业" }],
    },
    {
      code: "super_admin",
      name: "Super Admin",
      scope: "platform",
      permissions: [{ code: "platform.users.manage", name: "管理平台用户" }],
    },
  ],
  clusters: [{ id: "clusterfixture1", name: "隔离集群" }],
}
const perms = [
  "platform.settings.read",
  "platform.settings.manage",
  "platform.users.manage",
  "platform.roles.manage",
]
function role(permissions = perms) {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "actor",
      email: "actor@example.test",
      name: "管理员",
      username: "",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: permissions,
        clusters: [],
      },
    },
  })
}
function show() {
  render(
    <QueryClientProvider client={client}>
      <PlatformSettingsPage />
    </QueryClientProvider>
  )
}
beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  )
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  role()
  usePlatformIdentity.setState(defaults)
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  client.clear()
  useAuth.setState({ user: null, status: "anonymous" })
  usePlatformIdentity.setState(defaults)
})
it("hides unauthorized settings without reading protected endpoints", () => {
  role([])
  show()
  expect(screen.getByRole("alert")).toHaveTextContent("没有平台设置")
  expect(api).not.toHaveBeenCalled()
})
it("shows readonly settings without save authority", async () => {
  role(["platform.settings.read"])
  vi.mocked(api).mockResolvedValue(settings)
  show()
  expect(await screen.findByLabelText("平台名称 *")).toBeDisabled()
  expect(screen.getByRole("button", { name: "保存基础设置" })).toBeDisabled()
  expect(
    screen.queryByRole("tab", { name: "用户与角色" })
  ).not.toBeInTheDocument()
})
it("persists basic info and updates public identity only after success", async () => {
  const u = userEvent.setup()
  vi.mocked(api)
    .mockResolvedValueOnce(settings)
    .mockResolvedValueOnce({ ...settings, name: "科研平台", revision: 2 })
  show()
  await u.clear(await screen.findByLabelText("平台名称 *"))
  await u.type(screen.getByLabelText("平台名称 *"), "科研平台")
  expect(usePlatformIdentity.getState().name).toBe(defaults.name)
  await u.click(screen.getByRole("button", { name: "保存基础设置" }))
  expect(await screen.findByText("修订 2")).toBeInTheDocument()
  expect(usePlatformIdentity.getState().name).toBe("科研平台")
  expect(api).toHaveBeenLastCalledWith("/platform-settings", {
    method: "PATCH",
    body: { ...defaults, name: "科研平台", revision: 1 },
  })
})
it("keeps unsaved edits and authoritative identity after revision conflict", async () => {
  const u = userEvent.setup()
  vi.mocked(api)
    .mockResolvedValueOnce(settings)
    .mockRejectedValueOnce(
      new ClientResponseError({
        status: 409,
        response: { error: { message: "设置已变化，请刷新后核对。" } },
      })
    )
  show()
  await u.clear(await screen.findByLabelText("平台名称 *"))
  await u.type(screen.getByLabelText("平台名称 *"), "未保存名称")
  await u.click(screen.getByRole("button", { name: "保存基础设置" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("设置已变化")
  expect(usePlatformIdentity.getState().name).toBe(defaults.name)
  expect(screen.getByLabelText("平台名称 *")).toHaveValue("未保存名称")
  expect(api).toHaveBeenCalledTimes(2)
})
it("reviews AI configuration and requires exact typed confirmation before a metadata-only save", async () => {
  const u = userEvent.setup()
  vi.mocked(api).mockImplementation(async (path, options) => {
    if (path === "/platform-settings") return settings
    if (path === "/platform-settings/runtime") return runtime
    if (path === "/platform-settings/ai")
      return options?.method === "PATCH"
        ? { ...ai, label: "隔离服务", revision: 2, source: "platform" }
        : ai
    throw Error("unexpected path")
  })
  show()
  await screen.findByLabelText("平台名称 *")
  await u.click(screen.getByRole("tab", { name: "集成配置" }))
  await u.clear(await screen.findByLabelText("服务名称"))
  await u.type(screen.getByLabelText("服务名称"), "隔离服务")
  await u.click(screen.getByRole("button", { name: "核对 AI 配置" }))
  expect(await screen.findByRole("alertdialog")).toHaveTextContent(
    "此步骤不调用模型"
  )
  expect(
    vi.mocked(api).mock.calls.filter(([, o]) => o?.method === "PATCH")
  ).toHaveLength(0)
  expect(screen.getByRole("button", { name: "确认提交" })).toBeDisabled()
  await u.type(screen.getByLabelText("输入 APPLY AI 确认"), "APPLY AI")
  await u.click(screen.getByRole("button", { name: "确认提交" }))
  expect(await screen.findByText(/修订 2/)).toBeInTheDocument()
  const writes = vi
    .mocked(api)
    .mock.calls.filter(([, o]) => o?.method === "PATCH")
  expect(writes).toHaveLength(1)
  expect(writes[0][1]?.body).toMatchObject({
    confirmation: "APPLY AI",
    revision: 1,
    config: { enabled: false, label: "隔离服务" },
  })
  expect(JSON.stringify(writes[0])).not.toContain("apiKeyFile")
  expect(
    vi.mocked(api).mock.calls.some(([path]) => path.includes("/explain"))
  ).toBe(false)
})
it("lists users and confirms status changes without changing authentication identity", async () => {
  const u = userEvent.setup()
  vi.mocked(api).mockImplementation(async (path, options) => {
    if (path === "/platform-settings") return settings
    if (path.startsWith("/platform-users?"))
      return { items: [target], page: 1, perPage: 20, totalItems: 1 }
    if (path === "/platform-role-catalog") return catalog
    if (path === `/platform-users/${target.id}/roles`)
      return { user: target, roles: [] }
    if (path === `/platform-users/${target.id}` && options?.method === "PATCH")
      return { ...target, active: false, revision: 2 }
    throw Error(path)
  })
  show()
  await screen.findByLabelText("平台名称 *")
  await u.click(screen.getByRole("tab", { name: "用户与角色" }))
  await u.click(await screen.findByRole("button", { name: "管理 普通用户" }))
  await u.click(screen.getByRole("checkbox", { name: /允许此用户登录平台/ }))
  await u.click(screen.getByRole("button", { name: "核对用户修改" }))
  expect(await screen.findByRole("alertdialog")).toHaveTextContent(target.email)
  await u.type(screen.getByLabelText("输入 UPDATE USER 确认"), "UPDATE USER")
  await u.click(screen.getByRole("button", { name: "确认提交" }))
  expect(
    await screen.findByRole("checkbox", { name: /允许此用户登录平台/ })
  ).not.toBeChecked()
  const write = vi
    .mocked(api)
    .mock.calls.find(
      ([path, o]) =>
        path === `/platform-users/${target.id}` && o?.method === "PATCH"
    )
  expect(write?.[1]?.body).toEqual({
    name: target.name,
    active: false,
    revision: 1,
    confirmation: "UPDATE USER",
  })
})
it("requires cluster scope and confirmation before granting an existing role", async () => {
  const u = userEvent.setup()
  vi.mocked(api).mockImplementation(async (path, options) => {
    if (path === "/platform-settings") return settings
    if (path.startsWith("/platform-users?"))
      return { items: [target], page: 1, perPage: 20, totalItems: 1 }
    if (path === "/platform-role-catalog") return catalog
    if (path === `/platform-users/${target.id}/roles`)
      return options?.method === "POST"
        ? { ...target, revision: 2 }
        : { user: target, roles: [] }
    throw Error(path)
  })
  show()
  await screen.findByLabelText("平台名称 *")
  await u.click(screen.getByRole("tab", { name: "用户与角色" }))
  await u.click(await screen.findByRole("button", { name: "管理 普通用户" }))
  await u.click(await screen.findByRole("combobox", { name: "授予角色" }))
  await u.click(screen.getByRole("option", { name: "Viewer · 集群" }))
  expect(screen.getByRole("button", { name: "核对授予角色" })).toBeDisabled()
  await u.click(screen.getByRole("combobox", { name: "授权集群" }))
  await u.click(screen.getByRole("option", { name: "隔离集群" }))
  await u.click(screen.getByRole("button", { name: "核对授予角色" }))
  expect(await screen.findByRole("alertdialog")).toHaveTextContent("隔离集群")
  await u.type(
    screen.getByLabelText("输入 CHANGE ACCESS 确认"),
    "CHANGE ACCESS"
  )
  await u.click(screen.getByRole("button", { name: "确认提交" }))
  expect(
    vi
      .mocked(api)
      .mock.calls.find(
        ([p, o]) =>
          p === `/platform-users/${target.id}/roles` && o?.method === "POST"
      )?.[1]?.body
  ).toEqual({
    roleCode: "viewer",
    clusterId: "clusterfixture1",
    revision: 1,
    confirmation: "CHANGE ACCESS",
  })
})
it("does not reveal old private settings when access is revoked", async () => {
  vi.mocked(api).mockResolvedValue(settings)
  show()
  expect(await screen.findByLabelText("平台名称 *")).toBeInTheDocument()
  act(() => role([]))
  expect(screen.queryByLabelText("平台名称 *")).not.toBeInTheDocument()
  expect(screen.getByRole("alert")).toHaveTextContent("没有平台设置")
})
it("renders public notice and contact as plain text before login", () => {
  useAuth.setState({ user: null, status: "anonymous" })
  usePlatformIdentity.setState({
    ...defaults,
    name: "科研平台",
    description: "平台说明",
    loginNotice: "<script>测试公告</script> [链接](https://example.test)",
    supportContact: "管理员联系信息",
  })
  render(
    <MemoryRouter>
      <LoginPage />
    </MemoryRouter>
  )
  expect(screen.getByLabelText("登录公告")).toHaveTextContent(
    "<script>测试公告</script>"
  )
  expect(screen.queryByRole("link", { name: "链接" })).not.toBeInTheDocument()
  expect(screen.getByRole("button", { name: "登录" })).toBeInTheDocument()
  expect(screen.getByText(/管理员联系信息/)).toBeInTheDocument()
})
