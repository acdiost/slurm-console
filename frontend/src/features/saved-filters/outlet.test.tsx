import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, waitFor, act } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import {
  MemoryRouter,
  Routes,
  Route,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router"
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query"
import { ClientResponseError, type SendOptions } from "pocketbase"
import { useState } from "react"
import { SavedFiltersOutlet } from "./outlet"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { cluster } from "@/features/overview/fixtures.test-support"
import {
  appliedSearch,
  conditionsFromURL,
  validConditions,
  historyWindow,
} from "./schema"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
let client: QueryClient
const saved = {
  id: "saved1",
  module: "jobs",
  clusterId: "alpha",
  name: "排队作业",
  conditions: { state: "PENDING", user: "alice" },
  isDefault: true,
  revision: 1,
  usable: true,
  compatible: true,
  updatedAt: "2026-10-31T01:00:00Z",
}
function role(id = "alice") {
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id,
      email: id + "@example.test",
      name: id,
      username: id,
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: [],
        clusters: [
          { cluster, roles: [], permissions: ["clusters.read", "jobs.read"] },
          {
            cluster: { ...cluster, id: "beta" },
            roles: [],
            permissions: ["clusters.read", "jobs.read"],
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
function Page() {
  const location = useLocation(),
    navigate = useNavigate(),
    [, setParams] = useSearchParams(),
    [editing, setEditing] = useState(false)
  useQuery({
    queryKey: ["sample-resource", location.search],
    queryFn: () => api("/sample-resource" + location.search),
    retry: false,
  })
  return (
    <>
      <output aria-label="实际条件">{location.search}</output>
      <button onClick={() => navigate("/jobs?cluster=beta")}>切换集群</button>
      <button
        onClick={() => {
          setParams({ cluster: "alpha" })
        }}
      >
        重新进入当前集群
      </button>
      <button onClick={() => setEditing(true)}>打开操作确认</button>
      {editing && <p>临时操作确认</p>}
    </>
  )
}
function show(url = "/jobs?cluster=alpha") {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route element={<SavedFiltersOutlet />}>
            <Route path="jobs" element={<Page />} />
            <Route path="history" element={<Page />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}
function mock(items: unknown[] = [saved]) {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.startsWith("/saved-filters?"))
      return {
        items: path.includes("beta") ? [] : items,
        maxPerScope: 10,
        maxPerUser: 100,
      } as T
    if (path.startsWith("/clusters?"))
      return { items: [cluster], page: 1, perPage: 20, totalItems: 1 } as T
    return {} as T
  })
}
it("restores a default before any resource query, and scopes implicit cluster selection", async () => {
  mock()
  show("/jobs")
  await screen.findByLabelText("实际条件")
  expect(screen.getByLabelText("实际条件")).toHaveTextContent("state=PENDING")
  const reads = vi
    .mocked(api)
    .mock.calls.filter(([p]) => p.startsWith("/sample-resource"))
  expect(reads.length).toBeGreaterThan(0)
  expect(
    reads.every(
      ([p]) => p.includes("state=PENDING") && p.includes("cluster=alpha")
    )
  ).toBe(true)
})
it("explicit URL conditions and detail/operation links take precedence over defaults", async () => {
  mock()
  show("/jobs?cluster=alpha&state=RUNNING&job=42&operation=existing")
  await screen.findByLabelText("实际条件")
  expect(screen.getByLabelText("实际条件")).toHaveTextContent("state=RUNNING")
  expect(screen.getByLabelText("实际条件")).toHaveTextContent(
    "operation=existing"
  )
  expect(
    vi
      .mocked(api)
      .mock.calls.filter(([p]) => p.startsWith("/sample-resource"))
      .every(([p]) => !p.includes("PENDING"))
  ).toBe(true)
})
it("applying or resetting clears selection-like component state and transient URL data", async () => {
  mock()
  show("/jobs?cluster=alpha&q=manual&page=7&job=42&batch=old&operations=old")
  await screen.findByLabelText("实际条件")
  await userEvent.click(screen.getByRole("button", { name: "打开操作确认" }))
  expect(screen.getByText("临时操作确认")).toBeInTheDocument()
  await userEvent.selectOptions(screen.getByLabelText("已保存方案"), "saved1")
  await userEvent.click(screen.getByRole("button", { name: "应用方案" }))
  expect(screen.queryByText("临时操作确认")).not.toBeInTheDocument()
  const output = screen.getByLabelText("实际条件")
  expect(output).toHaveTextContent("PENDING")
  expect(output).not.toHaveTextContent("operations=")
  expect(output).not.toHaveTextContent("page=")
  await userEvent.click(screen.getByRole("button", { name: "重置当前条件" }))
  expect(screen.getByLabelText("实际条件")).not.toHaveTextContent("PENDING")
  expect(screen.getByLabelText("实际条件")).toHaveTextContent(
    "filterMode=custom"
  )
})
it("saves only applied whitelisted conditions, including name and explicit default choice", async () => {
  mock([])
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: SendOptions) => {
      if (options?.method === "POST")
        return {
          ...saved,
          name: "我的方案",
          conditions: { q: "hello", state: "RUNNING", sort: "cpus" },
          isDefault: false,
        } as T
      if (path.startsWith("/saved-filters?"))
        return { items: [], maxPerScope: 10, maxPerUser: 100 } as T
      return {} as T
    }
  )
  show(
    "/jobs?cluster=alpha&q=hello&state=RUNNING&sort=cpus&page=9&job=42&operation=secret&batch=old&proof=secret"
  )
  await screen.findByLabelText("实际条件")
  await userEvent.click(screen.getByText("保存与管理方案"))
  await userEvent.type(screen.getByLabelText("方案名称"), "我的方案")
  await userEvent.click(screen.getByRole("button", { name: "另存方案" }))
  await screen.findByText("已保存当前已应用的列表条件。")
  const body = vi
    .mocked(api)
    .mock.calls.find(([, o]) => o?.method === "POST")![1]!.body
  expect(body).toEqual({
    module: "jobs",
    clusterId: "alpha",
    name: "我的方案",
    conditions: { q: "hello", state: "RUNNING", sort: "cpus" },
    isDefault: false,
  })
})
it("does not inherit another cluster or another user's saved defaults", async () => {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.startsWith("/saved-filters?"))
      return {
        items:
          path.includes("beta") || useAuth.getState().user?.id === "bob"
            ? []
            : [saved],
        maxPerScope: 10,
        maxPerUser: 100,
      } as T
    return {} as T
  })
  show()
  await screen.findByLabelText("实际条件")
  expect(screen.getByLabelText("实际条件")).toHaveTextContent("PENDING")
  await userEvent.click(screen.getByRole("button", { name: "切换集群" }))
  await waitFor(() =>
    expect(screen.getByLabelText("实际条件")).toHaveTextContent("cluster=beta")
  )
  expect(screen.getByLabelText("实际条件")).not.toHaveTextContent("PENDING")
  await act(async () => role("bob"))
  await userEvent.click(
    await screen.findByRole("button", { name: "重新进入当前集群" })
  )
  await waitFor(() =>
    expect(screen.getByLabelText("实际条件")).toHaveTextContent("cluster=alpha")
  )
  expect(screen.getByLabelText("实际条件")).not.toHaveTextContent("PENDING")
  expect(
    screen.queryByRole("option", { name: "排队作业（默认）" })
  ).not.toBeInTheDocument()
})
it("shows an unavailable preference source and keeps explicit URL filters usable", async () => {
  vi.mocked(api).mockImplementation(async <T,>(path: string) => {
    if (path.startsWith("/saved-filters?"))
      throw new ClientResponseError({ status: 503 })
    return {} as T
  })
  show("/jobs?cluster=alpha&state=RUNNING")
  await screen.findByRole("alert")
  expect(screen.getByLabelText("实际条件")).toHaveTextContent("RUNNING")
  expect(screen.getByRole("button", { name: "另存方案" })).toBeDisabled()
})
it("rejects unsafe state and preserves only context when building an applied link", () => {
  expect(validConditions("jobs", { operation: "bad" })).toBe(false)
  expect(validConditions("jobs", JSON.parse('{"__proto__":"bad"}'))).toBe(false)
  expect(validConditions("constructor", {})).toBe(false)
  expect(validConditions("fleet", { q: "集".repeat(128) })).toBe(true)
  expect(validConditions("fleet", { q: "集".repeat(129) })).toBe(false)
  expect(
    validConditions("audits", {
      from: "2026-10-31T00:00:00Z",
      to: "2026-10-31T00:00:00Z",
    })
  ).toBe(false)
  const p = new URLSearchParams(
    "cluster=alpha&clusterPage=2&job=42&operation=secret&page=9&batch=x"
  )
  expect(appliedSearch("jobs", p, { state: "PENDING" }).toString()).toBe(
    "cluster=alpha&clusterPage=2&state=PENDING&filterMode=custom"
  )
  expect(
    conditionsFromURL(
      "partitions",
      new URLSearchParams("q=cpu&partition=detail&nodesPage=2")
    )
  ).toEqual({ q: "cpu" })
})

