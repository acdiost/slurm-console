import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { emptySpec } from "@/features/accounting/schema"
import { AccountingPage } from "./accounting-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const inventory = { items: [cluster], page: 1, perPage: 20, totalItems: 1 }
const account = {
  name: "research",
  description: "研究项目",
  organization: "Lab",
  associationCount: 3,
  userCount: 1,
}
const assoc = {
  id: "6",
  cluster: "production",
  account: "research",
  user: "alice",
  partition: "gpu",
  parentAccount: "",
  isDefault: false,
  comment: "批处理",
}
const result = {
  operationId: "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
  action: "account_create",
  spec: {
    ...emptySpec,
    account: "new-lab",
    description: "新项目",
    organization: "Lab",
  },
  slurmCluster: "production",
  status: "confirmed",
  message: "已核实目标记账状态。",
  reason: "新建项目账户",
  createdAt: "2026-10-28T15:00:00Z",
  updatedAt: "2026-10-28T15:00:01Z",
}
const prepared = {
  allowed: true,
  message: "请核对最新记账状态",
  action: "account_create",
  spec: result.spec,
  slurmCluster: "production",
  global: true,
  confirmation: "new-lab",
  proof: "opaque-proof",
  operationId: result.operationId,
  expiresAt: "2099-10-28T15:02:00Z",
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.mocked(api).mockReset()
  useAuth.setState({ status: "authenticated", user: null, message: "" })
})
afterEach(() => client.clear())
function RouteState() {
  return <output aria-label="当前查询">{useLocation().search}</output>
}
function show(url = "/accounts?cluster=alpha") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[url]}>
        <AccountingPage />
        <RouteState />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function role(global = false) {
  useAuth.setState({
    user: {
      id: "actor",
      email: "actor@example.test",
      username: "actor",
      authSource: "local",
      name: "管理员",
      access: {
        platformRoles: [],
        platformPermissions: global ? ["platform.accounting.manage"] : [],
        clusters: [
          {
            cluster,
            roles: ["管理"],
            permissions: [
              "clusters.read",
              "accounts.read",
              "accounts.update",
              "slurm_users.read",
              "associations.read",
              "associations.update",
              "jobs.read",
            ],
          },
        ],
      },
    },
  })
}
function mock() {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path === "/clusters?perPage=1" || path.startsWith("/clusters?page="))
      return inventory as T
    if (path.endsWith("/accounting-operations")) return { items: [] } as T
    if (path.includes("/accounting-operations/")) return result as T
    if (path.endsWith("/accounting-controls/prepare")) return prepared as T
    if (path.endsWith("/accounting-controls/execute")) return result as T
    if (path.endsWith("/accounts/research"))
      return {
        item: account,
        fetchedAt: "2026-10-28T15:00:00Z",
        cached: false,
      } as T
    if (path.endsWith("/associations/6"))
      return {
        item: assoc,
        fetchedAt: "2026-10-28T15:00:00Z",
        cached: false,
      } as T
    return {
      items: path.includes("/associations?")
        ? [assoc]
        : path.includes("/slurm-users?")
          ? [
              {
                name: "alice",
                defaultAccount: "",
                defaultAccountScope: "outside_cluster",
                administratorLevel: ["None"],
                associationCount: 1,
              },
            ]
          : [account],
      page: 1,
      perPage: 20,
      totalItems: 25,
      fetchedAt: "2026-10-28T15:00:00Z",
      cached: false,
    } as T
  })
}
it("记账列表筛选、排序、分页和切换类型保留所选集群", async () => {
  mock()
  show()
  const user = userEvent.setup()
  await screen.findByRole("button", { name: "research" })
  await user.type(screen.getByLabelText("搜索Account"), "research")
  await user.click(screen.getByRole("button", { name: "筛选" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("q=research")
  )
  await user.click(screen.getByRole("button", { name: "下一页记账记录" }))
  await waitFor(() =>
    expect(screen.getByLabelText("当前查询")).toHaveTextContent("page=2")
  )
  await user.click(screen.getByRole("tab", { name: "Slurm 用户" }))
  await screen.findByText("其他集群（已隐藏）")
  expect(screen.getByLabelText("当前查询")).toHaveTextContent("cluster=alpha")
  await user.click(screen.getByRole("tab", { name: "Association" }))
  await screen.findByRole("button", { name: "#6" })
  expect(
    screen.queryByRole("button", { name: "创建 Association" })
  ).not.toBeInTheDocument()
})
it("集群管理员不显示全局身份创建，但可按权限创建关联", async () => {
  role()
  mock()
  show()
  const user = userEvent.setup()
  await screen.findByRole("button", { name: "research" })
  expect(
    screen.queryByRole("button", { name: "创建 Account" })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole("tab", { name: "Association" }))
  expect(
    await screen.findByRole("button", { name: "创建 Association" })
  ).toBeEnabled()
})
it("账户详情可跳转当前账户关联，关联详情给出精确身份", async () => {
  role()
  mock()
  show()
  const user = userEvent.setup()
  await user.click(await screen.findByRole("button", { name: "research" }))
  await user.click(
    await screen.findByRole("button", { name: "查看此 Account 的关联" })
  )
  await screen.findByRole("button", { name: "#6" })
  expect(screen.getByLabelText("当前查询")).toHaveTextContent(
    "account=research"
  )
  await user.click(screen.getByRole("button", { name: "#6" }))
  await screen.findByText("批处理")
  expect(screen.getByRole("button", { name: "校验并移除关联" })).toBeEnabled()
})
it("创建账户需校验、原因和完整确认，只发送固定确认请求", async () => {
  role(true)
  mock()
  show()
  const user = userEvent.setup()
  await user.click(await screen.findByRole("button", { name: "创建 Account" }))
  const dialog = screen.getByRole("alertdialog")
  await user.type(within(dialog).getByLabelText("Account"), "new-lab")
  await user.type(within(dialog).getByLabelText("描述"), "新项目")
  await user.type(within(dialog).getByLabelText("组织"), "Lab")
  await user.click(
    within(dialog).getByRole("button", { name: "校验最新记账状态" })
  )
  await within(dialog).findByText("请核对最新记账状态")
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p.endsWith("/execute"))
  ).toHaveLength(0)
  await user.type(within(dialog).getByLabelText("操作原因"), "新建项目账户")
  await user.type(within(dialog).getByLabelText("输入 new-lab 确认"), "wrong")
  await user.click(
    within(dialog).getByRole("button", { name: "确认创建 Account" })
  )
  expect(
    await within(dialog).findByText("确认文本与目标不一致")
  ).toBeInTheDocument()
  await user.clear(within(dialog).getByLabelText("输入 new-lab 确认"))
  await user.type(within(dialog).getByLabelText("输入 new-lab 确认"), "new-lab")
  await user.click(
    within(dialog).getByRole("button", { name: "确认创建 Account" })
  )
  await screen.findByText("已核实目标记账状态。")
  const calls = vi
    .mocked(api)
    .mock.calls.filter(([p]) => p.endsWith("/execute"))
  expect(calls).toHaveLength(1)
  expect(calls[0][1]?.body).toEqual({
    proof: "opaque-proof",
    confirmation: "new-lab",
    reason: "新建项目账户",
  })
  expect(screen.getByLabelText("当前查询")).toHaveTextContent(
    result.operationId
  )
})
it("缺少记账插件时明确显示错误，不展示旧的成功列表", async () => {
  mock()
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.includes("/accounts?"))
      throw new ClientResponseError({
        status: 503,
        response: { error: { message: "连接未开放 slurmdbd 记账接口" } },
      })
    return inventory as T
  })
  show()
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "连接未开放 slurmdbd 记账接口"
  )
  expect(
    screen.queryByRole("button", { name: "research" })
  ).not.toBeInTheDocument()
})
it("编辑 Account 保留只读名称，字段变化后必须重新校验", async () => {
  role(true)
  mock()
  show("/accounts?cluster=alpha&item=research")
  const user = userEvent.setup()
  await user.click(await screen.findByRole("button", { name: "编辑 Account" }))
  const dialog = screen.getByRole("alertdialog")
  expect(within(dialog).getByLabelText("Account")).toHaveValue("research")
  expect(within(dialog).getByLabelText("Account")).toHaveAttribute("readonly")
  await user.clear(within(dialog).getByLabelText("描述"))
  await user.type(within(dialog).getByLabelText("描述"), "新描述")
  await user.click(
    within(dialog).getByRole("button", { name: "校验最新记账状态" })
  )
  await within(dialog).findByText("请核对最新记账状态")
  const call = vi.mocked(api).mock.calls.find(([p]) => p.endsWith("/prepare"))
  expect(call?.[1]?.body).toMatchObject({
    action: "account_update",
    spec: { account: "research", description: "新描述", organization: "Lab" },
  })
  await user.type(within(dialog).getByLabelText("组织"), "2")
  expect(
    within(dialog).queryByRole("button", { name: "确认编辑 Account" })
  ).not.toBeInTheDocument()
})
