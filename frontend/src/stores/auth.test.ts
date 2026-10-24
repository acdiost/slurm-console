import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { pb } from "@/services/pocketbase"
import { queryClient } from "@/lib/query-client"
import {
  authErrorMessage,
  login,
  logout,
  restoreSession,
  useAuth,
} from "./auth"

const record = {
  id: "fixtureuser00001",
  collectionId: "_pb_users_auth_",
  collectionName: "users",
  email: "fixture@example.com",
  name: "Fixture",
  active: true,
}
const user = {
  id: record.id,
  email: record.email,
  name: record.name,
  access: { platformRoles: [], platformPermissions: [], clusters: [] },
}
export function fixtureToken(expiry = Math.floor(Date.now() / 1000) + 3600) {
  return `${btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }))}.${btoa(JSON.stringify({ id: record.id, exp: expiry, type: "auth", collectionId: record.collectionId }))}.fixture`
}
function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}
const fetchMock = vi.fn()
function successfulFetch(input: RequestInfo | URL) {
  return Promise.resolve(
    String(input).includes("/auth/me")
      ? json({ data: user })
      : json({ token: fixtureToken(), record })
  )
}

beforeEach(() => {
  logout()
  useAuth.setState({ status: "checking", user: null, message: "" })
  fetchMock.mockReset().mockImplementation(successfulFetch)
  vi.stubGlobal("fetch", fetchMock)
})
afterEach(() => {
  logout()
  vi.unstubAllGlobals()
})

describe("平台会话", () => {
  it.each(["pam", "ldap"])("以用户名登录并保留服务器返回的 %s 认证来源", async (source) => {
    const pamRecord = { ...record, username: "fixture_linux", auth_source: source }
    fetchMock.mockImplementation((input: RequestInfo | URL) =>
      Promise.resolve(
        String(input).includes("/auth/me")
          ? json({ data: { ...user, username: "fixture_linux", authSource: source } })
          : json({ token: fixtureToken(), record: pamRecord })
      )
    )
    await login("fixture_linux", "system-password")
    expect(JSON.parse(fetchMock.mock.calls[0][1].body).identity).toBe("fixture_linux")
    expect(useAuth.getState().user).toMatchObject({
      username: "fixture_linux",
      authSource: source,
    })
    expect(sessionStorage.getItem("slurm-console-session")).not.toContain("system-password")
  })
  it("复用 SDK 登录并校验当前用户，保存会话时不保存密码", async () => {
    queryClient.setQueryData(["private-data"], "old-account-data")
    await login(record.email, "fixture-password-123")
    expect(useAuth.getState()).toMatchObject({ status: "authenticated", user })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(queryClient.getQueryData(["private-data"])).toBeUndefined()
    expect(sessionStorage.getItem("slurm-console-session")).not.toContain(
      "fixture-password-123"
    )
    logout()
    expect(useAuth.getState().status).toBe("anonymous")
    expect(sessionStorage.getItem("slurm-console-session")).toBeNull()
  })
  it("拒绝错误凭据，给出不会枚举账户的提示", async () => {
    fetchMock.mockResolvedValue(
      json({ code: 400, message: "Failed to authenticate." }, 400)
    )
    let failure: unknown
    try {
      await login(record.email, "wrong")
    } catch (error) {
      failure = error
    }
    expect(authErrorMessage(failure)).toBe("用户名、邮箱或密码不正确，或账户不可用。")
    expect(pb.authStore.isValid).toBe(false)
    expect(useAuth.getState().status).toBe("anonymous")
  })
  it("刷新持久会话，并合并 StrictMode 的重复初始化", async () => {
    pb.authStore.save(fixtureToken(), record)
    const first = restoreSession()
    const second = restoreSession()
    expect(first).toBe(second)
    await first
    expect(fetchMock.mock.calls[0][0]).toContain("/auth-refresh")
    expect(useAuth.getState().status).toBe("authenticated")
  })
  it("已过期的令牌直接回到匿名状态", async () => {
    pb.authStore.save(fixtureToken(1), record)
    await restoreSession()
    expect(useAuth.getState().status).toBe("anonymous")
    expect(fetchMock).not.toHaveBeenCalled()
  })
  it("认证服务离线时阻止进入工作空间，但允许稍后重试", async () => {
    pb.authStore.save(fixtureToken(), record)
    fetchMock.mockRejectedValue(new TypeError("network unavailable"))
    await restoreSession()
    expect(useAuth.getState().status).toBe("unavailable")
    expect(pb.authStore.isValid).toBe(true)
    fetchMock.mockImplementation(successfulFetch)
    await restoreSession()
    expect(useAuth.getState().status).toBe("authenticated")
  })
  it("禁用账户的刷新失败会清除会话", async () => {
    pb.authStore.save(fixtureToken(), record)
    fetchMock.mockResolvedValue(json({ code: 403, message: "Forbidden" }, 403))
    await restoreSession()
    expect(useAuth.getState().status).toBe("anonymous")
    expect(pb.authStore.token).toBe("")
  })
  it("退出登录后，迟到的成功响应无法恢复会话", async () => {
    let resolveLogin!: (response: Response) => void
    fetchMock.mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          resolveLogin = resolve
        })
    )
    const pending = login(record.email, "fixture-password-123")
    logout()
    resolveLogin(json({ token: fixtureToken(), record }))
    await pending
    expect(useAuth.getState().status).toBe("anonymous")
    expect(pb.authStore.token).toBe("")
  })
})
