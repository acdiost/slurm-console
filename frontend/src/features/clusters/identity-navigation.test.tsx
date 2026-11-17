/// <reference lib="es2024.promise" />
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { act, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router"
import {
  keepPreviousData,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query"
import { ConsoleLayout } from "@/app/console-layout"
import { TooltipProvider } from "@/components/ui/tooltip"
import { cluster } from "@/features/overview/fixtures.test-support"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { ClusterPicker, useAccessibleClusters } from "./cluster-picker"

vi.mock("@/services/api", () => ({ api: vi.fn() }))

const pam = { ...cluster, authenticationMethod: "pam" as const }
const ldap = {
  ...cluster,
  id: "beta",
  name: "LDAP 集群",
  authenticationMethod: "ldap" as const,
}
const ipa = {
  ...cluster,
  id: "gamma",
  name: "IPA 集群",
  authenticationMethod: "freeipa" as const,
}
const providerName = /^(PAM|LDAP|FreeIPA)$/
let client: QueryClient
let metadata: Map<string, unknown>

function role(directory = true) {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "reader",
      email: "reader@example.test",
      name: "Reader",
      username: "reader",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: directory ? ["platform.directory.read"] : [],
        clusters: [],
      },
    },
  })
}

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        gcTime: 0,
        placeholderData: keepPreviousData,
      },
    },
  })
  metadata = new Map<string, unknown>([
    [pam.id, pam],
    [ldap.id, ldap],
    [ipa.id, ipa],
  ])
  role()
  vi.mocked(api).mockReset()
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path === "/notifications?perPage=1")
      return {
        items: [],
        page: 1,
        perPage: 1,
        totalItems: 0,
        unreadCount: 0,
      } as T
    if (path.startsWith("/clusters?")) {
      const params = new URLSearchParams(path.split("?")[1])
      return {
        items:
          params.has("q") || params.get("page") === "2"
            ? [ipa]
            : [pam, ldap, ipa],
        page: Number(params.get("page") || 1),
        perPage: 20,
        totalItems: params.has("q") ? 1 : 21,
      } as T
    }
    if (path.startsWith("/clusters/")) {
      const result = metadata.get(
        decodeURIComponent(path.slice("/clusters/".length))
      )
      if (result instanceof Error) throw result
      if (result === undefined) throw new Error("Cluster not found")
      return (await result) as T
    }
    throw new Error(`Unexpected API path: ${path}`)
  })
})
afterEach(() => {
  client.clear()
  useAuth.setState({ user: null, status: "anonymous" })
})

function PickerWorkspace() {
  const selection = useAccessibleClusters()
  return <ClusterPicker selection={selection} label="作业" />
}

function show(path: string, picker = false) {
  const router = createMemoryRouter(
    [
      {
        element: <ConsoleLayout />,
        children: [
          { path: "*", element: picker ? <PickerWorkspace /> : <div /> },
        ],
      },
    ],
    { initialEntries: [path] }
  )
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  return router
}

it("当前集群依次绑定 PAM、LDAP、FreeIPA，唯一入口保留上下文和激活状态", async () => {
  const router = show(
    "/nodes?cluster=alpha&clusterPage=2&clusterQ=gpu&node=node1"
  )
  const user = userEvent.setup()
  const pamLink = await screen.findByRole("link", { name: "PAM" })
  expect(screen.getAllByRole("link", { name: providerName })).toHaveLength(1)
  expect(pamLink).toHaveAttribute(
    "href",
    "/linux-users?cluster=alpha&clusterPage=2&clusterQ=gpu"
  )
  await user.click(pamLink)
  expect(await screen.findByRole("link", { name: "PAM" })).toHaveAttribute(
    "aria-current",
    "page"
  )
  await act(() =>
    router.navigate("/nodes?cluster=beta&clusterPage=2&clusterQ=gpu")
  )
  const ldapLink = await screen.findByRole("link", { name: "LDAP" })
  expect(screen.getAllByRole("link", { name: providerName })).toHaveLength(1)
  expect(ldapLink).toHaveAttribute(
    "href",
    "/linux-users?cluster=beta&clusterPage=2&clusterQ=gpu"
  )
  await act(() =>
    router.navigate("/nodes?cluster=gamma&clusterPage=2&clusterQ=gpu")
  )
  const ipaLink = await screen.findByRole("link", { name: "FreeIPA" })
  expect(screen.getAllByRole("link", { name: providerName })).toHaveLength(1)
  expect(ipaLink).toHaveAttribute(
    "href",
    "/directory?cluster=gamma&clusterPage=2&clusterQ=gpu"
  )
  await user.click(ipaLink)
  expect(await screen.findByRole("link", { name: "FreeIPA" })).toHaveAttribute(
    "aria-current",
    "page"
  )
  expect(screen.getByRole("link", { name: "节点管理" })).toHaveAttribute(
    "href",
    "/nodes?cluster=gamma&clusterPage=2&clusterQ=gpu"
  )
})

