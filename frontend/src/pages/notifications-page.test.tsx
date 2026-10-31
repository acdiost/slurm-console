import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError, type SendOptions } from "pocketbase"
import { NotificationsPage } from "./notifications-page"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { parseHints } from "@/features/notifications/feed"
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
const rule = {
  id: "rule1",
  clusterId: "alpha",
  name: "节点巡检",
  kind: "node_unavailable",
  severity: "critical",
  enabled: true,
  threshold: 1,
  holdMinutes: 2,
  revision: 7,
  state: "firing",
  incidentActive: true,
  pendingSince: "",
  lastChecked: "2026-10-31T01:00:00Z",
  summary: "1 个节点处于 DOWN。",
  affectedCount: 1,
  samples: ["node1"],
  usable: true,
  stale: false,
}
const notice = {
  id: "notice1",
  clusterId: "alpha",
  ruleId: "rule1",
  ruleName: "节点巡检",
  kind: "node_unavailable",
  severity: "critical",
  phase: "firing",
  incidentId: "incident1",
  message: "1 个节点处于 DOWN。",
  affectedCount: 1,
  samples: ["node1"],
  read: false,
  createdAt: "2026-10-31T01:00:00Z",
  href: "/nodes?cluster=alpha&filterMode=custom",
}
function role(allowed = true) {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "alice",
      email: "alice@example.test",
      name: "Alice",
      username: "alice",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: [],
        clusters: [
          {
            cluster,
            roles: [],
            permissions: allowed
              ? ["clusters.read", "nodes.read", "jobs.read", "metrics.read"]
              : ["clusters.read"],
          },
        ],
      },
    },
  })
}
function mock(items: unknown[] = [notice], rules: unknown[] = [rule]) {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.startsWith("/clusters?"))
      return { items: [cluster], page: 1, perPage: 20, totalItems: 1 } as T
    if (path.startsWith("/notification-rules"))
      return {
        items: rules,
        maxPerUser: 20,
        maxRules: 100,
        intervalSeconds: 30,
      } as T
    if (path.startsWith("/notifications"))
      return {
        items,
        page: 1,
        perPage: 20,
        totalItems: items.length,
        unreadCount: items.length,
      } as T
    return {} as T
  })
}
function show() {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/notifications?cluster=alpha"]}>
        <NotificationsPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  role()
  mock()
})
afterEach(() => client.clear())
it("shows current incidents and authorized resource links without executing checks on visit", async () => {
  show()
  expect(
    await screen.findByRole("article", { name: "节点巡检 触发" })
  ).toBeInTheDocument()
  expect(screen.getByRole("link", { name: "查看当前资源" })).toHaveAttribute(
    "href",
    notice.href
  )
  expect(screen.getByText("告警中")).toBeInTheDocument()
  expect(vi.mocked(api).mock.calls.every(([, o]) => !o?.method)).toBe(true)
})
it("writes explicit read state and read-all without resource controls", async () => {
  show()
  await screen.findByRole("article", { name: "节点巡检 触发" })
  await userEvent.click(screen.getByRole("button", { name: "标为已读" }))
  await waitFor(() =>
    expect(vi.mocked(api)).toHaveBeenCalledWith("/notifications/notice1/read", {
      method: "POST",
      body: { read: true },
    })
  )
  await userEvent.click(screen.getByRole("button", { name: "全部标为已读" }))
  await waitFor(() =>
    expect(vi.mocked(api)).toHaveBeenCalledWith("/notifications/read-all", {
      method: "POST",
      body: {},
    })
  )
})
it("creates a personal rule with explicit threshold and continuous hold", async () => {
  mock([], [])
  show()
  await screen.findByText("尚未创建告警规则。")
  await userEvent.click(screen.getByRole("button", { name: "新建规则" }))
  await userEvent.type(
    screen.getByLabelText("规则名称", { exact: true }),
    "我的离线巡检"
  )
  await userEvent.clear(screen.getByLabelText("持续分钟数", { exact: true }))
  await userEvent.type(
    screen.getByLabelText("持续分钟数", { exact: true }),
    "0"
  )
  await userEvent.click(screen.getByRole("button", { name: "保存规则" }))
  await waitFor(() =>
    expect(vi.mocked(api)).toHaveBeenCalledWith("/notification-rules", {
      method: "POST",
      body: {
        clusterId: "alpha",
        name: "我的离线巡检",
        kind: "node_unavailable",
        severity: "warning",
        enabled: true,
        threshold: 1,
        holdMinutes: 0,
      },
    })
  )
})
it("retains form changes after a CAS conflict without retry or implicit overwrite", async () => {
  const fallback = vi.mocked(api).getMockImplementation()!
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, o?: SendOptions) => {
      if (o?.method === "PATCH") throw new ClientResponseError({ status: 409 })
      return fallback(path, o) as Promise<T>
    }
  )
  show()
  await screen.findByRole("article", { name: "规则 节点巡检" })
  await userEvent.click(screen.getByRole("button", { name: "编辑规则" }))
  await userEvent.clear(screen.getByLabelText("规则名称", { exact: true }))
  await userEvent.type(
    screen.getByLabelText("规则名称", { exact: true }),
    "新的巡检名"
  )
  await userEvent.click(screen.getByRole("button", { name: "保存规则" }))
  await screen.findByText(
    "规则版本已变化、达到数量限制或检查繁忙，请刷新后重试。"
  )
  expect(screen.getByLabelText("规则名称")).toHaveValue("新的巡检名")
  const writes = vi
    .mocked(api)
    .mock.calls.filter(([, o]) => o?.method === "PATCH")
  expect(writes).toHaveLength(1)
  expect(writes[0][1]?.body).toMatchObject({ revision: 7, name: "新的巡检名" })
})
it("treats unavailable data as unknown while retaining the unresolved incident", async () => {
  mock([], [{ ...rule, state: "unknown", stale: true }])
  show()
  await screen.findByText("无法判断")
  expect(screen.getByText("原告警尚未确认恢复。")).toBeInTheDocument()
  expect(screen.getByText(/采样延迟/)).toBeInTheDocument()
})
it("disables reads and edits of revoked rules while allowing owner cleanup", async () => {
  role(false)
  mock(
    [],
    [
      {
        ...rule,
        state: "blocked",
        usable: false,
        incidentActive: false,
        samples: [],
      },
    ]
  )
  show()
  await screen.findByText("权限已撤销")
  expect(screen.getByRole("button", { name: "立即检查" })).toBeDisabled()
  expect(screen.getByRole("button", { name: "编辑规则" })).toBeDisabled()
  expect(screen.getByRole("button", { name: "删除规则" })).toBeEnabled()
  await userEvent.click(screen.getByRole("button", { name: "新建规则" }))
  expect(screen.getByRole("button", { name: "保存规则" })).toBeDisabled()
  expect(screen.getByRole("alert")).toHaveTextContent("没有所选集群")
})
it("does not render stale notification content after source errors", async () => {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.startsWith("/notifications?"))
      throw new ClientResponseError({ status: 503 })
    if (path.startsWith("/notification-rules"))
      return {
        items: [],
        maxPerUser: 20,
        maxRules: 100,
        intervalSeconds: 30,
      } as T
    return { items: [cluster], page: 1, perPage: 20, totalItems: 1 } as T
  })
  show()
  await screen.findByRole("alert")
  expect(screen.queryByText("node1")).not.toBeInTheDocument()
  expect(screen.getByRole("button", { name: "全部标为已读" })).toBeDisabled()
})
it("coalesces only SSE invalidation hints, preserves fragments and bounds input", () => {
  expect(parseHints("event: changed\ndata: {}\n\nevent: rea")).toEqual({
    changed: true,
    rest: "event: rea",
  })
  expect(parseHints(": heartbeat\n\n").changed).toBe(false)
  expect(() => parseHints("x".repeat(8193))).toThrow()
})
