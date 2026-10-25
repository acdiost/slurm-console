import { useQuery } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { RefreshCw, ShieldAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { api } from "@/services/api"
import { timestamp, timeLimit } from "@/features/overview/schema"
import {
  active,
  errorMessage,
  jobDetailSchema,
  jobTime,
  operationLabel,
  operationSchema,
  quantity,
} from "./schema"

export function JobDetail({
  clusterId,
  jobId,
  operationId,
  automatic,
  canCancel,
  onCancel,
  onClose,
}: {
  clusterId: string
  jobId: string
  operationId: string
  automatic: boolean
  canCancel: boolean
  onCancel: () => void
  onClose: () => void
}) {
  const query = useQuery({
    queryKey: ["job", clusterId, jobId],
    queryFn: async ({ signal }) =>
      jobDetailSchema.parse(
        await api(`/clusters/${clusterId}/jobs/${jobId}`, { signal })
      ),
    retry: false,
    staleTime: 0,
    refetchInterval: (query) =>
      automatic &&
      !(
        query.state.error instanceof ClientResponseError &&
        [403, 404].includes(query.state.error.status)
      )
        ? 10000
        : false,
    refetchOnWindowFocus: automatic,
  })
  const operation = useQuery({
    queryKey: ["job-operation", clusterId, operationId],
    enabled: !!operationId,
    queryFn: async ({ signal }) =>
      operationSchema.parse(
        await api(
          `/clusters/${clusterId}/job-operations/${encodeURIComponent(operationId)}`,
          { signal }
        )
      ),
    retry: false,
    refetchInterval: (q) =>
      automatic &&
      !q.state.error &&
      (q.state.data?.status === "accepted" ||
        q.state.data?.status === "unknown")
        ? 10000
        : false,
  })
  const data = query.isError ? undefined : query.data
  const job = data?.item
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent className="overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>作业 #{jobId}</SheetTitle>
          <SheetDescription>
            当前控制器可见的调度信息与资源申请。
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-6 px-5 pb-6">
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={query.isFetching}
              onClick={() => {
                void query.refetch()
                if (operationId) void operation.refetch()
              }}
            >
              <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
              刷新详情
            </Button>
            {canCancel && job && (
              <Button
                size="sm"
                variant="destructive"
                disabled={!active(job) || query.isFetching}
                onClick={onCancel}
              >
                <ShieldAlert />
                取消作业
              </Button>
            )}
          </div>
          {query.isPending && (
            <div role="status" aria-label="正在读取作业详情">
              <Skeleton className="h-24" />
              <Skeleton className="mt-4 h-64" />
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
          {job && (
            <>
              <section className="space-y-3 rounded-md border bg-muted/20 p-4">
                <h2 className="text-base font-semibold break-all">
                  {job.name || "未命名作业"}
                </h2>
                <div className="flex flex-wrap gap-1">
                  {job.states.map((state) => (
                    <SlurmStatusBadge key={state} state={state} />
                  ))}
                </div>
                <p className="text-2xs leading-6 text-muted-foreground">
                  {job.reason || "没有额外状态原因。"}
                </p>
                <p className="text-2xs text-muted-foreground">
                  {timestamp(data.fetchedAt)} ·{" "}
                  {data.cached ? "缓存数据" : "已更新"}
                </p>
              </section>
              <section>
                <h2 className="mb-3 text-xs font-semibold">身份与调度</h2>
                <dl className="grid grid-cols-2 gap-x-5 gap-y-4">
                  {[
                    ["用户", job.user || "—"],
                    ["UID", job.userId ?? "—"],
                    ["账户", job.account || "—"],
                    ["分区", job.partition || "—"],
                    ["QOS", job.qos || "—"],
                    ["优先级", quantity(job.priority)],
                  ].map(([label, value]) => (
                    <div key={label}>
                      <dt className="text-2xs text-muted-foreground">
                        {label}
                      </dt>
                      <dd className="mt-1.5 font-mono text-xs break-all">
                        {value}
                      </dd>
                    </div>
                  ))}
                </dl>
              </section>
              <section className="border-t pt-5">
                <h2 className="mb-3 text-xs font-semibold">资源与时限</h2>
                <dl className="grid grid-cols-2 gap-x-5 gap-y-4">
                  {[
                    ["CPU", quantity(job.cpus)],
                    ["节点数", quantity(job.nodeCount)],
                    ["每节点内存", quantity(job.memoryPerNodeMiB, " MiB")],
                    ["每 CPU 内存", quantity(job.memoryPerCpuMiB, " MiB")],
                    ["运行时限", timeLimit(job.timeLimitMinutes)],
                    ["重启次数", job.restartCount ?? "—"],
                  ].map(([label, value]) => (
                    <div key={label}>
                      <dt className="text-2xs text-muted-foreground">
                        {label}
                      </dt>
                      <dd className="mt-1.5 font-mono text-xs break-all">
                        {value}
                      </dd>
                    </div>
                  ))}
                </dl>
                <p className="mt-5 text-2xs text-muted-foreground">
                  节点表达式
                </p>
                <p className="mt-1.5 font-mono text-xs break-all">
                  {job.nodes || "尚未分配节点"}
                </p>
              </section>
              <section className="border-t pt-5">
                <h2 className="mb-3 text-xs font-semibold">时间记录</h2>
                <dl className="space-y-3">
                  {[
                    ["提交时间", job.submitTime],
                    ["开始/预计开始", job.startTime],
                    ["结束/预计结束", job.endTime],
                  ].map(([label, value]) => (
                    <div
                      key={String(label)}
                      className="flex flex-wrap justify-between gap-2 text-2xs"
                    >
                      <dt className="text-muted-foreground">{String(label)}</dt>
                      <dd className="font-mono">
                        {jobTime(value as typeof job.submitTime)}
                      </dd>
                    </div>
                  ))}
                </dl>
              </section>
              {job.arrayJobId.value ||
              job.arrayTaskString ||
              job.hetJobId.value ? (
                <p className="rounded-md border p-3 text-2xs leading-6 text-muted-foreground">
                  数组/异构作业仅提供查询，首版不提供整组或子任务取消。
                </p>
              ) : null}
            </>
          )}
          {operationId && (
            <section className="space-y-3 rounded-md border p-4">
              <h2 className="text-xs font-semibold">取消操作记录</h2>
              <p className="font-mono text-2xs break-all text-muted-foreground">
                {operationId}
              </p>
              {operation.data?.jobId === jobId && !operation.isError ? (
                <>
                  <Badge
                    variant="outline"
                    className="status-badge"
                    data-tone={
                      operation.data.status === "confirmed"
                        ? "success"
                        : operation.data.status === "failed"
                          ? "danger"
                          : "warning"
                    }
                  >
                    {operationLabel(operation.data.status)}
                  </Badge>
                  <p className="text-xs leading-6">{operation.data.message}</p>
                  <p className="text-2xs leading-6 text-muted-foreground">
                    原因：{operation.data.reason}
                  </p>
                </>
              ) : (
                <p className="text-2xs leading-6 text-muted-foreground">
                  {operation.isPending
                    ? "正在读取操作记录…"
                    : "暂时无法取得操作记录，请核对最新作业状态，勿重复取消。"}
                </p>
              )}
            </section>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
