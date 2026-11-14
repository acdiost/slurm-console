import { beforeEach, describe, expect, it, vi } from "vitest"
import { act, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ConnectionForm } from "./connection-form"
import { api } from "@/services/api"
vi.mock("@/services/api", () => ({ api: vi.fn() }))
beforeEach(() => {
  vi.mocked(api).mockReset()
})
import {
  connectionFormSchema,
  connectionStatus,
  type ClusterConfiguration,
} from "./schema"

const profiles = [
  {
    ref: "local",
    label: "本地集群",
    endpoint: "unix:///run/slurm/rest.sock",
    authType: "local" as const,
    apiVersions: ["v0.0.43"],
  },
]
const configuration: ClusterConfiguration = {
  id: "cluster1",
  name: "科研集群",
  slug: "research",
  description: "",
  connectionRef: "local",
  apiVersion: "v0.0.43",
  enabled: true,
  timeoutSeconds: 5,
  revision: 3,
  endpoint: profiles[0].endpoint,
  authType: "local",
  profileAvailable: true,
  lastProbe: {
    status: "connected",
    message: "成功",
    checkedAt: "",
    durationMs: 1,
    serverVersion: "25.11.4",
    controllers: [],
  },
}

describe("集群连接配置", () => {
  it("切换连接后丢弃旧连接的检测响应", async () => {
    let resolve!: (value: unknown) => void
    vi.mocked(api).mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done
        })
    )
    const user = userEvent.setup()
    render(
      <ConnectionForm
        config={configuration}
        profiles={[
          ...profiles,
          { ...profiles[0], ref: "other", label: "另一集群" },
        ]}
        saving={false}
        error=""
        onSave={vi.fn()}
        onCancel={vi.fn()}
      />
    )
    await user.click(screen.getByRole("button", { name: "检测可用 API 版本" }))
    await user.click(screen.getByLabelText("连接方式"))
    await user.click(screen.getByRole("option", { name: /另一集群/ }))
    await act(async () => {
      resolve({ versions: ["v0.0.40"], recommended: "v0.0.40" })
    })
    expect(screen.getByLabelText("REST API 版本")).toHaveTextContent("v0.0.43")
    expect(screen.queryByRole("status")).not.toBeInTheDocument()
    expect(vi.mocked(api).mock.calls[0][1]?.signal?.aborted).toBe(true)
  })
  it("读取 slurmrestd 可用版本并为旧集群选择 v0.0.40", async () => {
    vi.mocked(api).mockResolvedValue({
      versions: ["v0.0.40"],
      recommended: "v0.0.40",
    })
    const save = vi.fn(),
      user = userEvent.setup()
    render(
      <ConnectionForm
        config={configuration}
        profiles={[{ ...profiles[0], apiVersions: ["v0.0.43", "v0.0.40"] }]}
        saving={false}
        error=""
        onSave={save}
        onCancel={vi.fn()}
      />
    )
    await user.click(screen.getByRole("button", { name: "检测可用 API 版本" }))
    expect(await screen.findByRole("status")).toHaveTextContent("v0.0.40")
    expect(screen.getByLabelText("REST API 版本")).toHaveTextContent("v0.0.40")
    await user.click(screen.getByRole("button", { name: "保存配置" }))
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({ apiVersion: "v0.0.40", revision: 3 }),
      expect.anything()
    )
    expect(api).toHaveBeenCalledWith(
      "/cluster-connection-profiles/local/versions",
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })
  it("版本检测失败时保留原版本并允许手动保存", async () => {
    vi.mocked(api).mockRejectedValue(new Error("unavailable"))
    const user = userEvent.setup()
    render(
      <ConnectionForm
        config={configuration}
        profiles={profiles}
        saving={false}
        error=""
        onSave={vi.fn()}
        onCancel={vi.fn()}
      />
    )
    await user.click(screen.getByRole("button", { name: "检测可用 API 版本" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("版本检测失败")
    expect(screen.getByLabelText("REST API 版本")).toHaveTextContent("v0.0.43")
    expect(screen.getByRole("button", { name: "保存配置" })).toBeEnabled()
  })
  it("拒绝空名称，只提交配置引用和非敏感字段", async () => {
    const user = userEvent.setup()
    const save = vi.fn()
    render(
      <ConnectionForm
        config={null}
        profiles={profiles}
        saving={false}
        error=""
        onSave={save}
        onCancel={vi.fn()}
      />
    )
    await user.click(screen.getByRole("button", { name: "保存配置" }))
    expect(await screen.findByText("名称至少 2 个字符")).toBeVisible()
    expect(save).not.toHaveBeenCalled()
    await user.type(screen.getByLabelText("集群名称"), "科研集群")
    await user.type(screen.getByLabelText("集群标识"), "research")
    await user.click(screen.getByRole("button", { name: "保存配置" }))
    expect(save).toHaveBeenCalledWith(
      {
        name: "科研集群",
        slug: "research",
        description: "",
        connectionRef: "local",
        apiVersion: "v0.0.43",
        enabled: true,
        timeoutSeconds: 5,
        revision: 0,
      },
      expect.anything()
    )
  })
  it("编辑保留不可变标识，显示服务端冲突信息", () => {
    render(
      <ConnectionForm
        config={configuration}
        profiles={profiles}
        saving={false}
        error="配置已被修改，请刷新后重试。"
        onSave={vi.fn()}
        onCancel={vi.fn()}
      />
    )
    expect(screen.getByLabelText("集群标识")).toHaveAttribute("readonly")
    expect(screen.getByLabelText("集群标识")).toHaveValue("research")
    expect(screen.getByRole("alert")).toHaveTextContent("配置已被修改")
  })
  it("超时必须为 1–30 秒，额外凭据字段不会进入提交对象", () => {
    expect(
      connectionFormSchema.safeParse({ ...configuration, timeoutSeconds: 0 })
        .success
    ).toBe(false)
    expect(
      connectionFormSchema.safeParse({ ...configuration, timeoutSeconds: 31 })
        .success
    ).toBe(false)
    const parsed = connectionFormSchema.parse({
      ...configuration,
      token: "discard",
      endpoint: "https://unapproved.example",
    })
    expect(parsed).not.toHaveProperty("token")
    expect(parsed).not.toHaveProperty("endpoint")
    expect(parsed.revision).toBe(3)
  })
  it("停用及连接方式失效时不会继续显示旧的成功状态", () => {
    expect(connectionStatus(configuration).label).toBe("连接正常")
    expect(connectionStatus({ ...configuration, enabled: false }).label).toBe(
      "已停用"
    )
    expect(
      connectionStatus({ ...configuration, profileAvailable: false }).label
    ).toBe("连接方式不可用")
    expect(
      connectionStatus({ ...configuration, connectionRef: "" }).label
    ).toBe("未配置")
  })
})
