import { ClientResponseError, type SendOptions } from "pocketbase"
import { pb } from "./pocketbase"
import { logout } from "@/stores/auth"

export async function api<T>(
  path: string,
  options: SendOptions = {}
): Promise<T> {
  try {
    const response = await pb.send<{ data: T }>(`/api/v1${path}`, {
      ...options,
      requestKey: null,
    })
    return response.data
  } catch (error) {
    if (error instanceof ClientResponseError && error.status === 401)
      logout("会话已失效，请重新登录。")
    throw error
  }
}
