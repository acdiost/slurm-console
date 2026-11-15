import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { act, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { LinuxUserSourceConfiguration } from "./source-configuration"
import type { LinuxUserSource } from "./schema"

vi.mock("@/services/api", () => ({ api: vi.fn() }))
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
)
let client: QueryClient
const changed = vi.fn()
const source: LinuxUserSource = {
  configured: true,
  revision: 4,
  label: "Alpha NSS",
  transport: "ssh",
  host: "identity.example.test",
  port: 22,
  username: "reader",
  knownHosts: "identity.example.test ssh-ed25519 trusted-public-key",
  privateKeyConfigured: true,
  manageLocal: false,
}
function role(manage = true, read = true, id = "alpha") {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "admin",
      email: "admin@example.test",
      name: "Admin",
      username: "admin",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: manage ? ["platform.settings.manage"] : [],
        clusters: [
          {
            cluster: { ...cluster, id },
            roles: [],
            permissions: read ? ["linux_users.read"] : [],
          },
        ],
      },
    },
  })
}
function show(clusterId = "alpha") {
  return render(
    <QueryClientProvider client={client}>
      <LinuxUserSourceConfiguration
        clusterId={clusterId}
        clusterName={clusterId}
        onChanged={changed}
      />
    </QueryClientProvider>
  )
}
function mockSource(initial = source) {
  let current = { ...initial }
  vi.mocked(api).mockImplementation(
    async <T,>(_path: string, options?: Parameters<typeof api>[1]) => {
      if (options?.method === "PUT") {
        const body = options.body as Record<string, unknown>
        current = {
          ...current,
          ...body,
          privateKeyConfigured: body.transport === "ssh",
          configured: true,
          revision: current.revision + 1,
        } as LinuxUserSource
      }
      if (options?.method === "DELETE")
        current = {
          ...source,
          configured: false,
          revision: current.revision + 1,
          label: "",
          transport: "local",
          host: "",
          port: 0,
          username: "",
          knownHosts: "",
          privateKeyConfigured: false,
        }
      return current as T
    }
  )
}
async function open() {
  const button = await screen.findByRole("button", { name: "配置身份来源" })
  await waitFor(() => expect(button).toBeEnabled())
  await userEvent.setup().click(button)
  await screen.findByLabelText("来源名称")
}

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  changed.mockReset()
  role()
  mockSource()
})
afterEach(() => {
  client.clear()
  useAuth.setState({ user: null, status: "anonymous" })
})

it.each([
  [false, true, "alpha"],
  [true, false, "alpha"],
  [true, true, "beta"],
] as const)(
  "平台管理=%s、读取=%s、授权集群=%s 时无管理配置请求",
  (manage, read, authorizedCluster) => {
    role(manage, read, authorizedCluster)
    show()
    expect(
      screen.queryByRole("button", { name: "配置身份来源" })
    ).not.toBeInTheDocument()
    expect(api).not.toHaveBeenCalled()
  }
)

it("未配置来源显示可操作入口，初始修订版 0 保存本地来源", async () => {
  mockSource({
    ...source,
    configured: false,
    revision: 0,
    label: "",
    transport: "local",
    host: "",
    port: 0,
    username: "",
    knownHosts: "",
    privateKeyConfigured: false,
  })
  show()
  expect(await screen.findByText(/此集群尚未配置身份来源/)).toBeVisible()
  await open()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText("来源名称"), "Local NSS")
  await user.click(screen.getByRole("button", { name: "验证并保存" }))
  await waitFor(() => expect(changed).toHaveBeenCalledOnce())
  expect(api).toHaveBeenCalledWith(
    "/clusters/alpha/linux-user-source",
    expect.objectContaining({
      method: "PUT",
      body: {
        revision: 0,
        label: "Local NSS",
        transport: "local",
        host: "",
        port: 0,
        username: "",
        privateKey: "",
        knownHosts: "",
        manageLocal: false,
      },
    })
  )
})

