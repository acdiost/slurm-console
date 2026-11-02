import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ClientResponseError, type SendOptions } from "pocketbase"
import { OperationTemplatesPage } from "./operation-templates-page"
import {
  targets,
  type OperationTemplate,
} from "@/features/operation-templates/schema"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import { fixtureJob } from "@/features/jobs/fixtures.test-support"
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
const template: OperationTemplate = {
  id: "templateone00001",
  clusterId: "alpha",
  name: "取消核查",
  action: "jobs.cancel",
  description: "结束明确作业前的核查",
  defaultReason: "用户确认取消",
  checklist: ["已确认作业归属"],
  revision: 1,
  updatedAt: "2026-10-31T12:00:00Z",
}
const prepared = {
  id: "batch-template-1234567890",
  action: "jobs.cancel",
  status: "ready",
  allowed: true,
  expiresAt: "2099-01-01T00:00:00Z",
  items: [
    {
      action: "jobs.cancel",
      target: "42",
      operationId: "operation-template-1234567890",
      allowed: true,
      status: "ready",
      code: "",
      message: "已校验",
      states: ["PENDING"],
      job: fixtureJob(),
      node: null,
    },
  ],
  counts: { ready: 1 },
}
function role(write = true) {
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
              "jobs.read",
              ...(write ? ["jobs.cancel"] : []),
            ],
          },
        ],
      },
    },
  })
}
function mock(items = [template]) {
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: SendOptions) => {
      if (path.startsWith("/clusters?"))
        return { items: [cluster], totalItems: 1, page: 1, perPage: 20 } as T
      if (path.endsWith("/prepare")) return { template, prepared } as T
      if (path.endsWith("/operation-templates") && options?.method === "POST")
        return template as T
      return { items, maxPerCluster: 20, maxPerUser: 100 } as T
    }
  )
}
function show() {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/operation-templates?cluster=alpha"]}>
        <OperationTemplatesPage />
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
  mock()
})
afterEach(() => client.clear())
it("never prepares or executes merely by listing or selecting a template", async () => {
  show()
  await screen.findByText("取消核查")
  await userEvent.click(
    screen.getByRole("button", { name: "使用模板 取消核查" })
  )
  expect(
    screen.getByRole("button", { name: "校验目标并准备确认" })
  ).toBeDisabled()
  expect(vi.mocked(api).mock.calls.some(([, o]) => o?.method === "POST")).toBe(
    false
  )
})
it("requires fresh targets and checklist before the existing explicit confirmation dialog", async () => {
  show()
  await screen.findByText("取消核查")
  await userEvent.click(
    screen.getByRole("button", { name: "使用模板 取消核查" })
  )
  await userEvent.type(screen.getByLabelText("本次模板目标"), "42")
  await userEvent.click(
    screen.getByRole("checkbox", { name: "已确认作业归属" })
  )
  await userEvent.click(
    screen.getByRole("button", { name: "校验目标并准备确认" })
  )
  await screen.findByLabelText("统一操作原因")
  expect(screen.getByLabelText("统一操作原因")).toHaveValue("用户确认取消")
  expect(screen.getByLabelText("输入 CANCEL 1 以确认")).toHaveValue("")
  expect(vi.mocked(api).mock.calls.some(([p]) => p.endsWith("/execute"))).toBe(
    false
  )
  const call = vi
    .mocked(api)
    .mock.calls.find(([p]) => p.includes("/templateone00001/prepare"))
  expect(call?.[1]?.body).toEqual({ revision: 1, targets: ["42"] })
})
it("resets human checklist when target selection changes", async () => {
  show()
  await screen.findByText("取消核查")
  await userEvent.click(
    screen.getByRole("button", { name: "使用模板 取消核查" })
  )
  await userEvent.type(screen.getByLabelText("本次模板目标"), "42")
  await userEvent.click(
    screen.getByRole("checkbox", { name: "已确认作业归属" })
  )
  expect(
    screen.getByRole("button", { name: "校验目标并准备确认" })
  ).toBeEnabled()
  await userEvent.type(screen.getByLabelText("本次模板目标"), ",43")
  expect(
    screen.getByRole("checkbox", { name: "已确认作业归属" })
  ).not.toBeChecked()
  expect(
    screen.getByRole("button", { name: "校验目标并准备确认" })
  ).toBeDisabled()
})
it("saves typed metadata without targets, source URLs or commands", async () => {
  mock([])
  show()
  await screen.findByText(/还没有本人模板/)
  await userEvent.click(screen.getByRole("button", { name: "新建模板" }))
  await userEvent.type(screen.getByLabelText("模板名称"), "新方案")
  await userEvent.type(
    screen.getByLabelText("核查清单（每行一项，最多 8 项）"),
    "已确认业务影响"
  )
  await userEvent.click(screen.getByRole("button", { name: "保存模板" }))
  await waitFor(() =>
    expect(
      vi.mocked(api).mock.calls.some(([, o]) => o?.method === "POST")
    ).toBe(true)
  )
  const body = vi
    .mocked(api)
    .mock.calls.find(([, o]) => o?.method === "POST")?.[1]?.body
  expect(body).toEqual({
    name: "新方案",
    action: "jobs.cancel",
    description: "",
    defaultReason: "",
    checklist: ["已确认业务影响"],
    revision: 0,
  })
})
it("hides cached templates on forbidden refresh", async () => {
  show()
  await screen.findByText("取消核查")
  vi.mocked(api).mockRejectedValue(new ClientResponseError({ status: 403 }))
  await userEvent.click(screen.getByRole("button", { name: "刷新模板" }))
  await screen.findByRole("alert")
  expect(screen.queryByText("取消核查")).not.toBeInTheDocument()
})
it("keeps viewers out of management and never fetches template data", async () => {
  role(false)
  show()
  await screen.findByRole("alert")
  expect(
    vi.mocked(api).mock.calls.some(([p]) => p.includes("operation-templates"))
  ).toBe(false)
})
it("rejects expressions, duplicate targets and over-limit selections", () => {
  expect(targets("nodes.drain", "node[1-2]")).toBeNull()
  expect(targets("jobs.cancel", "42,42")).toBeNull()
  expect(targets("jobs.cancel", "42_1")).toBeNull()
  expect(
    targets(
      "jobs.cancel",
      Array.from({ length: 11 }, (_, i) => String(i + 1)).join(",")
    )
  ).toBeNull()
  expect(targets("nodes.resume", "node1,node2")).toEqual(["node1", "node2"])
})
