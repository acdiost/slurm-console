import { useQuery } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { PauseCircle, Play, RefreshCw } from "lucide-react"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { api } from "@/services/api"
import { timestamp } from "@/features/overview/schema"
import {
  errorMessage,
  jobTime,
  nodeDetailSchema,
  quantity,
  type Action,
} from "./schema"
export function NodeDetail({
  clusterId,
  name,
  automatic,
  canDrain,
  canResume,
  onControl,
  onClose,
}: {
  clusterId: string
  name: string
  automatic: boolean
  canDrain: boolean
  canResume: boolean
  onControl: (action: Action) => void
  onClose: () => void
}) {
  const query = useQuery({
    queryKey: ["node", clusterId, name],
    queryFn: async ({ signal }) =>
      nodeDetailSchema.parse(
        await api(`/clusters/${clusterId}/nodes/${encodeURIComponent(name)}`, {
          signal,
        })
      ),
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: automatic,
    refetchInterval: (q) =>
      automatic &&
      !(
        q.state.error instanceof ClientResponseError &&
        [403, 404].includes(q.state.error.status)
      )
        ? 10000
        : false,
  })
  const data = query.isError ? undefined : query.data,
    n = data?.item
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent className="overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle className="break-all">节点 {name}</SheetTitle>
          <SheetDescription>
            控制器记录的资源、状态、备注和维护信息。
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-6 px-5 pb-6">
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
              刷新详情
            </Button>
            {canDrain && (
              <Button
                size="sm"
                variant="destructive"
                disabled={!n || query.isFetching || n.states.includes("DRAIN")}
                onClick={() => onControl("drain")}
              >
                <PauseCircle />
                Drain 排空
              </Button>
            )}
            {canResume && (
              <Button
                size="sm"
                disabled={
                  !n ||
                  query.isFetching ||
                  (!n.states.includes("DRAIN") && !n.states.includes("DOWN"))
                }
                onClick={() => onControl("resume")}
              >
                <Play />
                Resume 恢复
              </Button>
            )}
          </div>
          {query.isPending && (
            <div role="status" aria-label="正在读取节点详情">
              <Skeleton className="h-64" />
            </div>
          )}
          {query.isError && (
            <p
              role="alert"
              className="rounded-md border p-4 text-xs leading-6 text-destructive"
            >
              {errorMessage(query.error)}
            </p>
          )}
          {n && (
            <>
              <section className="space-y-3 rounded-md border bg-muted/20 p-4">
                <div className="flex flex-wrap gap-1">
                  {n.states.map((state) => (
                    <SlurmStatusBadge key={state} state={state} kind="node" />
                  ))}
                </div>
                <p className="text-xs leading-6 break-all">
                  {n.reason || "没有额外状态原因。"}
                </p>
                <p className="text-2xs text-muted-foreground">
                  原因设置者 {n.reasonUser || "—"} ·{" "}
                  {jobTime(n.reasonChangedAt)}
                </p>
                <p className="text-2xs text-muted-foreground">
                  {timestamp(data!.fetchedAt)} ·{" "}
                  {data!.cached ? "缓存数据" : "已更新"}
                </p>
              </section>
              <section>
                <h2 className="mb-3 text-xs font-semibold">调度资源</h2>
                <div className="grid gap-3 sm:grid-cols-2">
                  {[
                    [
                      "CPU",
                      `${n.resources.cpus.total}`,
                      `${n.resources.cpus.allocated} 已分配 · ${n.resources.cpus.idle} 空闲 · ${n.resources.cpus.unavailable} 不可用/预留`,
                    ],
                    [
                      "内存",
                      `${n.memoryMiB.toLocaleString()} MiB`,
                      `${n.allocatedMemoryMiB.toLocaleString()} MiB 已分配 · ${n.resources.memoryMiB.idle.toLocaleString()} MiB 空闲`,
                    ],
                    [
                      "GPU",
                      n.resources.gpus.total === null
                        ? "未知"
                        : `${n.resources.gpus.total}`,
                      n.resources.gpus.allocated === null
                        ? "分配数量未知"
                        : `${n.resources.gpus.allocated} 已分配`,
                    ],
                    [
                      "OS 报告空闲内存",
                      quantity(n.freeMemoryMiB, " MiB"),
                      "不等同于可调度余量",
                    ],
                  ].map(([label, value, description]) => (
                    <div key={label} className="rounded-md border p-3">
                      <p className="text-2xs text-muted-foreground">{label}</p>
                      <p className="mt-2 font-mono text-lg font-semibold">
                        {value}
                      </p>
                      <p className="mt-2 text-2xs leading-5 text-muted-foreground">
                        {description}
                      </p>
                    </div>
                  ))}
                </div>
                <p className="mt-3 text-2xs leading-6 text-muted-foreground">
                  调度分配量不是运行时利用率；不可用节点的未分配资源不计为空闲。
                </p>
              </section>
              <section className="border-t pt-5">
                <h2 className="mb-3 text-xs font-semibold">身份与环境</h2>
                <dl className="grid grid-cols-2 gap-x-5 gap-y-4">
                  {[
                    ["主机名", n.hostname],
                    ["地址", n.address],
                    ["架构", n.architecture],
                    ["Slurmd 版本", n.version],
                    ["分区", n.partitions.join(", ")],
                    ["有效 CPU", String(n.effectiveCpus)],
                    ["可用特征", n.features.join(", ")],
                    ["活动特征", n.activeFeatures.join(", ")],
                    ["预留", n.reservation],
                    ["GRES", n.gres],
                    ["已分配 GRES", n.gresUsed],
                    ["备注", n.comment],
                  ].map(([label, value]) => (
                    <div key={label}>
                      <dt className="text-2xs text-muted-foreground">
                        {label}
                      </dt>
                      <dd className="mt-1.5 font-mono text-xs break-all">
                        {value || "—"}
                      </dd>
                    </div>
                  ))}
                </dl>
                <p className="mt-5 text-2xs text-muted-foreground">操作系统</p>
                <p className="mt-2 font-mono text-xs leading-6 break-all">
                  {n.operatingSystem || "—"}
                </p>
              </section>
              <section className="border-t pt-5">
                <h2 className="mb-3 text-xs font-semibold">时间与维护</h2>
                <dl className="space-y-3">
                  {[
                    ["系统启动", jobTime(n.bootTime)],
                    ["Slurmd 启动", jobTime(n.slurmdStartTime)],
                    ["自动恢复时间", jobTime(n.resumeAfter)],
                  ].map(([label, value]) => (
                    <div
                      key={label}
                      className="flex flex-wrap justify-between gap-2 text-2xs"
                    >
                      <dt className="text-muted-foreground">{label}</dt>
                      <dd className="font-mono">{value}</dd>
                    </div>
                  ))}
                </dl>
              </section>
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
