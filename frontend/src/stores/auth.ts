import PocketBase, { BaseAuthStore, ClientResponseError } from "pocketbase"
import { create } from "zustand"
import { z } from "zod"
import { pb } from "@/services/pocketbase"
import { queryClient } from "@/lib/query-client"

const clusterSchema = z.object({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
  description: z.string(),
})
export const accessSchema = z.object({
  platformRoles: z.array(z.string()),
  platformPermissions: z.array(z.string()),
  clusters: z.array(
    z.object({
      cluster: clusterSchema,
      roles: z.array(z.string()),
      permissions: z.array(z.string()),
    })
  ),
})
const userSchema = z.object({
  id: z.string().min(1),
  email: z.email(),
  name: z.string(),
  username: z.string().default(""),
  authSource: z.enum(["local", "pam"]).default("local"),
  access: accessSchema,
})
export type PlatformUser = z.infer<typeof userSchema>
type AuthState = {
  status: "checking" | "authenticated" | "anonymous" | "unavailable"
  user: PlatformUser | null
  message: string
}
export const useAuth = create<AuthState>(() => ({
  status: "checking",
  user: null,
  message: "",
}))

let activeFlow: AbortController | undefined
let restoration: Promise<void> | undefined

export function authErrorMessage(error: unknown) {
  if (error instanceof ClientResponseError) {
    if (error.status === 429) return "登录请求过于频繁，请稍后再试。"
    if ([400, 401, 403].includes(error.status))
      return "用户名、邮箱或密码不正确，或账户不可用。"
  }
  return "暂时无法连接认证服务，请稍后重试。"
}

function clearSession(message = "") {
  pb.authStore.clear()
  queryClient.clear()
  useAuth.setState({ status: "anonymous", user: null, message })
}

export function logout(message = "") {
  activeFlow?.abort()
  activeFlow = undefined
  restoration = undefined
  clearSession(message)
}

async function authenticate(credentials?: { identity: string; password: string }) {
  activeFlow?.abort()
  const controller = new AbortController()
  activeFlow = controller
  const timeout = setTimeout(() => controller.abort(), 10_000)
  // The temporary SDK client prevents a late response from restoring a logged-out
  // session. Only a completed, still-current flow updates the shared auth store.
  const client = new PocketBase("/", new BaseAuthStore())
  const options = { signal: controller.signal, requestKey: null }
  try {
    if (credentials)
      await client
        .collection("users")
        .authWithPassword(credentials.identity, credentials.password, options)
    else {
      client.authStore.save(pb.authStore.token, pb.authStore.record)
      await client.collection("users").authRefresh(options)
    }
    const response = await client.send<{ data: PlatformUser }>(
      "/api/v1/auth/me",
      { ...options, method: "GET" }
    )
    const user = userSchema.parse(response.data)
    if (activeFlow !== controller) return
    queryClient.clear()
    pb.authStore.save(client.authStore.token, client.authStore.record)
    useAuth.setState({ status: "authenticated", user, message: "" })
  } catch (error) {
    if (activeFlow !== controller) return
    if (credentials) throw error
    if (
      error instanceof ClientResponseError &&
      [400, 401, 403].includes(error.status)
    )
      clearSession("会话已失效，请重新登录。")
    else
      useAuth.setState({
        status: "unavailable",
        user: null,
        message: "无法校验当前会话，请检查认证服务后重试。",
      })
  } finally {
    clearTimeout(timeout)
    if (activeFlow === controller) activeFlow = undefined
  }
}

export function restoreSession(): Promise<void> {
  if (restoration) return restoration
  if (useAuth.getState().status === "authenticated") return Promise.resolve()
  if (
    !pb.authStore.isValid ||
    pb.authStore.record?.collectionName !== "users"
  ) {
    clearSession()
    return Promise.resolve()
  }
  useAuth.setState({ status: "checking", user: null, message: "" })
  const pending = authenticate()
  restoration = pending
  void pending.finally(() => {
    if (restoration === pending) restoration = undefined
  })
  return pending
}

export async function login(identity: string, password: string) {
  clearSession()
  await authenticate({ identity, password })
}
