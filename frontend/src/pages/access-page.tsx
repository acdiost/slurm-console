import { useQuery } from "@tanstack/react-query"
import {
  KeyRound,
  RefreshCw,
  Server,
  ShieldCheck,
  ShieldOff,
  UserRound,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/services/api"
import { accessSchema, useAuth, type PlatformUser } from "@/stores/auth"

export function AccessPage() {
  const user = useAuth((state) => state.user)
  const {
    data: access,
    isPending,
    isError,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ["my-access"],
    queryFn: async ({ signal }) =>
      accessSchema.parse(
        (await api<PlatformUser>("/auth/me", { signal })).access
      ),
    staleTime: 0,
    retry: false,
  })
  return (
    <div className="mx-auto max-w-6xl space-y-6 p-6 sm:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="font-mono text-micro tracking-widest text-muted-foreground">
            IDENTITY & ACCESS
          </p>
          <h1 className="mt-2 text-2xl font-semibold">我的权限</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            查看平台角色与各集群的授权动作。
          </p>
        </div>
        <Button
          variant="outline"
          disabled={isFetching}
          onClick={() => void refetch()}
        >
          <RefreshCw
            className={
              isFetching ? "animate-spin motion-reduce:animate-none" : ""
            }
          />
          刷新权限
        </Button>
      </header>
      {user && (
        <div className="panel flex flex-wrap items-center gap-3 p-4">
          <UserRound className="size-5 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium">{user.name || user.email}</p>
            <p className="mt-1 text-2xs text-muted-foreground">
              {user.email}
              {user.username && ` · ${user.username}`}
            </p>
          </div>
          <Badge variant="outline">
            {
              {
                local: "平台密码认证",
                pam: "PAM 系统认证",
                ldap: "LDAP 目录认证",
              }[user.authSource]
            }
          </Badge>
        </div>
      )}
      <div className="rounded-lg border bg-primary/5 p-4 text-xs leading-6 text-muted-foreground">
        权限由管理员统一授予，服务器在每次请求时验证动作和集群范围。登录账户本身不授予集群管理权限。
      </div>
      {isPending && (
        <div role="status" aria-label="正在读取权限" className="space-y-3">
          <Skeleton className="h-24" />
          <Skeleton className="h-40" />
        </div>
      )}
      {isError && (
        <div role="alert" className="panel p-6 text-sm text-destructive">
          暂时无法读取权限，请重试。
        </div>
      )}
      {access && (
        <>
          <section className="panel">
            <div className="panel-heading">
              <h2 className="flex items-center gap-2">
                <ShieldCheck className="size-4 text-primary" />
                平台授权
              </h2>
              <Badge variant="outline">
                {access.platformRoles.length} 个角色
              </Badge>
            </div>
            <div className="space-y-4 p-5">
              {access.platformRoles.length ? (
                <>
                  <div className="flex flex-wrap gap-2">
                    {access.platformRoles.map((role) => (
                      <Badge key={role}>{role}</Badge>
                    ))}
                  </div>
                  <PermissionList values={access.platformPermissions} />
                  <p className="text-2xs text-muted-foreground">
                    平台授权中的集群动作适用于所有集群。配置管理需要平台级授权，Slurm
                    业务操作将在后续阶段接入。
                  </p>
                </>
              ) : (
                <p className="text-sm text-muted-foreground">
                  尚未授予平台级角色。
                </p>
              )}
            </div>
          </section>
          <section className="space-y-4">
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <Server className="size-4" />
              集群范围授权
              <Badge variant="secondary">{access.clusters.length}</Badge>
            </h2>
            {access.clusters.length ? (
              access.clusters.map((entry) => (
                <div key={entry.cluster.id} className="panel">
                  <div className="panel-heading">
                    <div>
                      <h2>{entry.cluster.name}</h2>
                      <p className="mt-1 font-mono text-2xs text-muted-foreground">
                        {entry.cluster.slug}
                      </p>
                    </div>
                    <div className="flex flex-wrap gap-2">
                      {entry.roles.map((role) => (
                        <Badge key={role} variant="outline">
                          {role}
                        </Badge>
                      ))}
                    </div>
                  </div>
                  <div className="p-5">
                    <PermissionList values={entry.permissions} />
                  </div>
                </div>
              ))
            ) : (
              <div className="panel flex flex-col items-center gap-3 border-dashed p-10 text-center">
                <ShieldOff className="size-6 text-muted-foreground" />
                <p className="text-sm font-medium">尚无单独的集群授权</p>
                <p className="text-xs text-muted-foreground">
                  需要访问集群时，请联系管理员。平台级授权另见上方。
                </p>
              </div>
            )}
          </section>
        </>
      )}
    </div>
  )
}

function PermissionList({ values }: { values: string[] }) {
  return values.length ? (
    <div className="flex flex-wrap gap-2">
      {values.map((permission) => (
        <Badge
          key={permission}
          variant="outline"
          className="gap-1.5 rounded-md font-mono text-2xs font-normal"
        >
          <KeyRound className="size-3" />
          {permission}
        </Badge>
      ))}
    </div>
  ) : (
    <p className="text-xs text-muted-foreground">未授予动作权限。</p>
  )
}
