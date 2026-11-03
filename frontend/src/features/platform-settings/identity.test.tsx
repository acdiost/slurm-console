import { beforeEach, afterEach, it, expect, vi } from "vitest"
import { render, screen, cleanup, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router"
import { PlatformIdentityLoader, usePlatformIdentity } from "./identity"
import { defaults } from "./schema"
import { LoginPage } from "@/pages/login-page"
import { useAuth } from "@/stores/auth"
let client: QueryClient
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  useAuth.setState({ status: "anonymous", user: null, message: "" })
  usePlatformIdentity.setState(defaults)
})
afterEach(() => {
  cleanup()
  client.clear()
  vi.unstubAllGlobals()
  usePlatformIdentity.setState(defaults)
})
function show() {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <PlatformIdentityLoader />
        <LoginPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
it("loads only public identity without session credentials or blocking the login form", async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValue(
      new Response(
        JSON.stringify({
          data: {
            ...defaults,
            name: "独立算力平台",
            loginNotice: "<b>维护公告</b>",
            supportContact: "contact@example.test",
          },
        })
      )
    )
  vi.stubGlobal("fetch", fetchMock)
  show()
  expect(screen.getByRole("button", { name: "登录" })).toBeEnabled()
  expect(await screen.findByText("<b>维护公告</b>")).toBeInTheDocument()
  expect(screen.getAllByText("独立算力平台")).toHaveLength(2)
  expect(fetchMock).toHaveBeenCalledTimes(1)
  expect(fetchMock.mock.calls[0][1]).toMatchObject({
    credentials: "omit",
    cache: "no-store",
  })
  expect(fetchMock.mock.calls[0][1].headers).toBeUndefined()
})
it("clears stale public notice and keeps login available when metadata cannot be read", async () => {
  usePlatformIdentity.setState({
    ...defaults,
    name: "旧名称",
    loginNotice: "过期公告",
  })
  const fetchMock = vi
    .fn()
    .mockResolvedValue(new Response("{}", { status: 503 }))
  vi.stubGlobal("fetch", fetchMock)
  show()
  await waitFor(() =>
    expect(screen.queryByText("过期公告")).not.toBeInTheDocument()
  )
  expect(screen.getAllByText("Slurm Console")).toHaveLength(2)
  expect(screen.getByRole("button", { name: "登录" })).toBeEnabled()
  expect(fetchMock).toHaveBeenCalledTimes(1)
})
