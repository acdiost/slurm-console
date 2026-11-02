import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClusterConfigurationsPage } from "./cluster-configurations-page"
import { api } from "@/services/api"
import type { SendOptions } from "pocketbase"
import { useAuth } from "@/stores/auth"
import { labels } from "@/features/config-history/schema"
import type {
  ClusterConfiguration,
  ConnectionValues,
} from "@/features/clusters/schema"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
)
let client: QueryClient, current: ClusterConfiguration
const original: ClusterConfiguration = {
  id: "historycluster1",
  name: "原始集群",
  slug: "research",
  description: "原始说明",
  connectionRef: "approved",
  apiVersion: "v0.0.43",
  enabled: true,
  timeoutSeconds: 5,
  revision: 4,
  endpoint: "unix:///run/test/rest.sock",
  authType: "local",
  profileAvailable: true,
  lastProbe: {
    status: "not_tested",
    message: "未检测",
    checkedAt: "",
    durationMs: 0,
    serverVersion: "",
    controllers: [],
  },
}
function snap(v: ClusterConfiguration | ConnectionValues) {
  return Object.fromEntries(
    (Object.keys(labels) as (keyof typeof labels)[]).map((k) => [k, v[k]])
  )
}
function preview(v: ConnectionValues, disable = false) {
  const before = snap(current),
    after = snap(v)
  return {
    clusterId: current.id,
    currentRevision: current.revision,
    nextRevision: current.revision + 1,
    before,
    after,
    fields: Object.keys(labels).map((k) => ({
      field: k,
      before: String(before[k]),
      after: String(after[k]),
      changed: before[k] !== after[k],
    })),
    configuration: disable ? null : v,
  }
}
function show() {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ClusterConfigurationsPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  current = structuredClone(original)
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
        platformPermissions: [
          "platform.clusters.manage",
          "platform.audit.read",
        ],
        clusters: [],
      },
    },
  })
  vi.mocked(api).mockReset()
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: SendOptions) => {
      if (path.includes("cluster-connection-profiles"))
        return [
          {
            ref: "approved",
            label: "可信连接",
            endpoint: original.endpoint,
            authType: "local",
            apiVersions: ["v0.0.43"],
          },
        ] as T
      if (path.endsWith("/disable-preview"))
        return preview({ ...current, enabled: false }, true) as T
      if (path.endsWith("/preview"))
        return preview(options?.body as ConnectionValues) as T
      if (path.endsWith("/disable")) {
        current = { ...current, enabled: false, revision: current.revision + 1 }
        return current as T
      }
      if (options?.method === "PATCH") {
        current = {
          ...current,
          ...(options.body as ConnectionValues),
          revision: current.revision + 1,
        }
        return current as T
      }
      return { items: [current], page: 1, perPage: 20, totalItems: 1 } as T
    }
  )
})
afterEach(() => {
  client.clear()
  vi.restoreAllMocks()
})
it("previews the exact normalized changes and only saves after explicit confirmation", async () => {
  show()
  await screen.findByText("原始集群")
  await userEvent.click(screen.getByRole("button", { name: "编辑 原始集群" }))
  await userEvent.clear(screen.getByLabelText("说明"))
  await userEvent.type(screen.getByLabelText("说明"), "更新说明")
  await userEvent.click(screen.getByRole("button", { name: "预览变更" }))
  const dialog = await screen.findByRole("alertdialog")
  expect(dialog).toHaveTextContent("更新说明")
  expect(dialog).toHaveTextContent("原始说明")
  expect(vi.mocked(api).mock.calls.some(([, o]) => o?.method === "PATCH")).toBe(
    false
  )
  await userEvent.click(
    within(dialog).getByRole("button", { name: "确认保存" })
  )
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.some(
          ([, o]) =>
            o?.method === "PATCH" &&
            (o.body as ConnectionValues).description === "更新说明" &&
            (o.body as ConnectionValues).revision === 4
        )
    ).toBe(true)
  )
  expect(vi.mocked(api).mock.calls.some(([p]) => p.endsWith("/test"))).toBe(
    false
  )
})
it("cancels a preview without saving or discarding the editable draft", async () => {
  show()
  await screen.findByText("原始集群")
  await userEvent.click(screen.getByRole("button", { name: "编辑 原始集群" }))
  await userEvent.type(screen.getByLabelText("说明"), " 草稿")
  await userEvent.click(screen.getByRole("button", { name: "预览变更" }))
  await screen.findByRole("alertdialog")
  await userEvent.click(screen.getByRole("button", { name: "返回编辑" }))
  expect(screen.getByLabelText("说明")).toHaveValue("原始说明 草稿")
  expect(vi.mocked(api).mock.calls.some(([, o]) => o?.method === "PATCH")).toBe(
    false
  )
})
it("previews disable without sending a disable request until confirmation", async () => {
  show()
  await screen.findByText("原始集群")
  expect(screen.getByRole("link", { name: "版本对比" })).toHaveAttribute(
    "href",
    "/configuration-history?cluster=historycluster1"
  )
  await userEvent.click(screen.getByRole("button", { name: "停用 原始集群" }))
  const dialog = await screen.findByRole("alertdialog")
  await within(dialog).findByText("平台连接状态")
  expect(vi.mocked(api).mock.calls.some(([p]) => p.endsWith("/disable"))).toBe(
    false
  )
  await userEvent.click(
    within(dialog).getByRole("button", { name: "确认停用" })
  )
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.some(
          ([p, o]) =>
            p.endsWith("/disable") &&
            (o?.body as { revision: number }).revision === 4
        )
    ).toBe(true)
  )
})
