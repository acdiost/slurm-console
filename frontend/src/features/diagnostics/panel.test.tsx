import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { DiagnosticPanel } from "./panel"
import type { Diagnosis } from "./schema"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const unknown = { value: null, infinite: false }
const fixture: Diagnosis = {
  revision: 1,
  cached: false,
  report: {
    version: 1,
    source: "controller",
    observedAt: "2026-10-31T08:00:00Z",
    generatedAt: "2026-10-31T08:00:10Z",
    identity: {
      jobId: "42",
      recordKey: "",
      name: "diagnostic-job",
      user: "alice",
      account: "research",
      partition: "normal",
      qos: "normal",
      nodes: "",
      states: ["PENDING"],
      arrayJobId: unknown,
      arrayTaskId: unknown,
      hetJobId: unknown,
      hetJobOffset: unknown,
    },
    category: "waiting",
    headline: "作业正在等待调度",
    timeline: {
      sinceSubmitSeconds: 100,
      submitToStartSeconds: null,
      observedRunSeconds: null,
      reportedElapsedSeconds: null,
    },
    metrics: {
      allocatedCpus: null,
      totalCpuSeconds: null,
      cpuAccountingPercent: null,
    },
    coverage: { totalSteps: 0, abnormalSteps: 0, displayedSteps: 0 },
    evidence: [
      {
        id: "reason",
        field: "reason",
        label: "来源原因码",
        value: "Resources",
      },
    ],
    findings: [
      {
        code: "pending_reason",
        category: "waiting",
        severity: "info",
        confidence: "medium",
        kind: "inference",
        title: "原因码指向所请求资源不可用",
        explanation: "原因码只反映当前调度尝试，不保证唯一根因。",
        evidenceIds: ["reason"],
        checks: [
          {
            audience: "admin",
            text: "核对请求资源与候选节点，不自动改变约束。",
          },
        ],
      },
    ],
    limitations: ["未读取应用日志，不能证明性能退化。"],
  },
}
function role(permissions = ["jobs.read"]) {
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
        platformPermissions: [],
        platformRoles: [],
        clusters: [{ cluster, roles: [], permissions }],
      },
    },
  })
}
function show(history = false) {
  render(
    <QueryClientProvider client={client}>
      <DiagnosticPanel
        clusterId="alpha"
        jobId="42"
        source={history ? "accounting" : "controller"}
        recordKey={history ? "a".repeat(64) : ""}
        start={1700000000}
        end={1700086400}
      />
    </QueryClientProvider>
  )
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  role()
  vi.mocked(api).mockReset()
  vi.mocked(api).mockResolvedValue(fixture)
})
afterEach(() => client.clear())
it("loads only on demand and distinguishes evidence, interpretation, confidence and administrator checks", async () => {
  show()
  expect(vi.mocked(api)).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole("button", { name: "诊断作业" }))
  await screen.findByText("原因码指向所请求资源不可用")
  expect(screen.getByText("解释 / 待验证")).toBeInTheDocument()
  expect(screen.getByText("管理员核查")).toBeInTheDocument()
  await userEvent.click(screen.getByText("支撑证据（1）"))
  expect(screen.getAllByText("Resources").length).toBeGreaterThan(0)
  expect(vi.mocked(api).mock.calls[0][0]).toBe(
    "/clusters/alpha/jobs/42/diagnosis"
  )
  expect(vi.mocked(api).mock.calls.every(([, o]) => !o?.method)).toBe(true)
})
it("binds historical diagnosis to an exact record, job and submission window with only history permission", async () => {
  role(["job_history.read"])
  const f = structuredClone(fixture)
  f.report.source = "accounting"
  f.report.identity.recordKey = "a".repeat(64)
  vi.mocked(api).mockResolvedValue(f)
  show(true)
  await userEvent.click(screen.getByRole("button", { name: "诊断作业" }))
  await screen.findByText("作业正在等待调度")
  expect(vi.mocked(api).mock.calls[0][0]).toBe(
    `/clusters/alpha/job-history/${"a".repeat(64)}/diagnosis?jobId=42&start=1700000000&end=1700086400`
  )
  expect(screen.getByText(/历史记录标识/)).toBeInTheDocument()
})
it("hides cached findings when fresh access is revoked", async () => {
  show()
  await userEvent.click(screen.getByRole("button", { name: "诊断作业" }))
  await screen.findByText("原因码指向所请求资源不可用")
  vi.mocked(api).mockRejectedValue(new ClientResponseError({ status: 403 }))
  await userEvent.click(screen.getByRole("button", { name: "重新生成诊断" }))
  await screen.findByRole("alert")
  expect(
    screen.queryByText("原因码指向所请求资源不可用")
  ).not.toBeInTheDocument()
})
it("does not request diagnosis without matching source permission", () => {
  role(["job_history.read"])
  show()
  expect(screen.getByRole("button", { name: "诊断作业" })).toBeDisabled()
  expect(screen.getByRole("alert")).toHaveTextContent("没有此诊断来源")
  expect(vi.mocked(api)).not.toHaveBeenCalled()
})
it("preserves measured zero and array task zero without inventing missing CPU values", async () => {
  role(["job_history.read"])
  const f = structuredClone(fixture)
  f.report.source = "accounting"
  f.report.metrics.cpuAccountingPercent = 0
  f.report.identity.arrayJobId = { value: 7, infinite: false }
  f.report.identity.arrayTaskId = { value: 0, infinite: false }
  vi.mocked(api).mockResolvedValue(f)
  show(true)
  await userEvent.click(screen.getByRole("button", { name: "诊断作业" }))
  await screen.findByText("0.0%")
  expect(screen.getByText(/数组 Job \/ Task：7 \/ 0/)).toBeInTheDocument()
  expect(screen.getByText("— / —")).toBeInTheDocument()
})
it("clears evidence on source failure rather than retaining an old diagnosis", async () => {
  show()
  await userEvent.click(screen.getByRole("button", { name: "诊断作业" }))
  await screen.findByText("原因码指向所请求资源不可用")
  vi.mocked(api).mockRejectedValue(new ClientResponseError({ status: 503 }))
  await userEvent.click(screen.getByRole("button", { name: "重新生成诊断" }))
  await screen.findByRole("alert")
  expect(screen.queryByLabelText("诊断发现")).not.toBeInTheDocument()
})
