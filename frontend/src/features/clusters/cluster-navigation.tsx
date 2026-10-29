import type { ComponentProps } from "react"
import { NavLink, useLocation } from "react-router"
const operational = new Set([
  "/overview",
  "/jobs",
  "/nodes",
  "/partitions",
  "/linux-users",
  "/accounts",
])
export function scopedNavigation(to: string, pathname: string, search: string) {
  if (!operational.has(to) || !operational.has(pathname)) return to
  const source = new URLSearchParams(search),
    id = source.get("cluster")
  if (!id) return to
  const next = new URLSearchParams({ cluster: id })
  for (const key of ["clusterPage", "clusterQ"])
    if (source.get(key)) next.set(key, source.get(key)!)
  return `${to}?${next}`
}
export function ClusterNavLink({
  to,
  ...props
}: Omit<ComponentProps<typeof NavLink>, "to"> & { to: string }) {
  const location = useLocation()
  return (
    <NavLink
      {...props}
      to={scopedNavigation(to, location.pathname, location.search)}
    />
  )
}
