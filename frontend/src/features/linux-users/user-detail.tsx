import { useQuery } from "@tanstack/react-query"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { api } from "@/services/api"
import { timestamp } from "@/features/overview/schema"
import { errorMessage, kinds, linuxUserDetailSchema, sources } from "./schema"
export function LinuxUserDetail({
  clusterId,
  name,
  onClose,
}: {
  clusterId: string
  name: string
  onClose: () => void
}) {
  const query = useQuery({
    queryKey: ["linux-user-detail", clusterId, name],
    enabled: !!clusterId && !!name,
    queryFn: async ({ signal }) =>
      linuxUserDetailSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/linux-users/${encodeURIComponent(name)}`,
          { signal }
        )
      ),
    retry: false,
    staleTime: 0,
  })
  const data = query.isError ? undefined : query.data
  return (
    <Sheet
      open={!!name}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>Linux 用户详情</SheetTitle>
          <SheetDescription>
            在当前集群的身份来源中精确查询 {name}。
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-5 px-4 pb-6">
          <Button
            variant="outline"
            size="sm"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            刷新用户详情
          </Button>
          {query.isPending && (
            <p role="status" className="text-sm text-muted-foreground">
              正在查询 Linux 用户…
            </p>
          )}
          {query.isError && (
            <p role="alert" className="text-sm text-destructive">
              {errorMessage(query.error)}
            </p>
          )}
          {data && (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="font-mono text-lg font-semibold break-all">
                  {data.item.name}
                </h2>
                <Badge variant="outline">{sources[data.item.source]}</Badge>
                <Badge variant="outline">{kinds[data.item.kind]}</Badge>
              </div>
              <dl className="divide-y rounded-md border px-4">
                {[
                  ["UID", String(data.item.uid)],
                  ["主组 GID", String(data.item.gid)],
                  ["描述", data.item.comment || "—"],
                  ["主目录", data.item.home || "—"],
                  ["Shell", data.item.shell || "—"],
                  ["查询来源", data.sourceLabel],
                  ["采样时间", timestamp(data.fetchedAt)],
                ].map(([label, value]) => (
                  <div
                    key={label}
                    className="grid grid-cols-[6rem_1fr] gap-3 py-3 text-xs"
                  >
                    <dt className="text-muted-foreground">{label}</dt>
                    <dd className="min-w-0 break-all">{value}</dd>
                  </div>
                ))}
              </dl>
              <p className="text-xs leading-6 text-muted-foreground">
                UID/GID 与目录和 Shell
                表示系统身份元数据，不代表该账户能够登录。密码、SSH、PAM
                及目录资格由系统策略决定。本地匹配表示 NSS 结果与本机 passwd
                记录一致，不判断 LDAP 或 FreeIPA 的具体目录类型。
              </p>
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