it("留空私钥保留已保存的密钥，并刷新此集群的列表、详情和控制能力", async () => {
  const reset = vi.spyOn(client, "resetQueries")
  const cancel = vi.spyOn(client, "cancelQueries")
  show()
  await open()
  expect(screen.getByLabelText("SSH 私钥内容")).toHaveValue("")
  expect(screen.getByText(/留空保留已保存的私钥/)).toBeVisible()
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "验证并保存" }))
  await waitFor(() => expect(changed).toHaveBeenCalledOnce())
  expect(api).toHaveBeenCalledWith(
    "/clusters/alpha/linux-user-source",
    expect.objectContaining({
      method: "PUT",
      body: expect.objectContaining({
        revision: 4,
        privateKey: "",
        knownHosts: source.knownHosts,
        manageLocal: false,
      }),
    })
  )
  await waitFor(() => {
    for (const key of [
      "linux-users",
      "linux-user-detail",
      "linux-user-capabilities",
      "linux-user-operation",
    ]) {
      expect(cancel).toHaveBeenCalledWith({ queryKey: [key, "alpha"] })
      expect(reset).toHaveBeenCalledWith({ queryKey: [key, "alpha"] })
    }
  })
  expect(await screen.findByText("身份来源已更新。")).toBeVisible()
})

it("新 SSH 来源必须提供私钥和可信记录，拒绝 root 与非法端口", async () => {
  mockSource({
    ...source,
    privateKeyConfigured: false,
    username: "root",
    port: 70000,
    knownHosts: "",
  })
  show()
  await open()
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "验证并保存" }))
  expect(
    await screen.findByText("请输入合法的非 root SSH 用户名")
  ).toBeVisible()
  expect(screen.getByText("端口必须为 1–65535 的整数")).toBeVisible()
  expect(screen.getByText(/请填写 SSH 私钥内容/)).toBeVisible()
  expect(screen.getByText(/请填写可信 known_hosts 内容/)).toBeVisible()
  expect(
    vi.mocked(api).mock.calls.filter(([, options]) => options?.method === "PUT")
  ).toHaveLength(0)
})

it("替换私钥只出现在请求体中，验证失败显示安全错误并允许修正重试", async () => {
  let rejected = true
  const secret = "PRIVATE-KEY-SECRET"
  vi.mocked(api).mockImplementation(
    async <T,>(_path: string, options?: Parameters<typeof api>[1]) => {
      if (options?.method === "PUT" && rejected)
        throw new ClientResponseError({
          status: 503,
          response: { error: { message: secret } },
        })
      return source as T
    }
  )
  show()
  await open()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText("SSH 私钥内容"), secret)
  await user.click(screen.getByRole("button", { name: "验证并保存" }))
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "身份来源验证失败，原配置未更改"
  )
  expect(screen.queryByText(secret)).not.toBeInTheDocument()
  expect(changed).not.toHaveBeenCalled()
  expect(
    JSON.stringify(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.queryKey)
    )
  ).not.toContain(secret)
  expect(client.getMutationCache().getAll()).toHaveLength(0)
  rejected = false
  await user.click(screen.getByRole("button", { name: "验证并保存" }))
  await waitFor(() => expect(changed).toHaveBeenCalledOnce())
  expect(api).toHaveBeenCalledWith(
    "/clusters/alpha/linux-user-source",
    expect.objectContaining({
      method: "PUT",
      body: expect.objectContaining({ privateKey: secret }),
    })
  )
  expect(screen.getByLabelText("SSH 私钥内容")).toHaveValue("")
})

it("冲突禁止再次提交，刷新采用最新修订版并清空未提交私钥", async () => {
  let revision = 4
  vi.mocked(api).mockImplementation(
    async <T,>(_path: string, options?: Parameters<typeof api>[1]) => {
      const body = options?.body
      if (
        options?.method === "PUT" &&
        body &&
        typeof body === "object" &&
        "revision" in body &&
        body.revision === 4
      ) {
        revision = 5
        throw new ClientResponseError({ status: 409 })
      }
      return { ...source, revision } as T
    }
  )
  show()
  await open()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText("SSH 私钥内容"), "discard-this-key")
  await user.click(screen.getByRole("button", { name: "验证并保存" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("身份来源已变更")
  expect(screen.getByRole("button", { name: "验证并保存" })).toBeDisabled()
  await user.click(screen.getByRole("button", { name: "刷新来源配置" }))
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "验证并保存" })).toBeEnabled()
  )
  expect(screen.getByLabelText("SSH 私钥内容")).toHaveValue("")
  await user.click(screen.getByRole("button", { name: "验证并保存" }))
  await waitFor(() => expect(changed).toHaveBeenCalledOnce())
  expect(api).toHaveBeenLastCalledWith(
    "/clusters/alpha/linux-user-source",
    expect.anything()
  )
  expect(api).toHaveBeenCalledWith(
    "/clusters/alpha/linux-user-source",
    expect.objectContaining({
      method: "PUT",
      body: expect.objectContaining({ revision: 5 }),
    })
  )
})

