import { beforeEach, afterEach, expect, it, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { emptySpec } from "@/features/accounting/schema"
import { PoliciesPage } from "./policies-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
const n = (value: number | null, infinite = false) => ({ value, infinite })
const qos = {
  id: "1",
  name: "normal",
  description: "默认服务等级",
  priority: n(0),
  usageFactor: n(1),
  maxJobsPerUser: n(null, true),
  maxSubmitJobsPerUser: n(null, true),
  maxWallMinutes: n(null, true),
  flags: [],
  preemptModes: ["DISABLED"],
}
const fair = {
  id: "6",
  cluster: "production",
  account: "research",
  user: "alice",
  parent: "research",
  partition: "",
  shares: n(0),
  normalizedShares: n(0.00000001),
  rawUsage: "18446744073709551615",
  normalizedUsage: n(null),
  effectiveUsage: n(0),
  factor: n(0),
  level: n(null, true),
}
const metadata = { fetchedAt: "2026-10-29T12:00:00Z", cached: false }
let client: QueryClient
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  role(false)
})
afterEach(() => client.clear())
function role(global: boolean) {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "actor",
      email: "actor@example.test",
      name: "管理员",
      username: "actor",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: global
          ? ["platform.qos.read", "platform.qos.manage"]
          : [],
        clusters: [
          {
            cluster,
            roles: [],
            permissions: [
              "clusters.read",
              "qos.read",
              "qos.update",
              "fairshare.read",
              "associations.read",
            ],
          },
        ],
      },
    },
  })
}
function RouteState() {
  return <output aria-label="当前策略查询">{useLocation().search}</output>
}
function show(url = "/policies?cluster=alpha") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[url]}>
        <PoliciesPage />
        <RouteState />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock() {
  let spec: unknown
  const result = () => ({
    operationId: "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
    action: "qos_create",
    spec,
    slurmCluster: "production",
    status: "confirmed",
    message: "已核实目标状态",
    reason: "测试服务等级",
    createdAt: metadata.fetchedAt,
    updatedAt: metadata.fetchedAt,
  })
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: Parameters<typeof api>[1]) => {
      if (path.startsWith("/clusters?page=") || path === "/clusters?perPage=1")
        return { items: [cluster], page: 1, perPage: 20, totalItems: 1 } as T
      if (path.endsWith("/qos-operations")) return { items: [] } as T
      if (path.endsWith("/accounting-controls/prepare")) {
        spec = (options?.body as { spec: unknown }).spec
        return {
          ...result(),
          allowed: true,
          global: true,
          confirmation: (spec as { qos: { name: string } }).qos.name,
          proof: "signed-proof",
          expiresAt: "2099-10-29T12:02:00Z",
        } as T
      }
      if (
        path.endsWith("/accounting-controls/execute") ||
        path.includes("/accounting-operations/")
      )
        return result() as T
      if (path.endsWith("/qos/normal")) return { item: qos, ...metadata } as T
      return {
        items: path.includes("/fairshare?") ? [fair] : [qos],
        page: 1,
        perPage: 20,
        totalItems: 25,
        ...metadata,
        slurmCluster: "production",
        catalogScope: "referenced",
      } as T
    }
  )
}

