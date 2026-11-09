import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { ClientResponseError } from "pocketbase"
import { pb } from "./pocketbase"
import { api } from "./api"
import { queryClient } from "@/lib/query-client"
import { logout, useAuth, type PlatformUser } from "@/stores/auth"

const user: PlatformUser = {
  id: "alice",
  email: "alice@example.com",
  name: "Alice",
  username: "alice",
  authSource: "local",
  access: { platformRoles: [], platformPermissions: [], clusters: [] },
}

function signIn(token: string) {
  pb.authStore.save(token, {
    id: user.id,
    collectionId: "_pb_users_auth_",
    collectionName: "users",
  })
  useAuth.setState({ status: "authenticated", user: { ...user }, message: "" })
}

beforeEach(() => {
  logout()
  signIn("old-token")
})
afterEach(() => {
  vi.restoreAllMocks()
  logout()
})

it("当前会话收到 401 时退出并清除缓存", async () => {
  const failure = new ClientResponseError({ status: 401 })
  vi.spyOn(pb, "send").mockRejectedValue(failure)
  queryClient.setQueryData(["private"], "cached")
  await expect(api("/clusters")).rejects.toBe(failure)
  expect(useAuth.getState().status).toBe("anonymous")
  expect(pb.authStore.token).toBe("")
  expect(queryClient.getQueryData(["private"])).toBeUndefined()
})

it.each(["new-token", "old-token"])(
  "旧请求迟到的 401 不会退出新会话（令牌 %s）",
  async (token) => {
    let reject!: (error: unknown) => void
    vi.spyOn(pb, "send").mockImplementationOnce(
      () =>
        new Promise((_, fail) => {
          reject = fail
        })
    )
    const pending = api("/clusters")
    const failure = new ClientResponseError({ status: 401 })
    const assertion = expect(pending).rejects.toBe(failure)
    logout()
    signIn(token)
    queryClient.setQueryData(["private"], "new-session-data")
    reject(failure)
    await assertion
    expect(useAuth.getState().status).toBe("authenticated")
    expect(pb.authStore.token).toBe(token)
    expect(queryClient.getQueryData(["private"])).toBe("new-session-data")
  }
)

it("资源权限不足的 403 保留当前会话", async () => {
  const failure = new ClientResponseError({ status: 403 })
  vi.spyOn(pb, "send").mockRejectedValue(failure)
  await expect(api("/clusters")).rejects.toBe(failure)
  expect(useAuth.getState().status).toBe("authenticated")
})
