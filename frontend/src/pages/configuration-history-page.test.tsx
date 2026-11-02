import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { ConfigurationHistoryPage } from "./configuration-history-page"
import { ConfigurationDiff } from "@/features/config-history/diff-table"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import {
  labels,
  type DiffField,
  type Version,
} from "@/features/config-history/schema"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
)
const cid = "historycluster1",
  v1 = "versionone00001",
  v2 = "versiontwo00002"
const before = {
    name: "旧集群",
    slug: "research",
    description: "历史说明",
    connectionRef: "approved",
    apiVersion: "v0.0.43",
    enabled: true,
    timeoutSeconds: 5,
  },
  after = { ...before, name: "新集群", description: "" }
const fields: DiffField[] = (
  Object.keys(labels) as (keyof typeof labels)[]
).map((k) => ({
  field: k,
  before: String(before[k]),
  after: String(after[k]),
  changed: before[k] !== after[k],
}))
const version: Version = {
  id: v1,
  clusterId: cid,
  revision: 4,
  schemaVersion: 1,
  kind: "baseline",
  action: "baseline",
  actorId: "",
  actorLabel: "",
  actorSource: "",
  operationId: "",
  requestId: "",
  recordedAt: "2026-11-02 00:00:00.000Z",
}
const newer: Version = {
  ...version,
  id: v2,
  revision: 5,
  kind: "change",
  action: "clusters.update",
  actorId: "admin",
  actorLabel: "原始管理员",
  actorSource: "platform",
  operationId: "operation123",
  requestId: "request123",
}
const list = {
  items: [newer, version],
  page: 1,
  perPage: 20,
  totalItems: 2,
  clusterId: cid,
  currentStatus: "matched",
  currentRevision: 5,
  fetchedAt: "2026-11-02T00:00:00Z",
}
const comparison = {
  clusterId: cid,
  from: version,
  to: newer,
  before,
  after,
  fields,
  fetchedAt: "2026-11-02T00:00:00Z",
}
let client: QueryClient
function role(manage = true, audit = true) {
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
          ...(manage ? ["platform.clusters.manage"] : []),
          ...(audit ? ["platform.audit.read"] : []),
        ],
        clusters: [],
      },
    },
  })
}
function show(extra = "") {
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter
        initialEntries={[`/configuration-history?cluster=${cid}${extra}`]}
      >
        <ConfigurationHistoryPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  role()
  vi.mocked(api).mockReset()
  vi.mocked(api).mockImplementation(
    async <T,>(p: string) =>
      structuredClone(p.includes("/compare?") ? comparison : list) as T
  )
})
afterEach(() => {
  client.clear()
  vi.restoreAllMocks()
})
it("reads versions only until two explicit versions are selected, then compares without writes", async () => {
  show()
  await screen.findByRole("region", { name: "配置版本列表" })
  expect(vi.mocked(api).mock.calls.some(([p]) => p.includes("/compare"))).toBe(
    false
  )
  await userEvent.click(screen.getByRole("button", { name: "以 v4 为基准" }))
  await userEvent.click(
    screen.getByRole("button", { name: "以 v5 为比较版本" })
  )
  await userEvent.click(screen.getByRole("button", { name: "比较所选版本" }))
  const result = await screen.findByRole("region", { name: "历史配置对比结果" })
  expect(result).toHaveTextContent("历史说明")
  expect(result).toHaveTextContent("空值")
  expect(result).toHaveTextContent("原始修改人未记录")
  expect(
    within(result).getByRole("link", { name: "查看关联审计" })
  ).toHaveAttribute("href", "/audits?operation=operation123")
  expect(
    vi.mocked(api).mock.calls.every(([, o]) => !o?.method || o.method === "GET")
  ).toBe(true)
})
it("requires both independent platform permissions without querying history", async () => {
  role(true, false)
  const v = show()
  await screen.findByText("需要平台配置管理和审计读取权限。")
  expect(api).not.toHaveBeenCalled()
  v.unmount()
  role(false, true)
  show()
  await screen.findByText("需要平台配置管理和审计读取权限。")
  expect(api).not.toHaveBeenCalled()
})
it("hides cached version metadata and comparison on a denied refresh", async () => {
  show(`&from=${v1}&to=${v2}`)
  await screen.findByRole("region", { name: "历史配置对比结果" })
  vi.mocked(api).mockRejectedValue(
    new ClientResponseError({ status: 403, response: {} })
  )
  await userEvent.click(screen.getByRole("button", { name: "刷新版本" }))
  await screen.findByText("需要平台配置管理和审计读取权限。")
  expect(
    screen.queryByRole("region", { name: "历史配置对比结果" })
  ).not.toBeInTheDocument()
  expect(screen.queryByText("原始管理员")).not.toBeInTheDocument()
  expect(
    screen.queryByRole("region", { name: "配置版本列表" })
  ).not.toBeInTheDocument()
})
it("keeps retained versions visible when the current cluster has disappeared", async () => {
  vi.mocked(api).mockResolvedValue({
    ...list,
    currentStatus: "missing",
    currentRevision: null,
  })
  show()
  await screen.findByText(/当前集群已不存在/)
  expect(screen.getByRole("button", { name: "以 v4 为基准" })).toBeEnabled()
})
it("rejects malformed version IDs without borrowing another record or comparing", async () => {
  show("&from=bad&to=bad")
  await screen.findByText("URL 中的版本 ID 不合法，请重新选择。")
  await waitFor(() =>
    expect(screen.getByRole("region", { name: "配置版本列表" })).toBeVisible()
  )
  expect(vi.mocked(api).mock.calls.some(([p]) => p.includes("/compare"))).toBe(
    false
  )
})
it("distinguishes empty values, absence, and no-op versions", async () => {
  render(
    <ConfigurationDiff fields={fields.map((f) => ({ ...f, changed: false }))} />
  )
  expect(screen.getByRole("status")).toHaveTextContent("配置字段相同")
  await userEvent.click(
    screen.getByRole("checkbox", { name: "仅显示变化字段" })
  )
  expect(screen.getByRole("table")).toBeVisible()
  expect(screen.getAllByText("启用")).toHaveLength(2)
  expect(screen.getByText("空值")).toBeVisible()
})
it("does not manufacture a baseline for an empty version list", async () => {
  vi.mocked(api).mockResolvedValue({
    ...list,
    items: [],
    totalItems: 0,
    currentStatus: "no_history",
  })
  show()
  await screen.findByText("没有可用版本。")
  expect(
    screen.queryByText("上线基线", { exact: true })
  ).not.toBeInTheDocument()
  expect(screen.getByRole("button", { name: "比较所选版本" })).toBeDisabled()
})
