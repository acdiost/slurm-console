import { useQuery } from "@tanstack/react-query"
import { useLocation } from "react-router"
import { clusterSchema } from "@/features/overview/schema"
import { api } from "@/services/api"
import { isClusterScopedPath } from "./cluster-navigation"

export function identityDestination(method: string | undefined) {
  switch (method) {
    case "pam":
    case "ldap":
      return "/linux-users"
    case "freeipa":
      return "/directory"
    default:
      return null
  }
}

export function identityLabel(method: string | undefined) {
  switch (method) {
    case "pam":
      return "PAM"
    case "ldap":
      return "LDAP"
    case "freeipa":
      return "FreeIPA"
    default:
      return ""
  }
}

export function useClusterIdentity() {
  const { pathname, search } = useLocation()
  const id = isClusterScopedPath(pathname)
    ? new URLSearchParams(search).get("cluster") || ""
    : ""
  const query = useQuery({
    queryKey: ["cluster-metadata", id],
    enabled: !!id,
    queryFn: async ({ signal }) =>
      clusterSchema.parse(
        await api(`/clusters/${encodeURIComponent(id)}`, { signal })
      ),
    retry: false,
  })
  const cluster =
    id &&
    query.isSuccess &&
    !query.isFetching &&
    !query.isPlaceholderData &&
    query.data.id === id
      ? query.data
      : undefined
  return { id, cluster, query }
}
