import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, within, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { emptySpec } from "@/features/accounting/schema"
import { TRESPage } from "./tres-page"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const resources = [
  { id: "1", type: "cpu", name: "", key: "cpu", unit: "CPU", editable: true },
  { id: "2", type: "mem", name: "", key: "mem", unit: "MiB", editable: true },
  {
    id: "1001",
    type: "gres",
    name: "gpu:a100",
    key: "gres/gpu:a100",
    unit: "个",
    editable: true,
  },
]
const target = {
  kind: "association" as const,
  id: "4",
  name: "",
  cluster: "production",
  account: "research",
  user: "alice",
  partition: "",
  relative: false,
  limits: {
    group_total: [
      { id: "1", value: "0", state: "finite" },
      { id: "2", value: "9007199254740993", state: "finite" },
    ],
    per_job: [],
    per_node: [],
    group_minutes: [],
    run_minutes: [],
    job_minutes: [],
  },
}
const meta = { fetchedAt: "2026-10-29T14:00:00Z", cached: false }
function role(edit = true, global = false) {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "actor",
      email: "actor@example.test",
      username: "actor",
      authSource: "local",
      name: "管理员",
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
              "tres.read",
              "associations.read",
              "qos.read",
              ...(edit
                ? ["tres.update", "associations.update", "qos.update"]
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
function State() {
  return <output aria-label="当前限额查询">{useLocation().search}</output>
}
function show(url = "/tres?cluster=alpha&item=4") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[url]}>
        <TRESPage />
        <State />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock(qos = false) {
  let spec: unknown
  const t = qos
    ? { ...target, kind: "qos", name: "normal", account: "", user: "" }
    : target
  const result = () => ({
    operationId: "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
    action: qos ? "qos_tres_update" : "association_tres_update",
    spec,
    slurmCluster: "production",
    status: "confirmed",
    message: "已核实目标限额",
    reason: "容量调整",
    createdAt: meta.fetchedAt,
    updatedAt: meta.fetchedAt,
  })
  vi.mocked(api).mockImplementation(
    async <T,>(p: string, options?: Parameters<typeof api>[1]) => {
      if (p.startsWith("/clusters?page=") || p === "/clusters?perPage=1")
        return { items: [cluster], page: 1, perPage: 20, totalItems: 1 } as T
      if (p.endsWith("/tres-operations")) return { items: [] } as T
      if (p.endsWith("/accounting-controls/prepare")) {
        spec = (options?.body as { spec: unknown }).spec
        return {
          ...result(),
          allowed: true,
          global: qos,
          confirmation: qos ? "normal" : "4",
          proof: "signed-tres-proof",
          expiresAt: "2099-10-29T14:02:00Z",
        } as T
      }
      if (
        p.endsWith("/accounting-controls/execute") ||
        p.includes("/accounting-operations/")
      )
        return result() as T
      if (p.includes("/tres-limits/"))
        return { item: t, resources, ...meta } as T
      return {
        items: [t],
        resources,
        slurmCluster: "production",
        page: 1,
        perPage: 20,
        totalItems: 25,
        ...meta,
      } as T
    }
  )
}
it("只读角色查看本层数值、超大内存精度及继承提示", async () => {
  role(false)
  mock()
  show()
  expect(
    await screen.findByRole("heading", { name: "research / alice" })
  ).toBeVisible()
  const table = screen.getByRole("table", { name: "本层资源限额" })
  expect(within(table).getByText("0")).toBeVisible()
  expect(within(table).getByText("9,007,199,254,740,993")).toBeVisible()
  expect(within(table).getByText("MiB")).toBeVisible()
  expect(
    screen.queryByRole("button", { name: "配置 TRES 限额" })
  ).not.toBeInTheDocument()
  expect(
    screen.getByRole("link", { name: "查看 Association" })
  ).toHaveAttribute("href", "/accounts?cluster=alpha&tab=associations&item=4")
})
it("设置大整数保持字符串，校验后需要原因与关联 ID 确认", async () => {
  mock()
  show()
  const u = userEvent.setup()
  await u.click(await screen.findByRole("button", { name: "配置 TRES 限额" }))
  const dialog = screen.getByRole("alertdialog")
  const input = within(dialog).getByRole("textbox", { name: "限额（CPU）" })
  await u.clear(input)
  await u.type(input, "9007199254740993")
  await u.click(within(dialog).getByRole("button", { name: "校验最新限额" }))
  expect(
    await within(dialog).findByRole("button", { name: "确认配置限额" })
  ).toBeVisible()
  expect(
    vi
      .mocked(api)
      .mock.calls.find(([p]) => p.endsWith("/accounting-controls/prepare"))?.[1]
      ?.body
  ).toEqual({
    action: "association_tres_update",
    spec: {
      ...emptySpec,
      tres: {
        kind: "association",
        targetId: "4",
        name: "",
        account: "research",
        user: "alice",
        partition: "",
        resourceId: "1",
        bucket: "group_total",
        mode: "set",
        value: "9007199254740993",
      },
    },
  })
  await u.type(
    within(dialog).getByRole("textbox", { name: "操作原因" }),
    "容量调整"
  )
  await u.type(
    within(dialog).getByRole("textbox", { name: "输入 4 确认" }),
    "4"
  )
  await u.click(within(dialog).getByRole("button", { name: "确认配置限额" }))
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.find(([p]) =>
          p.endsWith("/accounting-controls/execute")
        )?.[1]?.body
    ).toEqual({
      proof: "signed-tres-proof",
      confirmation: "4",
      reason: "容量调整",
    })
  )
})
it("清除动作显式提交 clear，切换资源使旧确认失效", async () => {
  mock()
  show()
  const u = userEvent.setup()
  await u.click(await screen.findByRole("button", { name: "配置 TRES 限额" }))
  const dialog = screen.getByRole("alertdialog")
  await u.click(within(dialog).getByRole("combobox", { name: "操作方式" }))
  await u.click(screen.getByRole("option", { name: "清除本层设置" }))
  await u.click(within(dialog).getByRole("button", { name: "校验最新限额" }))
  expect(
    await within(dialog).findByRole("button", { name: "确认配置限额" })
  ).toBeVisible()
  const body = vi
    .mocked(api)
    .mock.calls.find(([p]) => p.endsWith("/accounting-controls/prepare"))?.[1]
    ?.body as { spec: { tres: { mode: string; value: string } } }
  expect(body.spec.tres.mode).toBe("clear")
  expect(body.spec.tres.value).toBe("")
  await u.click(within(dialog).getByRole("combobox", { name: "资源" }))
  await u.click(screen.getByRole("option", { name: "mem · MiB" }))
  expect(
    within(dialog).queryByRole("button", { name: "确认配置限额" })
  ).not.toBeInTheDocument()
})
it("集群管理员可读 QOS 限额但没有全局写入口", async () => {
  mock(true)
  show("/tres?cluster=alpha&target=qos&item=4")
  expect(await screen.findByRole("heading", { name: "normal" })).toBeVisible()
  expect(
    screen.queryByRole("button", { name: "配置 TRES 限额" })
  ).not.toBeInTheDocument()
})
it("切换限额类型保留集群并清除对象与筛选", async () => {
  mock()
  show("/tres?cluster=alpha&q=research")
  const u = userEvent.setup()
  await u.click(await screen.findByRole("tab", { name: "QOS 限额" }))
  expect(screen.getByLabelText("当前限额查询").textContent).toContain(
    "cluster=alpha"
  )
  expect(screen.getByLabelText("当前限额查询").textContent).toContain(
    "target=qos"
  )
  expect(screen.getByLabelText("当前限额查询").textContent).not.toContain(
    "research"
  )
})
