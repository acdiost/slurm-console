import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { SendOptions } from "pocketbase"
import { SetupPage } from "./setup-page"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import {
  slurmSetupSchema,
  type SlurmSetupValues,
} from "@/features/setup/schema"
import type {
  AuthenticationMethod,
  ClusterConfiguration,
} from "@/features/clusters/schema"

vi.mock("@/services/api", () => ({ api: vi.fn() }))

const pam = {
  revision: 0,
  configured: false,
  available: false,
  supported: true,
  helperInstalled: false,
  policyInstalled: false,
  message: "",
}
const status = {
  needsSetup: true,
  pam,
  slurm: { configured: false, clusterCount: 0, profileCount: 0 },
}
const localConnection: SlurmSetupValues = {
  name: "研究集群",
  slug: "research",
  authenticationMethod: "ldap",
  endpoint: "unix:///run/slurmrestd/slurmrestd.sock",
  authType: "local",
  username: "",
  token: "",
  caPem: "",
  apiVersion: "auto",
  timeoutSeconds: 5,
}
let client: QueryClient
let currentPAM = structuredClone(pam)

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  currentPAM = structuredClone(pam)
  useAuth.setState({
    status: "authenticated",
    message: "",
    user: {
      id: "admin",
      email: "admin@example.test",
      name: "Admin",
      username: "admin",
      authSource: "local",
      access: {
        platformRoles: [],
        platformPermissions: [
          "platform.clusters.manage",
          "platform.settings.manage",
          "platform.users.manage",
        ],
        clusters: [],
      },
    },
  })
  vi.mocked(api).mockReset()
  vi.mocked(api).mockImplementation(
    async <T,>(path: string, options?: SendOptions) => {
      if (path === "/setup/status")
        return { ...structuredClone(status), pam: currentPAM } as T
      if (path === "/setup/pam/configure") {
        currentPAM = {
          ...pam,
          revision: 1,
          configured: true,
          available: true,
          helperInstalled: true,
          policyInstalled: true,
        }
        return currentPAM as T
      }
      if (path === "/setup/slurm") {
        const values = options?.body as SlurmSetupValues
        const configuration: ClusterConfiguration = {
          id: "research0000001",
          name: values.name,
          slug: values.slug,
          description: "",
          authenticationMethod: values.authenticationMethod,
          connectionRef: "setup-research",
          apiVersion: "v0.0.43",
          enabled: true,
          timeoutSeconds: values.timeoutSeconds,
          revision: 1,
          endpoint: values.endpoint,
          authType: values.authType,
          profileAvailable: true,
          lastProbe: {
            status: "connected",
            message: "连接成功",
            checkedAt: "",
            durationMs: 1,
            serverVersion: "",
            controllers: [],
          },
        }
        return configuration as T
      }
      throw new Error(`Unexpected API call: ${path}`)
    }
  )
})
afterEach(() => client.clear())

function show() {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <SetupPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}
async function submitConnection() {
  await userEvent.type(screen.getByLabelText("集群名称"), localConnection.name)
  await userEvent.type(screen.getByLabelText("集群标识"), localConnection.slug)
  await userEvent.type(
    screen.getByLabelText("Unix socket 地址"),
    localConnection.endpoint
  )
  await userEvent.click(screen.getByRole("button", { name: "验证连接并保存" }))
  await screen.findByRole("heading", { name: "集群连接已保存" })
}

it("requires an explicit single authentication choice before preparing a connection", async () => {
  show()
  await screen.findByRole("group", { name: "身份认证方式" })
  expect(screen.getAllByRole("radio")).toHaveLength(3)
  expect(
    screen
      .getAllByRole("radio")
      .every((radio) => !(radio as HTMLInputElement).checked)
  ).toBe(true)
  await userEvent.click(screen.getByRole("button", { name: "继续配置" }))
  expect(await screen.findByRole("alert")).toBeVisible()
  expect(screen.queryByLabelText("集群名称")).not.toBeInTheDocument()
  expect(
    vi.mocked(api).mock.calls.every(([path]) => path === "/setup/status")
  ).toBe(true)
})

