import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import { RefreshCw, ExternalLink, Filter } from "lucide-react"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from "@/components/ui/table"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import {
  actions,
  fields,
  auditDetailSchema,
  dateTime,
  errorMessage,
  resourceLink,
} from "./schema"
import { AuditStatus } from "./audit-status"
export function AuditDetail({
  id,
  onClose,
  onFilter,
}: {
  id: string
  onClose: () => void
  onFilter: (key: string, value: string) => void
}) {
  const user = useAuth((s) => s.user)
  const query = useQuery({
    queryKey: ["operation-audit", id],
    queryFn: async ({ signal }) =>
      auditDetailSchema.parse(
        await api(`/operation-audits/${encodeURIComponent(id)}`, { signal })
      ),
    retry: false,
    staleTime: 0,
  })
  const data = query.isError ? undefined : query.data,
    item = data?.item
  const permissions = item
    ? [
        ...(user?.access.platformPermissions || []),
        ...(user?.access.clusters.find((v) => v.cluster.id === item.clusterId)
          ?.permissions || []),
      ]
    : []
  const link = item && resourceLink(item, permissions)
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent className="overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-3xl">
        <SheetHeader>
          <SheetTitle>操作审计详情</SheetTitle>
          <SheetDescription>
            查看已持久化的证据。刷新只读取平台记录，不向 Slurm 发送请求。
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-5 px-5 pb-6">
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
              刷新审计详情
            </Button>
            {link && (
              <Button asChild size="sm" variant="outline">
                <Link to={link}>
                  查看当前资源
                  <ExternalLink />
                </Link>
              </Button>
            )}
          </div>
          {query.isPending && <Skeleton className="h-64" />}
          {query.isError && (
            <p
              role="alert"
              className="rounded-md border p-4 text-xs text-destructive"
            >
              {errorMessage(query.error)}
            </p>
          )}
          {item && (
            <>
              <section className="space-y-3 rounded-md border bg-muted/20 p-4">
                <div className="flex flex-wrap items-center gap-3">
                  <h2 className="text-sm font-semibold">
                    {actions[item.action] || item.action}
                  </h2>
                  <AuditStatus status={item.status} />
                </div>
                <p className="text-xs leading-6 break-all whitespace-pre-wrap">
                  {item.message || "未记录结果说明"}
                </p>
                {item.recordedStatus === "processing" && (
                  <p className="text-2xs text-muted-foreground">
                    持久状态为 processing，不能据此判断原进程仍在执行。
                  </p>
                )}
              </section>
              <dl className="grid gap-5 sm:grid-cols-2">
                {[
                  ["操作编号", item.operationId],
                  ["请求编号", item.requestId],
                  ["操作人", item.actorLabel || "历史记录未保存名称快照"],
                  ["操作人 ID", item.actorId],
                  [
                    "操作来源",
                    (
                      {
                        platform: "平台用户",
                        local: "本机管理命令（进程有效 UID）",
                        legacy: "历史记录，来源未快照",
                      } as Record<string, string>
                    )[item.actorSource] || item.actorSource,
                  ],
                  ["集群", item.clusterName || item.clusterId || "平台范围"],
                  ["集群 ID", item.clusterId],
                  ["集群标识", item.clusterSlug],
                  [
                    "连接配置版本",
                    item.clusterId ? String(item.clusterRevision) : "",
                  ],
                  [
                    "目标类型",
                    (
                      {
                        job: "作业",
                        node: "节点",
                        cluster: "集群",
                        user: "用户",
                      } as Record<string, string>
                    )[item.targetType] || item.targetType,
                  ],
                  ["目标 ID", item.targetId],
                  ["动作代码", item.action],
                  ["记录创建时间", dateTime(item.createdAt)],
                  ["最近记录更新时间", dateTime(item.updatedAt)],
                ].map(([label, value]) => (
                  <div key={label}>
                    <dt className="text-2xs text-muted-foreground">{label}</dt>
                    <dd className="mt-1.5 font-mono text-xs leading-6 break-all whitespace-pre-wrap">
                      {value || "未记录/不适用"}
                    </dd>
                  </div>
                ))}
              </dl>
              <div className="flex flex-wrap gap-2">
                {item.clusterId && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => onFilter("cluster", item.clusterId)}
                  >
                    <Filter />
                    按此集群筛选
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => onFilter("actor", item.actorId)}
                >
                  <Filter />
                  按此操作人筛选
                </Button>
              </div>
              <section className="space-y-2 border-t pt-4">
                <h2 className="text-xs font-semibold">操作原因</h2>
                <p className="text-xs leading-6 break-all whitespace-pre-wrap">
                  {item.reason || "未记录/该动作不要求填写原因"}
                </p>
              </section>
              {item.states.length > 0 && (
                <section className="space-y-2 border-t pt-4">
                  <h2 className="text-xs font-semibold">
                    记录时观察到的 Slurm 状态
                  </h2>
                  <p className="font-mono text-xs break-all">
                    {item.states.join(", ")}
                  </p>
                </section>
              )}
              {item.changes.length > 0 && (
                <section className="space-y-3 border-t pt-4">
                  <h2 className="text-xs font-semibold">记录的字段变更</h2>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>字段</TableHead>
                        <TableHead>变更前</TableHead>
                        <TableHead>变更后</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {item.changes.map((c, i) => (
                        <TableRow key={`${c.field}:${i}`}>
                          <TableCell>{fields[c.field] || c.field}</TableCell>
                          <TableCell className="font-mono break-all whitespace-pre-wrap">
                            {c.before || "未记录/无值"}
                          </TableCell>
                          <TableCell className="font-mono break-all whitespace-pre-wrap">
                            {c.after || "未记录/无值"}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </section>
              )}
              <p className="rounded-md border p-3 text-2xs leading-6 text-muted-foreground">
                这里展示保存时的证据，名称为当时快照；历史记录可能只有
                ID。当前资源状态可能已变化，记录中的已受理或待核实不代表操作成功。集群停用或资源消失不会删除审计。
              </p>
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
