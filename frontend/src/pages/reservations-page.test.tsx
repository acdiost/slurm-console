import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { emptySpec } from "@/features/accounting/schema"
import { ReservationsPage } from "./reservations-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const target = {
  name: "maint-demo",
  startTime: 1900000000,
  endTime: 1900003600,
  nodes: "node01",
  nodeNames: ["node01"],
  nodeCount: 1,
  coreCount: 8,
  users: "alice",
  accounts: "research",
  groups: "",
  partition: "",
  features: "",
  licenses: "",
  burstBuffer: "",
  tres: "cpu=8",
  flags: ["MAINT", "SPEC_NODES"],
  editable: true,
  state: "scheduled",
}
const meta = { fetchedAt: "2026-10-29T14:00:00Z", cached: false }
function role(edit = true) {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "actor",
      name: "管理员",
      email: "actor@example.test",
      username: "actor",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: [],
        clusters: [
          {
            cluster,
            roles: [],
            permissions: [
              "clusters.read",
              "reservations.read",
              ...(edit
                ? [
                    "reservations.create",
                    "reservations.update",
                    "reservations.delete",
                    "nodes.read",
                    "jobs.read",
                  ]
                : []),
            ],
          },
        ],
      },
    },
  })
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  role()
})
afterEach(() => client.clear())
function Location() {
  return <output aria-label="当前预留查询">{useLocation().search}</output>
}
function show(url = "/reservations?cluster=alpha") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[url]}>
        <ReservationsPage />
        <Location />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock(unknown = false) {
  let spec: unknown = {
      ...emptySpec,
      reservation: {
        name: target.name,
        startTime: 0,
        endTime: 0,
        nodes: [],
        users: [],
        accounts: [],
        maintenance: false,
      },
    },
    action = "reservation_delete"
  const result = () => ({
    operationId: "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
    action,
    spec,
    slurmCluster: "production",
    status: unknown ? "unknown" : "confirmed",
    message: "已核实目标状态",
    reason: "维护窗口测试",
    createdAt: meta.fetchedAt,
    updatedAt: meta.fetchedAt,
  })
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: Parameters<typeof api>[1]) => {
      if (path.startsWith("/clusters?page=") || path === "/clusters?perPage=1")
        return { items: [cluster], page: 1, perPage: 20, totalItems: 1 } as T
      if (path.endsWith("/reservation-operations")) return { items: [] } as T
      if (path.endsWith("/reservation-controls/prepare")) {
        const b = options?.body as { action: string; spec: unknown }
        spec = { ...emptySpec, reservation: b.spec }
        action = b.action
        return {
          ...result(),
          allowed: true,
          global: false,
          confirmation: target.name,
          proof: "reservation-proof",
          expiresAt: "2099-01-01T00:00:00Z",
        } as T
      }
      if (
        path.endsWith("/reservation-controls/execute") ||
        path.includes("/reservation-operations/")
      )
        return result() as T
      if (path.includes("/reservations/")) return { item: target, ...meta } as T
      return {
        items: [target],
        page: 1,
        perPage: 20,
        totalItems: 25,
        ...meta,
      } as T
    }
  )
}
it("只读角色查看访问范围，隐藏写入口", async () => {
  role(false)
  mock()
  show("/reservations?cluster=alpha&item=maint-demo")
  expect(
    await screen.findByRole("heading", { name: target.name })
  ).toBeInTheDocument()
  expect(screen.getAllByText("alice").length).toBeGreaterThan(0)
  expect(
    screen.queryByRole("button", { name: "新建预留" })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole("button", { name: "删除预留" })
  ).not.toBeInTheDocument()
})
it("删除先校验和精确名称确认，只提交一次", async () => {
  mock()
  show("/reservations?cluster=alpha&item=maint-demo")
  const u = userEvent.setup()
  await u.click(await screen.findByRole("button", { name: "删除预留" }))
  await u.click(screen.getByRole("button", { name: "校验最新预留" }))
  await screen.findByLabelText("操作原因")
  await u.type(screen.getByLabelText("操作原因"), "维护窗口测试")
  await u.type(screen.getByLabelText("输入 maint-demo 确认"), "maint-demo")
  await u.click(screen.getByRole("button", { name: "确认删除预留" }))
  expect(
    await screen.findByRole("heading", { name: "预留操作结果" })
  ).toBeInTheDocument()
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.filter(([p]) => p.endsWith("/reservation-controls/execute"))
    ).toHaveLength(1)
  )
})
it("新建提交明确节点，改变字段废弃旧确认", async () => {
  mock()
  show()
  const u = userEvent.setup()
  await u.click(await screen.findByRole("button", { name: "新建预留" }))
  await u.type(screen.getByLabelText("预留名称"), "maint-demo")
  await u.type(screen.getByLabelText("节点名称"), "node01,node02")
  await u.type(screen.getByLabelText("允许的 Linux 用户"), "alice")
  await u.click(screen.getByRole("button", { name: "校验最新预留" }))
  expect(await screen.findByLabelText("操作原因")).toBeInTheDocument()
  await u.type(screen.getByLabelText("节点名称"), ",node03")
  expect(screen.queryByLabelText("操作原因")).not.toBeInTheDocument()
  const call = vi
    .mocked(api)
    .mock.calls.find(([p]) => p.endsWith("/reservation-controls/prepare"))
  expect(call?.[1]?.body).toMatchObject({
    action: "reservation_create",
    spec: {
      name: "maint-demo",
      nodes: ["node01", "node02"],
      users: ["alice"],
      maintenance: false,
    },
  })
})
it("未知结果只读核实，不重复执行", async () => {
  mock(true)
  show("/reservations?cluster=alpha&operation=ABCDEFGHIJKLMNOPQRSTUVWXYZ")
  const u = userEvent.setup()
  await screen.findByText("结果待核实")
  await u.click(screen.getByRole("button", { name: "只读核实结果" }))
  expect(
    vi
      .mocked(api)
      .mock.calls.some(([p]) => p.endsWith("/reservation-controls/execute"))
  ).toBe(false)
})
it("分页保留当前集群", async () => {
  mock()
  show()
  const u = userEvent.setup()
  await u.click(await screen.findByRole("button", { name: "下一页预留" }))
  expect(screen.getByLabelText("当前预留查询")).toHaveTextContent(
    "cluster=alpha"
  )
  expect(screen.getByLabelText("当前预留查询")).toHaveTextContent("page=2")
})
