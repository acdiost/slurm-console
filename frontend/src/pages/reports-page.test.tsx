import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { ReportsPage } from "./reports-page"
import { csv, hours, sorted, type UsageReport } from "@/features/reports/schema"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
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
const amount = { resourceSeconds: "7200", knownRecords: 1, unknownRecords: 1 }
const row = {
  key: "alice",
  records: 2,
  countedRecords: 1,
  excluded: { active: 1, complex: 0, uncertain: 0, overlapping: 0 },
  cpu: amount,
  memory: { ...amount, resourceSeconds: "7372800" },
  gpu: { resourceSeconds: null, knownRecords: 0, unknownRecords: 2 },
}
const data: UsageReport = {
  report: {
    version: 1,
    basis: "execution_overlap_estimate",
    cluster: "fixture",
    start: 1700000000,
    end: 1700086400,
    groupBy: "user",
    sourceRecords: 2,
    outsideRecords: 0,
    summary: { ...row, key: "" },
    groups: [row],
  },
  fetchedAt: "2026-11-02T00:00:00Z",
  cached: false,
  revision: 1,
}
function role(report = true, history = true) {
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
            permissions: [
              "clusters.read",
              ...(report ? ["reports.read"] : []),
              ...(history ? ["job_history.read"] : []),
            ],
          },
        ],
      },
    },
  })
}
function show(query = "cluster=alpha&start=1700000000&end=1700086400") {
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/reports?${query}`]}>
        <ReportsPage />
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
  vi.mocked(api).mockImplementation(async <T,>(path: string) =>
    path.startsWith("/clusters?")
      ? ({ items: [cluster], totalItems: 1, page: 1, perPage: 20 } as T)
      : (structuredClone(data) as T)
  )
})
afterEach(() => {
  client.clear()
  vi.restoreAllMocks()
})
it("shows window estimate, per-metric coverage, and excluded records without automatic writes", async () => {
  show()
  await screen.findByRole("heading", { name: "按用户汇总" })
  expect(
    within(screen.getByRole("region", { name: "报表汇总" })).getAllByText(
      "2.000"
    )
  ).toHaveLength(2)
  expect(screen.getAllByText("仅已知部分，非完整总量")).toHaveLength(3)
  expect(screen.getByRole("region", { name: "未计量记录" })).toHaveTextContent(
    "活动记录 1"
  )
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p.includes("/reports/usage"))
  ).toHaveLength(1)
  expect(
    vi.mocked(api).mock.calls.every(([, o]) => !o?.method || o.method === "GET")
  ).toBe(true)
})
it("requires both independent report and history permissions", async () => {
  role(false, true)
  const v = show()
  await screen.findByText("需要此集群的报表和历史记账读取权限。")
  expect(
    vi.mocked(api).mock.calls.some(([p]) => p.includes("/reports/usage"))
  ).toBe(false)
  v.unmount()
  role(true, false)
  show()
  await screen.findByText("需要此集群的报表和历史记账读取权限。")
  expect(
    vi.mocked(api).mock.calls.some(([p]) => p.includes("/reports/usage"))
  ).toBe(false)
})
it("hides cached results and refuses export after denied refresh", async () => {
  show()
  await screen.findByRole("heading", { name: "按用户汇总" })
  vi.mocked(api).mockRejectedValue(
    new ClientResponseError({ status: 403, response: {} })
  )
  await userEvent.click(screen.getByRole("button", { name: "刷新报表" }))
  await screen.findByText("报表或历史记账读取权限不可用。")
  expect(
    screen.queryByRole("region", { name: "报表汇总" })
  ).not.toBeInTheDocument()
  expect(
    screen.getByRole("button", { name: "导出全部分组 CSV" })
  ).toBeDisabled()
})
it("reauthorizes export instead of downloading cached data on denial", async () => {
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(() => {})
  show()
  await screen.findByRole("heading", { name: "按用户汇总" })
  vi.mocked(api).mockRejectedValue(
    new ClientResponseError({ status: 403, response: {} })
  )
  await userEvent.click(
    screen.getByRole("button", { name: "导出全部分组 CSV" })
  )
  await screen.findByText("报表或历史记账读取权限不可用。")
  expect(click).not.toHaveBeenCalled()
  expect(
    vi.mocked(api).mock.calls.filter(([p]) => p.includes("/reports/usage"))
  ).toHaveLength(2)
})
it("changes exact source filters and dimension through explicit form submission", async () => {
  show()
  await screen.findByRole("heading", { name: "按用户汇总" })
  await userEvent.type(screen.getByLabelText("Account精确筛选"), "research")
  await userEvent.selectOptions(screen.getByLabelText("汇总维度"), "account")
  await userEvent.click(screen.getByRole("button", { name: "生成报表" }))
  await waitFor(() =>
    expect(
      vi
        .mocked(api)
        .mock.calls.some(
          ([p]) =>
            p.includes("groupBy=account") && p.includes("account=research")
        )
    ).toBe(true)
  )
})
it("rejects an invalid URL window without querying accounting", async () => {
  show("cluster=alpha&start=1&end=9999999999")
  await screen.findByText("运行窗口或汇总维度不合法，请重新选择。")
  expect(
    vi.mocked(api).mock.calls.some(([p]) => p.includes("/reports/usage"))
  ).toBe(false)
})
it("keeps large values exact, unknowns last, and exports every group safely", () => {
  expect(hours("9007199254740993000")).toBe("2,501,999,792,983,609.167")
  expect(hours(null)).toBe("未知")
  expect(hours("0")).toBe("0.000")
  expect(hours("1")).toBe("<0.001")
  const a = {
      ...row,
      key: "smaller",
      cpu: { ...amount, resourceSeconds: "9007199254740992" },
    },
    b = {
      ...row,
      key: "larger",
      cpu: { ...amount, resourceSeconds: "9007199254740993" },
    }
  expect(
    sorted(
      [
        a,
        b,
        { ...row, key: "unknown", cpu: { ...amount, resourceSeconds: null } },
      ],
      "cpu",
      "desc"
    ).map((v) => v.key)
  ).toEqual(["larger", "smaller", "unknown"])
  const report = structuredClone(data)
  report.report.groups = Array.from({ length: 25 }, (_, i) => ({
    ...row,
    key: i === 0 ? ' =HYPERLINK("x")' : `user${i}`,
  }))
  const out = csv(report, "alpha")
  expect(out.split("\r\n")).toHaveLength(28)
  expect(out).toContain(`"' =HYPERLINK(""x"")"`)
  expect(out).toContain('"user24"')
  expect(out).toContain("memory_mib_seconds_estimate")
  expect(out).not.toContain("undefined")
})
