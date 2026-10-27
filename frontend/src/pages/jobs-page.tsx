import { useCallback, useMemo, useState } from "react"
import { ClientResponseError } from "pocketbase"
import { Link, useSearchParams } from "react-router"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import {
  ArrowDown,
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  RefreshCw,
  SearchX,
  ShieldAlert,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { timestamp } from "@/features/overview/schema"
import {
  active,
  errorMessage,
  jobsPageSchema,
  jobTime,
  quantity,
  type Job,
  type Operation,
} from "@/features/jobs/schema"
import {
  JobFilters,
  emptyFilters,
  type Filters,
} from "@/features/jobs/job-filters"
import { JobDetail } from "@/features/jobs/job-detail"
import { CancelJobDialog } from "@/features/jobs/cancel-dialog"
import {
  ClusterPicker,
  useAccessibleClusters,
} from "@/features/clusters/cluster-picker"
const EMPTY: Job[] = []
function integer(value: string | null, fallback: number, max: number) {
  const n = Number(value ?? fallback)
  return Number.isInteger(n) && n >= 1 && n <= max ? n : fallback
}
export function JobsPage() {
  const [params, setParams] = useSearchParams()
  const [automatic, setAutomatic] = useState(true)
  const [cancelTarget, setCancelTarget] = useState<{
    clusterId: string
    jobId: string
  }>()
  const client = useQueryClient()
  const access = useAuth((state) => state.user?.access)
  const page = integer(params.get("page"), 1, 1000000),
    perPage = [20, 50, 100].includes(Number(params.get("perPage")))
      ? Number(params.get("perPage"))
      : 20
  const clusterSelection = useAccessibleClusters(),
    inventory = clusterSelection.inventory
  const clusterId = clusterSelection.id,
    jobId = params.get("job") || "",
    operationId = params.get("operation") || ""
  const cancelOpen =
    cancelTarget?.clusterId === clusterId && cancelTarget?.jobId === jobId
  const canCancel =
    !!access &&
    (access.platformPermissions.includes("jobs.cancel") ||
      access.clusters.some(
        (entry) =>
          entry.cluster.id === clusterId &&
          entry.permissions.includes("jobs.cancel")
      ))
  const filters: Filters = { ...emptyFilters }
  for (const key of Object.keys(filters) as (keyof Filters)[])
    filters[key] = params.get(key) || ""
  const filterKey = JSON.stringify(filters)
  const sortKey = params.get("sort") || "id",
    order = params.get("order") || "desc"
  const listQuery = new URLSearchParams({
    page: String(page),
    perPage: String(perPage),
    sort: sortKey,
    order,
  })
  for (const [key, value] of Object.entries(filters))
    if (value) listQuery.set(key, value)
  const queryString = listQuery.toString()
  const jobs = useQuery({
    queryKey: ["jobs", clusterId, queryString],
    enabled: !!clusterId,
    queryFn: async ({ signal }) =>
      jobsPageSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/jobs?${queryString}`,
          { signal }
        )
      ),
    retry: false,
    staleTime: 5000,
    refetchOnWindowFocus: automatic,
    refetchInterval: (query) =>
      automatic &&
      !(
        query.state.error instanceof ClientResponseError &&
        [403, 404].includes(query.state.error.status)
      )
        ? 10000
        : false,
  })
  const data = jobs.isError ? undefined : jobs.data,
    rows = data?.items ?? EMPTY
  const update = useCallback(
    (changes: Record<string, string | null>) => {
      setParams((previous) => {
        const next = new URLSearchParams(previous)
        for (const [key, value] of Object.entries(changes)) {
          if (value === null || value === "") next.delete(key)
          else next.set(key, value)
        }
        return next
      })
    },
    [setParams]
  )
  const openJob = useCallback(
    (id: string) => update({ cluster: clusterId, job: id, operation: null }),
    [clusterId, update]
  )
  const applyFilters = (values: Filters) =>
    update({ ...values, page: "1", job: null, operation: null })
  const sort = useCallback(
    (key: string) =>
      update({
        sort: key,
        order: sortKey === key && order === "desc" ? "asc" : "desc",
        page: "1",
      }),
    [sortKey, order, update]
  )
  const columns = useMemo<ColumnDef<Job>[]>(
    () => [
      {
        id: "id",
        header: () => (
          <Button
            variant="ghost"
            size="sm"
            className="-ml-2"
            onClick={() => sort("id")}
            aria-label="按 Job ID 排序"
          >
            作业
            {sortKey === "id" &&
              (order === "asc" ? <ArrowUp /> : <ArrowDown />)}
          </Button>
        ),
        cell: ({ row: { original: job } }) => (
          <div className="max-w-48 min-w-36">
            <Button
              variant="link"
              size="sm"
              className="h-auto p-0 font-mono text-xs"
              onClick={() => openJob(job.id)}
            >
              #{job.id}
            </Button>
            <p className="mt-1 truncate font-medium" title={job.name}>
              {job.name || "未命名作业"}
            </p>
            <p className="mt-1 text-2xs text-muted-foreground lg:hidden">
              {job.user} · {job.partition || "—"}
            </p>
            {job.arrayJobId.value ||
            job.arrayTaskString ||
            job.hetJobId.value ? (
              <Badge variant="outline" className="mt-1 text-2xs">
                数组/异构
              </Badge>
            ) : null}
          </div>
        ),
      },
      {
        id: "owner",
        header: "用户 / 账户",
        cell: ({ row: { original: job } }) => (
          <div className="min-w-24">
            <p className="font-mono text-2xs">{job.user || "—"}</p>
            <p className="mt-1 text-2xs text-muted-foreground">
              {job.account || "—"}
            </p>
          </div>
        ),
      },
      {
        id: "partition",
        header: "分区 / QOS",
        cell: ({ row: { original: job } }) => (
          <div className="min-w-24 font-mono text-2xs">
            <p>{job.partition || "—"}</p>
            <p className="mt-1 text-muted-foreground">{job.qos || "—"}</p>
          </div>
        ),
      },
      {
        id: "states",
        header: "状态",
        cell: ({ row: { original: job } }) => (
          <div className="max-w-60 min-w-32">
            <div className="flex flex-wrap gap-1">
              {job.states.slice(0, 3).map((state) => (
                <SlurmStatusBadge key={state} state={state} />
              ))}
            </div>
            {job.states.length > 3 && (
              <p className="mt-1 text-2xs text-muted-foreground">
                另有 {job.states.length - 3} 个标记
              </p>
            )}
            {job.reason && (
              <p className="mt-2 max-w-48 text-2xs leading-5 break-words text-muted-foreground">
                {job.reason}
              </p>
            )}
          </div>
        ),
      },
      {
        id: "cpus",
        header: () => (
          <Button
            variant="ghost"
            size="sm"
            className="-ml-2"
            onClick={() => sort("cpus")}
            aria-label="按 CPU 排序"
          >
            资源
            {sortKey === "cpus" &&
              (order === "asc" ? <ArrowUp /> : <ArrowDown />)}
          </Button>
        ),
        cell: ({ row: { original: job } }) => (
          <div className="min-w-24 font-mono text-2xs">
            <p>{quantity(job.cpus)} CPU</p>
            <p className="mt-1 text-muted-foreground">
              {quantity(job.nodeCount)} 节点
            </p>
          </div>
        ),
      },
      {
        id: "submitTime",
        header: () => (
          <Button
            variant="ghost"
            size="sm"
            className="-ml-2"
            onClick={() => sort("submitTime")}
            aria-label="按提交时间排序"
          >
            提交时间
            {sortKey === "submitTime" &&
              (order === "asc" ? <ArrowUp /> : <ArrowDown />)}
          </Button>
        ),
        cell: ({ row: { original: job } }) => (
          <p className="min-w-36 text-2xs text-muted-foreground">
            {jobTime(job.submitTime)}
          </p>
        ),
      },
    ],
    [sortKey, order, openJob, sort]
  )
  // TanStack Table exposes mutable methods that must not be compiler memoized.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    getRowId: (row) => row.id,
  })
  const afterMutation = (operation: Operation) => {
    update({ operation: operation.operationId })
    for (const key of [
      ["jobs", clusterId],
      ["job", clusterId],
      ["cluster-overview", clusterId],
      ["job-operation", clusterId, operation.operationId],
    ])
      void client.invalidateQueries({ queryKey: key })
  }
  const currentCluster = clusterSelection.current
  const hidden = (column: string) =>
    ["owner", "partition", "cpus", "submitTime"].includes(column)
      ? "hidden lg:table-cell"
      : ""
  return (
    <div className="mx-auto max-w-7xl space-y-5 p-6 sm:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="section-eyebrow">JOB OPERATIONS</p>
          <h1 className="mt-2 text-2xl font-semibold">作业管理</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {currentCluster?.name || "按集群查看可见作业、资源申请和状态原因。"}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Select
            value={automatic ? "automatic" : "paused"}
            onValueChange={(value) => setAutomatic(value === "automatic")}
          >
            <SelectTrigger aria-label="作业自动刷新" className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="automatic">每 10 秒刷新</SelectItem>
              <SelectItem value="paused">暂停刷新</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            disabled={jobs.isFetching || inventory.isFetching}
            onClick={() => {
              if (clusterId) void jobs.refetch()
              void inventory.refetch()
              if (jobId)
                void client.invalidateQueries({
                  queryKey: ["job", clusterId, jobId],
                })
            }}
          >
            <RefreshCw className={jobs.isFetching ? "animate-spin" : ""} />
            刷新作业
          </Button>
        </div>
      </header>
      <ClusterPicker
        selection={clusterSelection}
        label="作业"
        onSwitch={() => setCancelTarget(undefined)}
      />
      {inventory.isError && (
        <div role="alert" className="panel p-5 text-xs text-destructive">
          集群列表暂不可用，请刷新重试。
        </div>
      )}
      {!!clusterId && (
        <JobFilters key={filterKey} initial={filters} onApply={applyFilters} />
      )}
      {((inventory.isPending && !clusterId) ||
        (jobs.isPending && !!clusterId)) && (
        <div role="status" aria-label="正在读取作业列表" className="space-y-3">
          <Skeleton className="h-14" />
          <Skeleton className="h-64" />
        </div>
      )}
      {jobs.isError && clusterId && (
        <div
          role="alert"
          className="panel p-5 text-xs leading-6 text-destructive"
        >
          {errorMessage(jobs.error)}
        </div>
      )}
      {!inventory.isPending && !inventory.isError && !clusterId && (
        <div className="panel space-y-3 border-dashed p-12 text-center">
          <SearchX className="mx-auto size-7 text-muted-foreground" />
          <h2 className="text-base font-semibold">本页没有可访问的集群</h2>
          <p className="text-xs text-muted-foreground">
            选择其他集群页，或联系管理员授予读取权限。
          </p>
          <Button asChild variant="outline">
            <Link to="/access">查看我的权限</Link>
          </Button>
        </div>
      )}
      {data && (
        <section className="panel overflow-hidden">
          <div className="panel-heading">
            <h2>
              作业列表{" "}
              <Badge variant="secondary" className="ml-2">
                {data.totalItems}
              </Badge>
            </h2>
            <span className="text-2xs text-muted-foreground">
              {timestamp(data.fetchedAt)} ·{" "}
              {data.cached ? "缓存数据" : "已更新"}
              {jobs.isFetching && " · 正在刷新…"}
            </span>
          </div>
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id}>
                  {group.headers.map((header) => (
                    <TableHead key={header.id} className={hidden(header.id)}>
                      {flexRender(
                        header.column.columnDef.header,
                        header.getContext()
                      )}
                    </TableHead>
                  ))}
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows.map((row) => (
                <TableRow key={row.id}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className={`py-4 align-top ${hidden(cell.column.id)}`}
                    >
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext()
                      )}
                    </TableCell>
                  ))}
                  <TableCell className="py-4 align-top">
                    <div className="flex justify-end gap-1">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => openJob(row.id)}
                      >
                        详情
                      </Button>
                      {canCancel && (
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={`取消作业 #${row.id}`}
                          disabled={!active(row.original)}
                          onClick={() => {
                            openJob(row.id)
                            setCancelTarget({ clusterId, jobId: row.id })
                          }}
                        >
                          <ShieldAlert className="text-destructive" />
                        </Button>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!rows.length && (
            <div className="space-y-3 p-12 text-center">
              <SearchX className="mx-auto size-7 text-muted-foreground" />
              <p className="text-sm font-medium">
                {data.totalItems > 0
                  ? "本页没有作业"
                  : "没有符合条件的可见作业"}
              </p>
              <p className="text-2xs text-muted-foreground">
                可调整筛选、返回第一页或刷新后重试。
              </p>
              {page > 1 && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => update({ page: "1" })}
                >
                  返回第一页
                </Button>
              )}
            </div>
          )}
          <div className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3">
            <span className="text-2xs text-muted-foreground">
              共 {data.totalItems} 条 · 第 {page} 页
            </span>
            <div className="flex items-center gap-2">
              <Select
                value={String(perPage)}
                onValueChange={(value) => update({ perPage: value, page: "1" })}
              >
                <SelectTrigger aria-label="每页作业数" className="w-28">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {[20, 50, 100].map((n) => (
                    <SelectItem key={n} value={String(n)}>
                      {n} 条 / 页
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                size="icon"
                variant="outline"
                aria-label="上一页作业"
                disabled={page <= 1 || jobs.isFetching}
                onClick={() => update({ page: String(page - 1) })}
              >
                <ChevronLeft />
              </Button>
              <Button
                size="icon"
                variant="outline"
                aria-label="下一页作业"
                disabled={page * perPage >= data.totalItems || jobs.isFetching}
                onClick={() => update({ page: String(page + 1) })}
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </section>
      )}
      <p className="text-2xs leading-6 text-muted-foreground">
        作业数据来自当前控制器，已结束记录可能随保留策略移除。取消需要集群动作权限、显式
        Slurm 身份关联和最新确认；Slurm 服务端继续校验实际操作身份。
      </p>
      {clusterId && jobId && /^\d{1,10}$/.test(jobId) && !cancelOpen && (
        <JobDetail
          key={`${clusterId}:${jobId}`}
          clusterId={clusterId}
          jobId={jobId}
          operationId={operationId}
          automatic={automatic}
          canCancel={canCancel}
          onCancel={() => setCancelTarget({ clusterId, jobId })}
          onClose={() => {
            if (
              !cancelTarget ||
              cancelTarget.clusterId !== clusterId ||
              cancelTarget.jobId !== jobId
            )
              update({ job: null, operation: null })
          }}
        />
      )}
      {clusterId &&
        cancelTarget?.clusterId === clusterId &&
        cancelTarget.jobId === jobId && (
          <CancelJobDialog
            key={`${clusterId}:${cancelTarget.jobId}`}
            clusterId={clusterId}
            jobId={cancelTarget.jobId}
            onClose={() => setCancelTarget(undefined)}
            onStart={(id) => update({ operation: id })}
            onResult={afterMutation}
          />
        )}
    </div>
  )
}
