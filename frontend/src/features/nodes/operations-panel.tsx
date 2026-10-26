import { useEffect } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { api } from "@/services/api"
import { timestamp } from "@/features/overview/schema"
import {
  actionLabel,
  operationLabel,
  operationSchema,
  pooled,
  type Operation,
} from "./schema"
export function NodeOperations({
  clusterId,
  ids,
  automatic,
}: {
  clusterId: string
  ids: string[]
  automatic: boolean
}) {
  const client = useQueryClient()
  const query = useQuery({
    queryKey: ["node-operations", clusterId, ids.join(",")],
    queryFn: ({ signal }) =>
      pooled(ids, async (id) => {
        try {
          return {
            id,
            value: operationSchema.parse(
              await api(
                `/clusters/${clusterId}/node-operations/${encodeURIComponent(id)}`,
                { signal }
              )
            ),
          }
        } catch {
          return { id, value: undefined as Operation | undefined }
        }
      }),
    retry: false,
    refetchOnWindowFocus: automatic,
    refetchInterval: (q) =>
      automatic &&
      q.state.data?.some(
        (item) =>
          item.value &&
          ["processing", "accepted", "unknown"].includes(item.value.status)
      )
        ? 3000
        : false,
  })
  const confirmed =
    query.data
      ?.filter((item) => item.value?.status === "confirmed")
      .map((item) => item.id)
      .join(",") || ""
  useEffect(() => {
    if (!confirmed) return
    for (const key of [
      ["nodes", clusterId],
      ["node", clusterId],
      ["cluster-overview", clusterId],
    ])
      void client.invalidateQueries({ queryKey: key })
  }, [client, clusterId, confirmed])
  return (
    <section className="panel space-y-4 p-4" aria-label="节点操作记录">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold">节点操作记录</h2>
        <Button
          size="sm"
          variant="outline"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
          刷新记录
        </Button>
      </div>
      <p className="text-2xs leading-5 text-muted-foreground">
        仅通过读取核实结果，不重发控制命令。批量操作按节点独立执行。
      </p>
      {query.isPending && (
        <p role="status" className="text-xs">
          正在读取操作记录…
        </p>
      )}
      <div className="grid gap-3 lg:grid-cols-2">
        {query.data?.map(({ id, value }) => (
          <article key={id} className="space-y-2 rounded-md border p-3 text-xs">
            {value ? (
              <>
                <div className="flex flex-wrap items-center gap-2">
                  <strong className="font-mono break-all">
                    {value.nodeName}
                  </strong>
                  <span>{actionLabel(value.action)}</span>
                  <Badge
                    className="status-badge"
                    variant="outline"
                    data-tone={
                      value.status === "confirmed"
                        ? "success"
                        : value.status === "failed"
                          ? "danger"
                          : "warning"
                    }
                  >
                    {operationLabel(value.status)}
                  </Badge>
                </div>
                <p className="leading-6">{value.message}</p>
                <div className="flex flex-wrap gap-1">
                  {value.states.map((state) => (
                    <SlurmStatusBadge key={state} state={state} kind="node" />
                  ))}
                </div>
                <p className="text-2xs leading-5 text-muted-foreground">
                  原因：{value.reason} · {timestamp(value.updatedAt)}
                </p>
              </>
            ) : (
              <p
                role="alert"
                className="text-2xs leading-6 text-muted-foreground"
              >
                暂时无法读取此操作记录。请求可能尚未送达、已被拒绝或结果待核实，请核对节点状态，勿重复发送控制请求。
              </p>
            )}
            <p className="font-mono text-2xs break-all text-muted-foreground">
              {id}
            </p>
          </article>
        ))}
      </div>
    </section>
  )
}