it.each([
  ["ldap", "LDAP"],
  ["freeipa", "FreeIPA"],
] as const)(
  "submits %s without preparing or enabling PAM",
  async (method: AuthenticationMethod, label) => {
    show()
    await screen.findByRole("radio", { name: label })
    await userEvent.click(screen.getByRole("radio", { name: "PAM 系统认证" }))
    await userEvent.click(screen.getByRole("radio", { name: label }))
    expect(
      screen.getByRole("radio", { name: "PAM 系统认证" })
    ).not.toBeChecked()
    expect(screen.getByRole("radio", { name: label })).toBeChecked()
    await userEvent.click(screen.getByRole("button", { name: "继续配置" }))
    expect(
      screen.queryByRole("button", { name: "一键配置并启用 PAM" })
    ).not.toBeInTheDocument()
    await submitConnection()
    expect(
      vi.mocked(api).mock.calls.filter(([path]) => path === "/setup/slurm")
    ).toHaveLength(1)
    expect(api).toHaveBeenCalledWith(
      "/setup/slurm",
      expect.objectContaining({
        body: expect.objectContaining({ authenticationMethod: method }),
      })
    )
    expect(
      vi.mocked(api).mock.calls.some(([path]) => path.startsWith("/setup/pam/"))
    ).toBe(false)
  }
)

it("preserves the selected method when returning from the connection step", async () => {
  show()
  await userEvent.click(await screen.findByRole("radio", { name: "FreeIPA" }))
  await userEvent.click(screen.getByRole("button", { name: "继续配置" }))
  await userEvent.click(
    screen.getByRole("button", { name: "返回身份认证设置" })
  )
  expect(screen.getByRole("radio", { name: "FreeIPA" })).toBeChecked()
  await userEvent.click(screen.getByRole("button", { name: "继续配置" }))
  await submitConnection()
  expect(api).toHaveBeenCalledWith(
    "/setup/slurm",
    expect.objectContaining({
      body: expect.objectContaining({ authenticationMethod: "freeipa" }),
    })
  )
})

it("retains PAM preparation only for a PAM cluster and keeps platform user management available", async () => {
  show()
  await userEvent.click(
    await screen.findByRole("radio", { name: "PAM 系统认证" })
  )
  await userEvent.click(screen.getByRole("button", { name: "继续配置" }))
  await userEvent.click(
    screen.getByRole("button", { name: "一键配置并启用 PAM" })
  )
  await waitFor(() =>
    expect(api).toHaveBeenCalledWith("/setup/pam/configure", {
      method: "POST",
      body: { revision: 0 },
    })
  )
  await screen.findByRole("link", { name: "管理用户与角色" })
  await userEvent.click(
    screen.getByRole("button", { name: /继续配置 slurmrestd|暂不启用，继续/ })
  )
  await submitConnection()
  expect(api).toHaveBeenCalledWith(
    "/setup/slurm",
    expect.objectContaining({
      body: expect.objectContaining({ authenticationMethod: "pam" }),
    })
  )
  expect(screen.getByRole("link", { name: "管理用户与角色" })).toHaveAttribute(
    "href",
    "/settings?tab=users"
  )
})

it("rejects missing, empty, unknown and multiple authentication methods in setup input", () => {
  for (const authenticationMethod of [
    undefined,
    "",
    "other",
    ["pam", "ldap"],
    null,
  ]) {
    expect(
      slurmSetupSchema.safeParse({ ...localConnection, authenticationMethod })
        .success
    ).toBe(false)
  }
  for (const authenticationMethod of ["pam", "ldap", "freeipa"]) {
    expect(
      slurmSetupSchema.safeParse({ ...localConnection, authenticationMethod })
        .success
    ).toBe(true)
  }
})