it("ignores incompatible defaults and never restores operation state", async () => {
  mock([
    {
      ...saved,
      conditions: { operation: "private-control" },
      compatible: false,
      usable: false,
    },
  ])
  show()
  await screen.findByLabelText("实际条件")
  expect(screen.getByLabelText("实际条件")).not.toHaveTextContent(
    "private-control"
  )
  expect(screen.getByRole("button", { name: "应用方案" })).toBeDisabled()
  expect(vi.mocked(api).mock.calls.every(([, o]) => !o?.method)).toBe(true)
})
it("keeps current conditions after a revision conflict and does not retry writes", async () => {
  mock()
  const fallback = vi.mocked(api).getMockImplementation()!
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: SendOptions) => {
      if (options?.method === "PATCH")
        throw new ClientResponseError({ status: 409 })
      return fallback(path, options) as Promise<T>
    }
  )
  show("/jobs?cluster=alpha&state=RUNNING")
  await screen.findByRole("option", { name: "排队作业（默认）" })
  await userEvent.selectOptions(screen.getByLabelText("已保存方案"), "saved1")
  await userEvent.click(screen.getByText("保存与管理方案"))
  await userEvent.click(screen.getByRole("button", { name: "更新所选方案" }))
  await screen.findByText(
    "方案已变化、名称重复或达到数量限制，请刷新列表后重试。"
  )
  expect(screen.getByLabelText("实际条件")).toHaveTextContent("RUNNING")
  expect(
    vi.mocked(api).mock.calls.filter(([, o]) => o?.method === "PATCH")
  ).toHaveLength(1)
})
it("captures the displayed history window without carrying detail state", () => {
  const p = historyWindow(new URLSearchParams("cluster=alpha"), 1793401265123)
  expect(Number(p.get("end")) - Number(p.get("start"))).toBe(86400)
  expect(validConditions("history", conditionsFromURL("history", p))).toBe(true)
  p.set("record", "private-detail")
  const restored = appliedSearch("history", p, conditionsFromURL("history", p))
  expect(restored.get("start")).toBe(p.get("start"))
  expect(restored.has("record")).toBe(false)
  expect(
    validConditions("history", { start: "1700000000", end: "1800000000" })
  ).toBe(false)
})
