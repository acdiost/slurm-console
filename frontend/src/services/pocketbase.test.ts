import { expect, it } from "vitest"
import { createPlatformClient } from "./pocketbase"

function token(exp: number) {
  return `e30.${btoa(JSON.stringify({ exp, type: "auth" }))}.fixture`
}
const record = {
  id: "fixtureuser00001",
  collectionId: "_pb_users_auth_",
  collectionName: "users",
}

it("只恢复有效的平台会话，损坏和超管数据不会当作平台用户", () => {
  const key = "slurm-console-session"
  sessionStorage.setItem(key, "{broken")
  expect(createPlatformClient(sessionStorage).authStore.token).toBe("")
  expect(sessionStorage.getItem(key)).toBeNull()
  sessionStorage.setItem(key, JSON.stringify({ token: token(1), record }))
  expect(createPlatformClient(sessionStorage).authStore.isValid).toBe(false)
  sessionStorage.setItem(
    key,
    JSON.stringify({
      token: token(Date.now() / 1000 + 3600),
      record: { ...record, collectionName: "_superusers" },
    })
  )
  expect(createPlatformClient(sessionStorage).authStore.isValid).toBe(false)
  sessionStorage.setItem(
    key,
    JSON.stringify({ token: token(Date.now() / 1000 + 3600), record })
  )
  expect(createPlatformClient(sessionStorage).authStore.isValid).toBe(true)
})

it("存储被禁用时使用内存会话，不回退到 localStorage", () => {
  const storage = {
    getItem: () => {
      throw Error("disabled")
    },
    setItem: () => {
      throw Error("disabled")
    },
    removeItem: () => {
      throw Error("disabled")
    },
  }
  const client = createPlatformClient(storage)
  expect(() =>
    client.authStore.save(token(Date.now() / 1000 + 3600), record)
  ).not.toThrow()
  expect(client.authStore.isValid).toBe(true)
  client.authStore.clear()
  expect(client.authStore.isValid).toBe(false)
  expect(localStorage.getItem("slurm-console-session")).toBeNull()
})