it("切换等待和乱序响应不能展示旧集群的身份入口", async () => {
  const beta = Promise.withResolvers<unknown>()
  const gamma = Promise.withResolvers<unknown>()
  metadata.set(ldap.id, beta.promise)
  metadata.set(ipa.id, gamma.promise)
  const router = show("/nodes?cluster=alpha")
  await screen.findByRole("link", { name: "PAM" })
  await act(() => router.navigate("/nodes?cluster=beta"))
  expect(
    screen.queryByRole("link", { name: providerName })
  ).not.toBeInTheDocument()
  await act(() => router.navigate("/nodes?cluster=gamma"))
  expect(
    screen.queryByRole("link", { name: providerName })
  ).not.toBeInTheDocument()
  await act(async () => beta.resolve(ldap))
  expect(
    screen.queryByRole("link", { name: providerName })
  ).not.toBeInTheDocument()
  await act(async () => gamma.resolve(ipa))
  expect(await screen.findByRole("link", { name: "FreeIPA" })).toHaveAttribute(
    "href",
    "/directory?cluster=gamma"
  )
  expect(screen.getAllByRole("link", { name: providerName })).toHaveLength(1)
  await act(() => router.navigate("/audits?cluster=alpha"))
  expect(
    screen.queryByRole("link", { name: providerName })
  ).not.toBeInTheDocument()
})

it.each([
  "/nodes",
  "/audits?cluster=alpha",
  "/settings?cluster=alpha",
  "/notifications?cluster=alpha",
  "/fleet?cluster=alpha",
])("没有工作空间选择时不读取身份绑定或改写 URL：%s", async (path) => {
  const router = show(path)
  await screen.findByText("21 个已登记集群")
  expect(
    screen.queryByRole("link", { name: providerName })
  ).not.toBeInTheDocument()
  expect(api).not.toHaveBeenCalledWith("/clusters/alpha", expect.anything())
  expect(router.state.location.pathname + router.state.location.search).toBe(
    path
  )
})

it.each([
  ["未绑定", { ...pam, authenticationMethod: "" }, "success"],
  ["未知绑定", { ...pam, authenticationMethod: "unknown" }, "error"],
  ["缺失绑定", { ...pam, authenticationMethod: undefined }, "error"],
  ["无法访问", new Error("Forbidden"), "error"],
] as const)("%s 时不默认展示 PAM", async (_name, response, status) => {
  metadata.set(pam.id, response)
  show("/nodes?cluster=alpha")
  await waitFor(() =>
    expect(client.getQueryState(["cluster-metadata", pam.id])?.status).toBe(
      status
    )
  )
  expect(
    screen.queryByRole("link", { name: providerName })
  ).not.toBeInTheDocument()
})

it("已有绑定在刷新等待及失败时隐藏，不能保留失效入口", async () => {
  show("/nodes?cluster=alpha")
  await screen.findByRole("link", { name: "PAM" })
  const refresh = Promise.withResolvers<unknown>()
  metadata.set(pam.id, refresh.promise)
  act(() => {
    void client.invalidateQueries({ queryKey: ["cluster-metadata", pam.id] })
  })
  await waitFor(() =>
    expect(
      screen.queryByRole("link", { name: providerName })
    ).not.toBeInTheDocument()
  )
  await act(async () =>
    refresh.resolve({ ...pam, authenticationMethod: "unknown" })
  )
  await waitFor(() =>
    expect(client.getQueryState(["cluster-metadata", pam.id])?.status).toBe(
      "error"
    )
  )
  expect(
    screen.queryByRole("link", { name: providerName })
  ).not.toBeInTheDocument()
})

