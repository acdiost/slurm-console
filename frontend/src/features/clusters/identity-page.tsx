import { Fragment, type ReactNode } from "react"
import { Navigate, useLocation } from "react-router"
import { Button } from "@/components/ui/button"
import {
  ClusterPicker,
  useAccessibleClusters,
  type AccessibleClusters,
} from "./cluster-picker"
import { scopedNavigation } from "./cluster-navigation"
import { identityDestination, useClusterIdentity } from "./identity-navigation"
import type { AuthenticationMethod } from "./schema"

export function ClusterIdentityPage({
  path,
  children,
}: {
  path: "/linux-users" | "/directory"
  children: (
    selection: AccessibleClusters,
    method: AuthenticationMethod
  ) => ReactNode
}) {
  const selection = useAccessibleClusters()
  const identity = useClusterIdentity()
  const location = useLocation()
  const cluster =
    identity.query.isSuccess &&
    !identity.query.isPlaceholderData &&
    identity.query.data.id === identity.id
      ? identity.query.data
      : undefined
  const method = cluster?.authenticationMethod
  const destination = identityDestination(method)
  const error = identity.id
    ? identity.query.isError
    : selection.inventory.isError
  let content: ReactNode
  if (error) {
    content = (
      <div role="alert" className="panel m-5 space-y-3 p-5">
        <p>无法读取当前集群的身份认证绑定，请刷新后重试。</p>
        <Button
          variant="outline"
          onClick={() => {
            void selection.inventory.refetch()
            if (identity.id) void identity.query.refetch()
          }}
        >
          重新读取身份绑定
        </Button>
      </div>
    )
  } else if (!selection.id && !selection.inventory.isPending) {
    content = (
      <p role="status" className="panel m-5 p-5">
        本页没有可访问的集群。
      </p>
    )
  } else if (!cluster || identity.id !== selection.id) {
    content = (
      <p role="status" className="m-5 p-5 text-sm text-muted-foreground">
        正在读取集群身份认证绑定…
      </p>
    )
  } else if (!destination || !method) {
    content = (
      <p role="alert" className="panel m-5 p-5">
        此集群尚未绑定身份认证方式，请管理员在集群配置中选择 PAM、LDAP 或
        FreeIPA。
      </p>
    )
  } else if (destination !== path) {
    content = (
      <Navigate
        to={scopedNavigation(destination, location.pathname, location.search)}
        replace
      />
    )
  } else {
    content = (
      <Fragment key={`${identity.id}:${method}`}>
        {children(selection, method)}
      </Fragment>
    )
  }
  return (
    <>
      <ClusterPicker selection={selection} label="身份" />
      {content}
    </>
  )
}
