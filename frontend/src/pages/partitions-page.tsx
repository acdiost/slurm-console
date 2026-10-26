import { useCallback, useMemo, useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
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
import {
  ClusterPicker,
  pageNumber,
  useAccessibleClusters,
} from "@/features/clusters/cluster-picker"
import { PartitionFilters } from "@/features/partitions/partition-filters"
import { PartitionDetail } from "@/features/partitions/partition-detail"
import {
  errorMessage,
  partitionPageSchema,
  partitionTime,
  type Partition,
} from "@/features/partitions/schema"
import { timestamp } from "@/features/overview/schema"
import { api } from "@/services/api"
const EMPTY: Partition[] = []
export function PartitionsPage() {
  const [params, setParams] = useSearchParams(),
    [automatic, setAutomatic] = useState(true),
    client = useQueryClient()
  const selection = useAccessibleClusters(),
    clusterId = selection.id
  const page = pageNumber(params.get("page")),
    perPage = [20, 50, 100].includes(Number(params.get("perPage")))
      ? Number(params.get("perPage"))
      : 20
  const filters = { q: params.get("q") || "", state: params.get("state") || "" }
  const sortKey = params.get("sort") || "name",
    order = params.get("order") || "asc",
    name = params.get("partition") || ""
  const list = new URLSearchParams({
    page: String(page),
    perPage: String(perPage),
    sort: sortKey,
    order,
  })
  for (const [key, value] of Object.entries(filters))
    if (value) list.set(key, value)
  const queryString = list.toString()
  const query = useQuery({
    queryKey: ["partitions", clusterId, queryString],
    enabled: !!clusterId,
    queryFn: async ({ signal }) =>
      partitionPageSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/partitions?${queryString}`,
          { signal }
        )
      ),
    retry: false,
    staleTime: 15000,
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
    rows = data?.items ?? EMPTY
  const update = useCallback(
    (changes: Record<string, string | null>) =>
      setParams((previous) => {
        const next = new URLSearchParams(previous)
        for (const [key, value] of Object.entries(changes)) {
          if (value === null || value === "") next.delete(key)
          else next.set(key, value)
        }
        return next
      }),
    [setParams]
  )
  const open = useCallback(
    (name: string) =>
      update({ cluster: clusterId, partition: name, nodesPage: null }),
    [clusterId, update]
  )
  const sort = useCallback(
    (key: string) =>
      update({
        sort: key,
        order: sortKey === key && order === "asc" ? "desc" : "asc",
        page: "1",
      }),
    [sortKey, order, update]
  )
  const columns = useMemo<ColumnDef<Partition>[]>(() => {
    const heading = (key: string, label: string) => (
      <Button
        variant="ghost"
        size="sm"
        className="-ml-2"
        aria-label={`按${label}排序`}
        onClick={() => sort(key)}
      >
        {label}
        {sortKey === key && (order === "asc" ? <ArrowUp /> : <ArrowDown />)}
      </Button>
    )
    return [
      {
        id: "name",
        header: () => heading("name", "分区名称"),
        cell: ({ row: { original: p } }) => (
          <div className="max-w-56 min-w-32">
            <Button
              variant="link"
              size="sm"
              className="h-auto p-0 font-mono text-xs break-all"
              onClick={() => open(p.name)}
            >
              {p.name}
            </Button>
            <p
              className="mt-1 truncate font-mono text-2xs text-muted-foreground"
              title={p.nodes}
            >
              {p.nodes || "未指定节点"}
            </p>
          </div>
        ),
      },
      {
        id: "states",
        header: "状态",
        cell: ({ row: { original: p } }) => (
          <div className="flex min-w-24 flex-wrap gap-1">
            {p.states.map((state) => (
              <SlurmStatusBadge key={state} state={state} kind="partition" />
            ))}
          </div>
        ),
      },
      {
        id: "nodes",
        header: () => heading("nodes", "配置节点"),
        cell: ({ row: { original: p } }) => (
          <span className="font-mono text-xs">{p.nodeCount}</span>
        ),
      },
      {
        id: "cpus",
        header: () => heading("cpus", "配置 CPU"),
        cell: ({ row: { original: p } }) => (
          <span className="font-mono text-xs">{p.cpus}</span>
        ),
      },
      {
        id: "time",
        header: () => heading("maxTime", "最长时限"),
        cell: ({ row: { original: p } }) => (
          <div className="min-w-24 text-2xs">
            <p>{partitionTime(p.maxTimeMinutes)}</p>
            <p className="mt-1 text-muted-foreground">
              默认 {partitionTime(p.defaultTimeMinutes)}
            </p>
          </div>
        ),
      },
    ]
  }, [sortKey, order, sort, open])
  // TanStack Table exposes mutable methods that must not be compiler memoized.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    getRowId: (p) => p.name,
  })
  return (
    <div className="mx-auto max-w-7xl space-y-5 p-6 sm:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="section-eyebrow">PARTITION EXPLORER</p>
          <h1 className="mt-2 text-2xl font-semibold">分区查询</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {selection.current?.name || "查看分区容量、调度约束和关联节点。"}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Select
            value={automatic ? "automatic" : "paused"}
            onValueChange={(value) => setAutomatic(value === "automatic")}
          >
            <SelectTrigger aria-label="分区自动刷新" className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="automatic">每 10 秒刷新</SelectItem>
              <SelectItem value="paused">暂停刷新</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            disabled={query.isFetching || selection.inventory.isFetching}
            onClick={() => {
              if (clusterId) void query.refetch()
              void selection.inventory.refetch()
              if (name)
                void client.invalidateQueries({
                  queryKey: ["partition", clusterId, name],
                })
            }}
          >
            <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
            刷新分区
          </Button>
        </div>
      </header>
      <ClusterPicker selection={selection} label="分区" />
      {selection.inventory.isError && (
        <p role="alert" className="panel p-5 text-xs text-destructive">
          集群列表暂不可用，请刷新重试。
        </p>
      )}
      {clusterId && (
        <PartitionFilters
          key={JSON.stringify(filters)}
          initial={filters}
          onApply={(values) =>
            update({ ...values, page: "1", partition: null, nodesPage: null })
          }
        />
      )}
      {((selection.inventory.isPending && !clusterId) ||
        (query.isPending && !!clusterId)) && (
        <div role="status" aria-label="正在读取分区列表">
          <Skeleton className="h-64" />
        </div>
      )}
      {query.isError && clusterId && (
        <p
          role="alert"
          className="panel p-5 text-xs leading-6 text-destructive"
        >
          {errorMessage(query.error)}
        </p>
      )}
      {!selection.inventory.isPending &&
        !selection.inventory.isError &&
        !clusterId && (
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
              分区列表{" "}
              <Badge variant="secondary" className="ml-2">
                {data.totalItems}
              </Badge>
            </h2>
            <span className="text-2xs text-muted-foreground">
              {timestamp(data.fetchedAt)} ·{" "}
              {data.cached ? "缓存数据" : "已更新"}
              {query.isFetching && " · 正在刷新…"}
            </span>
          </div>
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id}>
                  {group.headers.map((header) => (
                    <TableHead
                      key={header.id}
                      className={
                        header.id === "cpus" ? "hidden lg:table-cell" : ""
                      }
                    >
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
                      className={`py-4 align-top ${cell.column.id === "cpus" ? "hidden lg:table-cell" : ""}`}
                    >
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext()
                      )}
                    </TableCell>
                  ))}
                  <TableCell className="py-4 text-right align-top">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => open(row.id)}
                    >
                      详情
                    </Button>
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
                  ? "本页没有分区"
                  : "没有符合条件的可见分区"}
              </p>
              {page > 1 && (
                <Button
                  size="sm"
                  variant="outline"
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
                <SelectTrigger aria-label="每页分区数" className="w-28">
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
                aria-label="上一页分区"
                disabled={page <= 1 || query.isFetching}
                onClick={() => update({ page: String(page - 1) })}
              >
                <ChevronLeft />
              </Button>
              <Button
                size="icon"
                variant="outline"
                aria-label="下一页分区"
                disabled={page * perPage >= data.totalItems || query.isFetching}
                onClick={() => update({ page: String(page + 1) })}
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </section>
      )}
      <p className="text-2xs leading-6 text-muted-foreground">
        分区可能共享节点，配置容量不能直接相加。资源统计另行检查节点读取权限；分区状态为
        UP 不代表所有关联节点都可运行作业。
      </p>
      {clusterId && name && (
        <PartitionDetail
          key={`${clusterId}:${name}`}
          clusterId={clusterId}
          name={name}
          automatic={automatic}
          nodesPage={pageNumber(params.get("nodesPage"))}
          onNodesPage={(page) => update({ nodesPage: String(page) })}
          onClose={() => update({ partition: null, nodesPage: null })}
        />
      )}
    </div>
  )
}
