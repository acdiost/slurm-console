import { useCallback, useMemo, useState } from "react"
import { useSearchParams, Link } from "react-router"
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
  Download,
  RefreshCw,
  ShieldCheck,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from "@/components/ui/table"
import { api } from "@/services/api"
import { AuditFilters, type Filters } from "@/features/audits/audit-filters"
import { AuditDetail } from "@/features/audits/audit-detail"
import { AuditStatus } from "@/features/audits/audit-status"
import {
  actions,
  auditPageSchema,
  dateTime,
  errorMessage,
  type Audit,
} from "@/features/audits/schema"
import { pageNumber } from "@/features/clusters/cluster-picker"
const EMPTY: Audit[] = []
export function AuditsPage() {
  const [params, setParams] = useSearchParams(),
    [automatic, setAutomatic] = useState(false),
    client = useQueryClient()
  const page = pageNumber(params.get("page")),
    perPage = [20, 50, 100].includes(Number(params.get("perPage")))
      ? Number(params.get("perPage"))
      : 20
  const filters: Filters = {
    q: params.get("q") || "",
    cluster: params.get("cluster") || "",
    actor: params.get("actor") || "",
    action: params.get("action") || "",
    status: params.get("status") || "",
    from: params.get("from") || "",
    to: params.get("to") || "",
  }
  const sort = params.get("sort") || "created",
    order = params.get("order") || "desc",
    selected = params.get("operation") || ""
  const list = new URLSearchParams({
    page: String(page),
    perPage: String(perPage),
    sort,
    order,
  })
  for (const [key, value] of Object.entries(filters))
    if (value) list.set(key, value)
  const queryString = list.toString()
  const query = useQuery({
    queryKey: ["operation-audits", queryString],
    queryFn: async ({ signal }) =>
      auditPageSchema.parse(
        await api(`/operation-audits?${queryString}`, { signal })
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
        ? 15000
        : false,
  })
  const data = query.isError ? undefined : query.data
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
  const sortBy = useCallback(
    (key: string) =>
      update({
        sort: key,
        order: sort === key && order === "desc" ? "asc" : "desc",
        page: "1",
      }),
    [sort, order, update]
  )
  const columns = useMemo<ColumnDef<Audit>[]>(
    () => [
      {
        id: "created",
        header: () => (
          <Button
            size="sm"
            variant="ghost"
            className="-ml-2"
            aria-label="按创建时间排序"
            onClick={() => sortBy("created")}
          >
            创建时间
            {sort === "created" &&
              (order === "desc" ? <ArrowDown /> : <ArrowUp />)}
          </Button>
        ),
        cell: ({ row: { original: a } }) => (
          <div className="min-w-36">
            <p className="font-mono text-2xs">{dateTime(a.createdAt)}</p>
            <p
              className="mt-1 font-mono text-micro text-muted-foreground"
              title={a.operationId}
            >
              {a.operationId.slice(0, 12)}…
            </p>
          </div>
        ),
      },
      {
        id: "action",
        header: "动作与目标",
        cell: ({ row: { original: a } }) => (
          <div className="max-w-52 min-w-32">
            <p className="text-xs font-medium">
              {actions[a.action] || a.action}
            </p>
            <p
              className="mt-1 truncate font-mono text-2xs text-muted-foreground"
              title={a.targetId}
            >
              {a.targetId || "未记录目标"}
            </p>
          </div>
        ),
      },
      {
        id: "actor",
        header: "操作人",
        cell: ({ row: { original: a } }) => (
          <div className="max-w-44">
            <p className="truncate text-xs" title={a.actorLabel || a.actorId}>
              {a.actorLabel || a.actorId}
            </p>
            <p className="mt-1 text-2xs text-muted-foreground">
              {a.actorSource === "local"
                ? "本机命令"
                : a.actorSource === "legacy"
                  ? "历史记录"
                  : "平台用户"}
            </p>
          </div>
        ),
      },
      {
        id: "cluster",
        header: "集群范围",
        cell: ({ row: { original: a } }) => (
          <span className="block max-w-44 truncate text-xs" title={a.clusterId}>
            {a.clusterName || a.clusterId || "平台范围"}
          </span>
        ),
      },
      {
        id: "status",
        header: "记录结果",
        cell: ({ row: { original: a } }) => <AuditStatus status={a.status} />,
      },
      {
        id: "detail",
        header: "操作",
        cell: ({ row: { original: a } }) => (
          <Button
            variant="outline"
            size="sm"
            aria-label={`查看审计 ${a.operationId}`}
            onClick={() => update({ operation: a.operationId })}
          >
            详情
          </Button>
        ),
      },
    ],
    [sort, order, sortBy, update]
  )
  // TanStack Table exposes mutable methods that must not be compiler memoized.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: data?.items ?? EMPTY,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
  })
  const apply = (value: Filters) =>
    update({ ...value, page: "1", operation: null })
  function download() {
    if (!data) return
    const blob = new Blob(
      [
        JSON.stringify(
          {
            schemaVersion: 1,
            exportedAt: new Date().toISOString(),
            scope: "currentPage",
            query: Object.fromEntries(list),
            ...data,
          },
          null,
          2
        ),
      ],
      { type: "application/json" }
    )
    const url = URL.createObjectURL(blob),
      anchor = document.createElement("a")
    anchor.href = url
    anchor.download = `slurm-audits-page-${page}.json`
    anchor.click()
    URL.revokeObjectURL(url)
  }
  return (
    <div className="mx-auto max-w-screen-2xl space-y-6 p-6 sm:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="section-eyebrow">OPERATION AUDIT</p>
          <h1 className="mt-2 text-2xl font-semibold">操作审计</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            检索平台保存的管理操作、请求编号与结果证据。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Select
            value={automatic ? "15" : "off"}
            onValueChange={(value) => setAutomatic(value === "15")}
          >
            <SelectTrigger aria-label="审计自动刷新" className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="off">暂停自动刷新</SelectItem>
              <SelectItem value="15">每 15 秒刷新</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            disabled={query.isFetching}
            onClick={() => {
              void query.refetch()
              if (selected)
                void client.invalidateQueries({
                  queryKey: ["operation-audit", selected],
                })
            }}
          >
            <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
            刷新审计
          </Button>
        </div>
      </header>
      <div className="flex gap-3 rounded-md border bg-primary/5 p-4 text-xs leading-6 text-muted-foreground">
        <ShieldCheck className="mt-0.5 size-4 shrink-0" />
        <p>
          查询范围由平台审计权限控制，包含停用集群的历史记录。刷新只读取已持久化证据，不核实
          Slurm 当前状态；处理中、中断及未知结果统一谨慎展示。
        </p>
      </div>
      <AuditFilters
        key={JSON.stringify(filters)}
        initial={filters}
        onApply={apply}
      />
      {query.isPending && (
        <div role="status" aria-label="正在读取审计">
          <Skeleton className="h-72" />
        </div>
      )}
      {query.isError && (
        <div role="alert" className="panel space-y-3 p-5 text-xs">
          <p className="text-destructive">{errorMessage(query.error)}</p>
          <Button asChild size="sm" variant="outline">
            <Link to="/access">查看我的权限</Link>
          </Button>
        </div>
      )}
      {data && (
        <section className="panel overflow-hidden" aria-label="审计列表">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b p-4">
            <div className="flex items-center gap-2">
              <h2 className="text-sm font-semibold">审计记录</h2>
              <Badge variant="secondary">{data.totalItems}</Badge>
            </div>
            <div className="flex flex-wrap items-center gap-3">
              <span className="text-2xs text-muted-foreground">
                查询时间 {dateTime(data.fetchedAt)}
              </span>
              <Button
                size="sm"
                variant="outline"
                disabled={!data.items.length || query.isFetching}
                onClick={download}
              >
                <Download />
                导出当前页 JSON
              </Button>
            </div>
          </div>
          {data.items.length ? (
            <Table>
              <TableHeader>
                {table.getHeaderGroups().map((group) => (
                  <TableRow key={group.id}>
                    {group.headers.map((header) => (
                      <TableHead key={header.id}>
                        {flexRender(
                          header.column.columnDef.header,
                          header.getContext()
                        )}
                      </TableHead>
                    ))}
                  </TableRow>
                ))}
              </TableHeader>
              <TableBody>
                {table.getRowModel().rows.map((row) => (
                  <TableRow key={row.original.operationId}>
                    {row.getVisibleCells().map((cell) => (
                      <TableCell className="py-4" key={cell.id}>
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext()
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="space-y-3 px-5 py-12 text-center">
              <h3 className="text-sm font-medium">
                {data.totalItems > 0
                  ? "本页没有审计记录"
                  : "没有符合条件的审计记录"}
              </h3>
              <p className="text-xs text-muted-foreground">
                {data.totalItems > 0
                  ? "记录数量可能已变化，请返回第一页。"
                  : "调整筛选条件，或在管理操作完成后刷新。历史日志不会被回填为审计证据。"}
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
                onValueChange={(value) =>
                  update({ perPage: value, page: "1", operation: null })
                }
              >
                <SelectTrigger aria-label="每页审计数" className="w-32">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {[20, 50, 100].map((value) => (
                    <SelectItem key={value} value={String(value)}>
                      {value} 条 / 页
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                size="icon"
                variant="outline"
                aria-label="上一页审计"
                disabled={page <= 1 || query.isFetching}
                onClick={() =>
                  update({ page: String(page - 1), operation: null })
                }
              >
                <ChevronLeft />
              </Button>
              <Button
                size="icon"
                variant="outline"
                aria-label="下一页审计"
                disabled={page * perPage >= data.totalItems || query.isFetching}
                onClick={() =>
                  update({ page: String(page + 1), operation: null })
                }
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </section>
      )}
      <p className="text-2xs leading-6 text-muted-foreground">
        导出仅包含当前页及筛选条件，不等同于完整备份。记录保存在应用数据库；独立备份与留存由管理员配置。
      </p>
      {selected && !query.isError && (
        <AuditDetail
          key={selected}
          id={selected}
          onClose={() => update({ operation: null })}
          onFilter={(key, value) =>
            update({ [key]: value, page: "1", operation: null })
          }
        />
      )}
    </div>
  )
}