it("切换本地清除 SSH 数据，本地管理必须显式选择且提示独立 root 策略", async () => {
  show()
  await open()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText("SSH 私钥内容"), "discard-this-key")
  await user.click(screen.getByRole("combobox", { name: "连接方式" }))
  await user.click(await screen.findByRole("option", { name: "本地 NSS" }))
  expect(screen.queryByLabelText("SSH 私钥内容")).not.toBeInTheDocument()
  const manage = screen.getByRole("checkbox", { name: "允许本地账户管理" })
  expect(manage).not.toBeChecked()
  expect(screen.getByText(/单独启用 root 账户管理策略/)).toBeVisible()
  await user.click(manage)
  await user.click(screen.getByRole("button", { name: "验证并保存" }))
  await waitFor(() => expect(changed).toHaveBeenCalledOnce())
  expect(api).toHaveBeenCalledWith(
    "/clusters/alpha/linux-user-source",
    expect.objectContaining({
      method: "PUT",
      body: expect.objectContaining({
        transport: "local",
        host: "",
        port: 0,
        username: "",
        privateKey: "",
        knownHosts: "",
        manageLocal: true,
      }),
    })
  )
})

it("删除需确认来源名称并携带修订版，之后显示缺少来源的配置入口", async () => {
  show()
  await open()
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "移除身份来源" }))
  const dialog = screen.getByRole("alertdialog")
  const confirm = within(dialog).getByRole("button", { name: "确认提交" })
  expect(confirm).toBeDisabled()
  await user.type(
    within(dialog).getByLabelText("输入 Alpha NSS 确认"),
    source.label
  )
  await user.click(confirm)
  await waitFor(() => expect(changed).toHaveBeenCalledOnce())
  expect(api).toHaveBeenCalledWith(
    "/clusters/alpha/linux-user-source",
    expect.objectContaining({ method: "DELETE", body: { revision: 4 } })
  )
  expect(await screen.findByText(/此集群尚未配置身份来源/)).toBeVisible()
  expect(
    screen.queryByRole("button", { name: "移除身份来源" })
  ).not.toBeInTheDocument()
})

it("集群切换销毁草稿并取消在途提交，旧响应不影响新集群", async () => {
  let finish: (value: unknown) => void = () => {}
  const pending = new Promise((resolve) => {
    finish = resolve
  })
  let submittedSignal: AbortSignal | undefined
  const access = useAuth.getState().user!
  useAuth.setState({
    user: {
      ...access,
      access: {
        ...access.access,
        platformPermissions: ["platform.settings.manage", "linux_users.read"],
      },
    },
  })
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: Parameters<typeof api>[1]) => {
      if (options?.method === "PUT") {
        submittedSignal = options.signal ?? undefined
        return (await pending) as T
      }
      return {
        ...source,
        label: path.includes("beta") ? "Beta NSS" : source.label,
        revision: path.includes("beta") ? 12 : 4,
      } as T
    }
  )
  const view = show()
  await open()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText("SSH 私钥内容"), "alpha-only-key")
  await user.click(screen.getByRole("button", { name: "验证并保存" }))
  await waitFor(() => expect(submittedSignal).toBeDefined())
  view.rerender(
    <QueryClientProvider client={client}>
      <LinuxUserSourceConfiguration
        clusterId="beta"
        clusterName="Beta"
        onChanged={changed}
      />
    </QueryClientProvider>
  )
  await open()
  expect(screen.getByLabelText("来源名称")).toHaveValue("Beta NSS")
  expect(screen.getByLabelText("SSH 私钥内容")).toHaveValue("")
  expect(submittedSignal?.aborted).toBe(true)
  await act(async () => {
    finish(source)
    await pending
  })
  expect(changed).not.toHaveBeenCalled()
  expect(screen.getByLabelText("来源名称")).toHaveValue("Beta NSS")
  await user.click(screen.getByRole("button", { name: "验证并保存" }))
  await waitFor(() =>
    expect(api).toHaveBeenCalledWith(
      "/clusters/beta/linux-user-source",
      expect.objectContaining({
        method: "PUT",
        body: expect.objectContaining({ revision: 12, privateKey: "" }),
      })
    )
  )
  await waitFor(() => expect(changed).toHaveBeenCalledOnce())
})