it("FreeIPA 保留目录读取权限要求，LDAP 使用原有 Linux 用户入口", async () => {
  role(false)
  const router = show("/nodes?cluster=gamma")
  await waitFor(() =>
    expect(client.getQueryState(["cluster-metadata", ipa.id])?.status).toBe(
      "success"
    )
  )
  expect(
    screen.queryByRole("link", { name: providerName })
  ).not.toBeInTheDocument()
  await act(() => router.navigate("/nodes?cluster=beta"))
  expect(await screen.findByRole("link", { name: "LDAP" })).toHaveAttribute(
    "href",
    "/linux-users?cluster=beta"
  )
})

it.each([
  ["alpha", "PAM", "/linux-users"],
  ["beta", "LDAP", "/linux-users"],
  ["gamma", "FreeIPA", "/directory"],
])("移动导航使用相同绑定并在访问后关闭：%s", async (id, label, destination) => {
  const router = show(`/nodes?cluster=${id}&clusterPage=2&clusterQ=gpu`)
  const user = userEvent.setup()
  await screen.findByRole("link", { name: label })
  await user.click(screen.getByRole("button", { name: "打开导航" }))
  const dialog = await screen.findByRole("dialog", { name: "主导航" })
  const mobileLink = await within(dialog).findByRole("link", { name: label })
  expect(
    within(dialog).getAllByRole("link", { name: providerName })
  ).toHaveLength(1)
  expect(mobileLink).toHaveAttribute(
    "href",
    `${destination}?cluster=${id}&clusterPage=2&clusterQ=gpu`
  )
  await user.click(mobileLink)
  await waitFor(() =>
    expect(
      screen.queryByRole("dialog", { name: "主导航" })
    ).not.toBeInTheDocument()
  )
  expect(router.state.location.pathname).toBe(destination)
  expect(await screen.findByRole("link", { name: label })).toHaveAttribute(
    "aria-current",
    "page"
  )
})

it("浏览不同提供方的候选集群不改变当前身份入口，确认选择后才切换", async () => {
  const router = show("/nodes?cluster=alpha&job=42", true)
  const user = userEvent.setup()
  await screen.findByRole("link", { name: "PAM" })
  await user.click(
    await screen.findByRole("button", { name: `选择作业集群：${pam.name}` })
  )
  await user.click(
    await screen.findByRole("button", { name: "下一组作业集群" })
  )
  await screen.findByRole("option", { name: ipa.name })
  expect(new URLSearchParams(router.state.location.search).get("cluster")).toBe(
    "alpha"
  )
  expect(screen.getByRole("link", { name: "PAM" })).toHaveAttribute(
    "href",
    "/linux-users?cluster=alpha&clusterPage=2"
  )
  await user.type(screen.getByLabelText("搜索作业可选集群"), "gamma")
  await user.click(screen.getByRole("button", { name: "搜索集群" }))
  await waitFor(() =>
    expect(screen.getByRole("link", { name: "PAM" })).toHaveAttribute(
      "href",
      "/linux-users?cluster=alpha&clusterPage=1&clusterQ=gamma"
    )
  )
  const candidate = await screen.findByRole("option", { name: ipa.name })
  candidate.focus()
  expect(new URLSearchParams(router.state.location.search).get("cluster")).toBe(
    "alpha"
  )
  expect(
    screen.queryByRole("link", { name: "FreeIPA" })
  ).not.toBeInTheDocument()
  await user.keyboard("{Enter}")
  expect(await screen.findByRole("link", { name: "FreeIPA" })).toHaveAttribute(
    "href",
    "/directory?cluster=gamma&clusterPage=1&clusterQ=gamma"
  )
  expect(new URLSearchParams(router.state.location.search).has("job")).toBe(
    false
  )
})
