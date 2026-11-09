import { ClientResponseError, type SendOptions } from "pocketbase"
import { pb } from "./pocketbase"
import { logout, useAuth } from "@/stores/auth"

export async function api<T>(
  path: string,
  options: SendOptions = {}
): Promise<T> {
  const token = pb.authStore.token
  const user = useAuth.getState().user
  try {
    const response = await pb.send<{ data: T }>(`/api/v1${path}`, {
      ...options,
      requestKey: null,
    })
    return response.data
  } catch (error) {
    // A response from an earlier login must not invalidate the current session.
    // Compare the user object too: a new login can receive the same token.
    if (
      error instanceof ClientResponseError &&
      error.status === 401 &&
      pb.authStore.token === token &&
      useAuth.getState().user === user
    )
      logout("会话已失效，请重新登录。")
    throw error
  }
}
