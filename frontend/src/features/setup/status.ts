import { useQuery, type QueryClient } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { setupStatusSchema } from "./schema"

export function useSetupStatus() {
  const user = useAuth((s) => s.user)
  const permissions = user?.access.platformPermissions ?? []
  const canPAM = permissions.includes("platform.settings.manage")
  const canSlurm = permissions.includes("platform.clusters.manage")
  const allowed = canPAM || canSlurm
  const query = useQuery({
    queryKey: ["setup-status", user?.id, canPAM, canSlurm],
    enabled: allowed,
    retry: false,
    staleTime: 0,
    queryFn: async ({ signal }) =>
      setupStatusSchema.parse(await api("/setup/status", { signal })),
  })
  return { query, canPAM, canSlurm, allowed }
}

export function setupError(error: unknown) {
  if (error instanceof ClientResponseError) {
    if (error.status === 403)
      return "当前没有执行此操作的权限，请联系平台管理员。"
    if (error.status === 409)
      return "配置已被其他管理员更新。请刷新状态，核对后重新提交。"
    if (error.status === 0)
      return "无法连接平台服务，请检查网络后重试；如部署脚本重启了服务，请等待服务恢复。"
    return String(
      error.response?.error?.message || "操作未完成，请核对前置条件后重试。"
    )
  }
  return "服务返回格式不可用。请刷新状态核对保存结果后重试。"
}

export async function invalidateSetupQueries(client: QueryClient) {
  await Promise.all(
    [
      "setup-status",
      "cluster-fleet",
      "cluster-configurations",
      "cluster-connection-profiles",
      "cluster-inventory",
      "accessible-clusters",
      "cluster-metadata",
      "configuration-versions",
      "platform-runtime",
    ].map((key) => client.invalidateQueries({ queryKey: [key] }))
  )
}