it("普通集群角色只读 QOS，查询排序分页保留集群", async () => {
  mock()
  show()
  const u = userEvent.setup()
  expect(await screen.findByRole("button", { name: "normal" })).toBeVisible()
  expect(
    screen.queryByRole("button", { name: "创建 QOS" })
  ).not.toBeInTheDocument()
  expect(screen.getByText(/仅展示所选集群 Association 引用/)).toBeVisible()
  await u.type(screen.getByRole("textbox", { name: "搜索策略记录" }), "normal")
  await u.click(screen.getByRole("button", { name: "筛选" }))
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.some(([p]) => p.includes("/qos?") && p.includes("q=normal"))
    ).toBe(true)
  )
  await u.click(await screen.findByRole("button", { name: "优先级" }))
  await u.click(await screen.findByRole("button", { name: "下一页策略记录" }))
  expect(screen.getByLabelText("当前策略查询").textContent).toContain(
    "cluster=alpha"
  )
  expect(screen.getByLabelText("当前策略查询").textContent).toContain("page=2")
  await u.click(await screen.findByRole("button", { name: "normal" }))
  expect(await screen.findByRole("heading", { name: "normal" })).toBeVisible()
  expect(
    screen.queryByRole("button", { name: "编辑 QOS" })
  ).not.toBeInTheDocument()
})
it("Fairshare 区分零、未知及无限值，保留超大用量和微小份额", async () => {
  mock()
  show()
  const u = userEvent.setup()
  await u.click(screen.getByRole("tab", { name: "Fairshare" }))
  const table = await screen.findByRole("table", { name: "Fairshare 份额" })
  expect(within(table).getByText("18,446,744,073,709,551,615")).toBeVisible()
  expect(within(table).getByText("1.0000e-8")).toBeVisible()
  expect(within(table).getAllByText("0").length).toBe(2)
  await u.click(within(table).getByRole("button", { name: "research" }))
  expect(
    await screen.findByRole("heading", { name: "Fairshare 详情" })
  ).toBeVisible()
  expect(screen.getByText("未提供")).toBeVisible()
  expect(screen.getByText("无限")).toBeVisible()
  expect(
    screen.getByRole("link", { name: "查看 Association" })
  ).toHaveAttribute(
    "href",
    "/accounts?cluster=alpha&tab=associations&item=6&account=research"
  )
})
it("QOS 创建在校验后要求原因与精确名称，零限制与无限制分别提交", async () => {
  role(true)
  mock()
  show()
  const u = userEvent.setup()
  await u.click(await screen.findByRole("button", { name: "创建 QOS" }))
  const dialog = screen.getByRole("alertdialog")
  await u.type(
    within(dialog).getByRole("textbox", { name: "QOS 名称" }),
    "lab-new"
  )
  await u.type(
    within(dialog).getByRole("textbox", { name: "描述" }),
    "新服务等级"
  )
  await u.type(
    within(dialog).getByRole("textbox", { name: "每用户运行作业上限" }),
    "0"
  )
  await u.click(
    within(dialog).getByRole("button", { name: "校验最新 QOS 状态" })
  )
  expect(
    await within(dialog).findByRole("button", { name: "确认创建 QOS" })
  ).toBeEnabled()
  const prepare = vi
    .mocked(api)
    .mock.calls.find(([p]) => p.endsWith("/accounting-controls/prepare"))
  expect(prepare?.[1]?.body).toEqual({
    action: "qos_create",
    spec: {
      ...emptySpec,
      qos: {
        id: "",
        name: "lab-new",
        description: "新服务等级",
        priority: 0,
        usageFactor: 1,
        maxJobsPerUser: 0,
        maxSubmitJobsPerUser: null,
        maxWallMinutes: null,
      },
    },
  })
  await u.type(
    within(dialog).getByRole("textbox", { name: "操作原因" }),
    "测试服务等级"
  )
  await u.type(
    within(dialog).getByRole("textbox", { name: "输入 lab-new 确认" }),
    "wrong"
  )
  await u.click(within(dialog).getByRole("button", { name: "确认创建 QOS" }))
  expect(
    vi
      .mocked(api)
      .mock.calls.some(([p]) => p.endsWith("/accounting-controls/execute"))
  ).toBe(false)
  await u.clear(
    within(dialog).getByRole("textbox", { name: "输入 lab-new 确认" })
  )
  await u.type(
    within(dialog).getByRole("textbox", { name: "输入 lab-new 确认" }),
    "lab-new"
  )
  await u.click(within(dialog).getByRole("button", { name: "确认创建 QOS" }))
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.find(([p]) =>
          p.endsWith("/accounting-controls/execute")
        )?.[1]?.body
    ).toEqual({
      proof: "signed-proof",
      confirmation: "lab-new",
      reason: "测试服务等级",
    })
  )
  expect(await screen.findByText("已核实目标状态")).toBeVisible()
})
it("编辑保留名称，修改表单会使旧确认失效", async () => {
  role(true)
  mock()
  show()
  const u = userEvent.setup()
  await u.click(await screen.findByRole("button", { name: "normal" }))
  await u.click(await screen.findByRole("button", { name: "编辑 QOS" }))
  const dialog = screen.getByRole("alertdialog")
  expect(
    within(dialog).getByRole("textbox", { name: "QOS 名称" })
  ).toHaveAttribute("readonly")
  await u.click(
    within(dialog).getByRole("button", { name: "校验最新 QOS 状态" })
  )
  expect(
    await within(dialog).findByRole("button", { name: "确认编辑 QOS" })
  ).toBeVisible()
  await u.type(within(dialog).getByRole("textbox", { name: "描述" }), " 已调整")
  expect(
    within(dialog).queryByRole("button", { name: "确认编辑 QOS" })
  ).not.toBeInTheDocument()
})
it("刷新失败后清除旧策略内容", async () => {
  mock()
  show()
  const u = userEvent.setup()
  expect(await screen.findByRole("button", { name: "normal" })).toBeVisible()
  vi.mocked(api).mockRejectedValue(
    new ClientResponseError({
      status: 503,
      response: { error: { message: "记账接口不可用" } },
    })
  )
  await u.click(screen.getByRole("button", { name: "刷新策略数据" }))
  expect(await screen.findByText("记账接口不可用")).toBeVisible()
  expect(
    screen.queryByRole("button", { name: "normal" })
  ).not.toBeInTheDocument()
})
