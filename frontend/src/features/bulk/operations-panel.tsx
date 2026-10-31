import { ClientResponseError } from "pocketbase"
import { useEffect } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import {
  viewSchema,
  label,
  statusLabel,
  errorMessage,
  validOperations,
} from "./schema"
export function BulkOperations({
  clusterId,
  ids,
  resource,
  automatic,
  batchId,
}: {
  clusterId: string
  ids: string[]
  resource: "job" | "node"
  automatic: boolean
  batchId?: string | null
}) {
  const client = useQueryClient(),
    access = useAuth((s) => s.user?.access),
    read = resource === "job" ? "jobs.read" : "nodes.read"
  const allowed =
    !!access &&
    (access.platformPermissions.includes(read) ||
      access.clusters.some(
        (c) => c.cluster.id === clusterId && c.permissions.includes(read)
      ))
  const valid = validOperations(ids)
  const query = useQuery({
    queryKey: ["bulk-operations", clusterId, resource, ids.join(","), batchId],
    enabled: allowed && valid,
    queryFn: async ({ signal }) => {
      const base = `/clusters/${encodeURIComponent(clusterId)}/bulk-control`
      if (batchId && /^[A-Za-z0-9_-]{20,64}$/.test(batchId)) {
        try {
          const group = viewSchema.parse(
            await api(`${base}/${batchId}`, { signal })
          )
          if (
            group.items.length !== ids.length ||
            group.items.some((item, i) => item.operationId !== ids[i]) ||
            !group.action.startsWith(resource === "job" ? "jobs." : "nodes.")
          )
            throw new Error("Batch context does not match operation link")
          if (
            group.status !== "completed" ||
            group.items.every((i) => ["confirmed", "failed"].includes(i.status))
          )
            return group
          const durable = viewSchema.parse(
            await api(
              `${base}/operations?${new URLSearchParams({ resource, ids: ids.join(",") })}`,
              { signal }
            )
          )
          durable.items = durable.items.map((item, index) =>
            item.code === "not_found" && group.items[index]?.status === "failed"
              ? group.items[index]
              : item
          )
          durable.counts = Object.fromEntries(
            [
              "ready",
              "processing",
              "confirmed",
              "accepted",
              "failed",
              "unknown",
            ].map((s) => [
              s,
              durable.items.filter((i) => i.status === s).length,
            ])
          )
          return durable
        } catch (cause) {
          if (!(cause instanceof ClientResponseError) || cause.status !== 404)
            throw cause
        }
      }
      return viewSchema.parse(
        await api(
          `${base}/operations?${new URLSearchParams({ resource, ids: ids.join(",") })}`,
          { signal }
        )
      )
    },
    retry: false,
    refetchOnWindowFocus: false,
    refetchInterval: (q) =>
      !q.state.error &&
      automatic &&
      ((q.state.data?.status === "ready" && q.state.data.allowed) ||
        q.state.data?.items.some(
          (i) =>
            !i.code && ["processing", "accepted", "unknown"].includes(i.status)
        ))
        ? 3000
        : false,
  })
  const data = allowed && !query.isError ? query.data : undefined,
    confirmed =
      data?.items
        .filter((i) => i.status === "confirmed")
        .map((i) => i.operationId)
        .join(",") || ""
  useEffect(() => {
    if (confirmed)
      for (const key of [
        [resource === "job" ? "jobs" : "nodes", clusterId],
        [resource, clusterId],
        ["cluster-overview", clusterId],
      ])
        void client.invalidateQueries({ queryKey: key })
  }, [client, clusterId, resource, confirmed])
  if (!allowed)
    return (
      <p role="alert" className="panel p-4 text-sm">
        没有读取这些操作记录的权限。
      </p>
    )
  if (!valid)
    return (
      <p role="alert" className="panel p-4 text-sm">
        操作记录链接不合法，每次最多查询 10 个不重复的编号。
      </p>
    )
  return (
    <section className="panel space-y-4 p-4" aria-label="批量操作记录">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-semibold">批量操作记录</h2>
        <Button
          variant="outline"
          size="sm"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          只读刷新记录
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        读取当前批量进度与逐项持久审计，不重发控制命令。上下文丢失且没有审计的项目保持待核实。
      </p>
      {query.isPending && <p role="status">正在读取操作记录…</p>}
      {query.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(query.error)}
        </p>
      )}
      {data && (
        <>
          <div className="flex flex-wrap gap-2" aria-label="操作结果汇总">
            {Object.entries(data.counts)
              .filter(([, n]) => n > 0)
              .map(([s, n]) => (
                <Badge variant="outline" key={s}>
                  {statusLabel(s)} {n}
                </Badge>
              ))}
          </div>
          <div className="grid gap-3 lg:grid-cols-2">
            {data.items.map((i) => (
              <article
                key={i.operationId}
                className="space-y-2 rounded-md border p-3 text-sm"
              >
                <p className="flex flex-wrap items-center gap-2">
                  <strong className="font-mono">
                    {i.target || "目标待核实"}
                  </strong>
                  <span>{label(i.action)}</span>
                  <Badge variant="outline">{statusLabel(i.status)}</Badge>
                </p>
                <p className="text-xs">{i.message}</p>
                <p className="font-mono text-xs break-all text-muted-foreground">
                  {i.operationId}
                </p>
              </article>
            ))}
          </div>
        </>
      )}
    </section>
  )
}
