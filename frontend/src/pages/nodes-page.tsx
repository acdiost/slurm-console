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
  Grid2X2,
  List,
  PauseCircle,
  Play,
  RefreshCw,
  SearchX,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
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
  errorMessage,
  nodeNameValid,
  nodePageSchema,
  type Node,
  type Action,
} from "@/features/nodes/schema"
import { NodeFilters, type Filters } from "@/features/nodes/node-filters"
import { NodeDetail } from "@/features/nodes/node-detail"
import { NodeControlDialog } from "@/features/nodes/control-dialog"
import { NodeOperations } from "@/features/nodes/operations-panel"
import {
  ClusterPicker,
  useAccessibleClusters,
} from "@/features/clusters/cluster-picker"
const EMPTY: Node[] = []
function integer(value: string | null, fallback: number, max = 1000000) {
  const n = Number(value ?? fallback)
  return Number.isInteger(n) && n >= 1 && n <= max ? n : fallback
}
type Target = {
  clusterId: string
  scope: string
  names: string[]
  action: Action
  detail?: string
}
export function NodesPage() {
  const [params, setParams] = useSearchParams()
  const [automatic, setAutomatic] = useState(true)
  const [selection, setSelection] = useState<{
    scope: string
    names: string[]
  }>({ scope: "", names: [] })
  const [target, setTarget] = useState<Target>()
  const client = useQueryClient(),
    access = useAuth((state) => state.user?.access)
  const page = integer(params.get("page"), 1),
    perPage = [20, 50, 100].includes(Number(params.get("perPage")))
      ? Number(params.get("perPage"))
      : 20
  const clusterSelection = useAccessibleClusters(),
    inventory = clusterSelection.inventory
  const clusterId = clusterSelection.id,
    nodeName = params.get("node") || ""
  const allows = (action: Action) =>
    !!access &&
    (access.platformPermissions.includes(`nodes.${action}`) ||
      access.clusters.some(
        (item) =>
          item.cluster.id === clusterId &&
          item.permissions.includes(`nodes.${action}`)
      ))
  const canDrain = allows("drain"),
    canResume = allows("resume"),
    canControl = canDrain || canResume
  const filters: Filters = {
    q: params.get("q") || "",
    partition: params.get("partition") || "",
    state: params.get("state") || "",
  }
  const sortKey = params.get("sort") || "name",
    order = params.get("order") || "asc",
    matrix = params.get("view") === "matrix"
  const list = new URLSearchParams({
    page: String(page),
    perPage: String(perPage),
    sort: sortKey,
    order,
  })
  for (const [key, value] of Object.entries(filters))
    if (value) list.set(key, value)
  const queryString = list.toString(),
    scope = `${clusterId}:${queryString}`
  const nodes = useQuery({
    queryKey: ["nodes", clusterId, queryString],
    enabled: !!clusterId,
    queryFn: async ({ signal }) =>
      nodePageSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/nodes?${queryString}`,
          { signal }
        )
      ),
    retry: false,
    staleTime: 10000,
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
  const data = nodes.isError ? undefined : nodes.data,
    rows = data?.items ?? EMPTY
  const names =
    selection.scope === scope
      ? selection.names.filter((name) => rows.some((row) => row.name === name))
      : []
  const activeTarget =
    target?.clusterId === clusterId &&
    target.scope === scope &&
    (!target.detail || target.detail === nodeName)
      ? target
      : undefined
  const operationIds = (params.get("operations") || "")
    .split(",")
    .filter(Boolean)
  const validOperations =
    operationIds.length <= 10 &&
    operationIds.every((id) => /^[A-Za-z0-9_-]{1,64}$/.test(id))
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
  const openNode = useCallback(
    (name: string) => update({ cluster: clusterId, node: name }),
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
  const columns = useMemo<ColumnDef<Node>[]>(
    () => [
      {
        id: "name",
        header: () => (
          <Button
            variant="ghost"
            size="sm"
            className="-ml-2"
            aria-label="按节点名称排序"
            onClick={() => sort("name")}
          >
            节点
            {sortKey === "name" &&
              (order === "asc" ? <ArrowUp /> : <ArrowDown />)}
          </Button>
        ),
        cell: ({ row: { original: n } }) => (
          <div className="max-w-48 min-w-28">
            <Button
              variant="link"
              size="sm"
              className="h-auto p-0 font-mono text-xs break-all"
              onClick={() => openNode(n.name)}
            >
              {n.name}
            </Button>
            <p className="mt-1 text-2xs break-all text-muted-foreground">
              {n.hostname || "—"}
            </p>
            <p className="mt-1 text-2xs text-muted-foreground">
              {n.partitions.join(", ") || "无分区"}
            </p>
          </div>
        ),
      },
      {
        id: "states",
        header: "状态 / 原因",
        cell: ({ row: { original: n } }) => (
          <div className="max-w-64 min-w-32">
            <div className="flex flex-wrap gap-1">
              {n.states.map((state) => (
                <SlurmStatusBadge key={state} state={state} kind="node" />
              ))}
            </div>
            <p className="mt-2 text-2xs leading-5 break-all text-muted-foreground">
              {n.reason || "—"}
            </p>
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
            aria-label="按节点 CPU 排序"
            onClick={() => sort("cpus")}
          >
            CPU
            {sortKey === "cpus" &&
              (order === "asc" ? <ArrowUp /> : <ArrowDown />)}
          </Button>
        ),
        cell: ({ row: { original: n } }) => (
          <div className="min-w-24 font-mono text-2xs">
            <p>
              {n.allocatedCpus} / {n.cpus} 已分配
            </p>
            <p className="mt-1 text-muted-foreground">
              {n.resources.cpus.idle} 空闲
            </p>
            <p className="mt-1 text-muted-foreground">
              {n.resources.cpus.unavailable} 不可用/预留
            </p>
          </div>
        ),
      },
      {
        id: "memory",
        header: () => (
          <Button
            variant="ghost"
            size="sm"
            className="-ml-2"
            aria-label="按节点内存排序"
            onClick={() => sort("memory")}
          >
            内存 MiB
            {sortKey === "memory" &&
              (order === "asc" ? <ArrowUp /> : <ArrowDown />)}
          </Button>
        ),
        cell: ({ row: { original: n } }) => (
          <div className="min-w-28 font-mono text-2xs">
            <p>
              {n.allocatedMemoryMiB.toLocaleString()} /{" "}
              {n.memoryMiB.toLocaleString()}
            </p>
            <p className="mt-1 text-muted-foreground">
              {n.resources.memoryMiB.idle.toLocaleString()} 空闲
            </p>
          </div>
        ),
      },
      {
        id: "gpu",
        header: "GPU",
        cell: ({ row: { original: n } }) => (
          <div className="min-w-20 font-mono text-2xs">
            <p>
              {n.resources.gpus.allocated ?? "?"} /{" "}
              {n.resources.gpus.total ?? "?"}
            </p>
            <p className="mt-1 max-w-36 break-all text-muted-foreground">
              {n.gres || "无 GRES"}
            </p>
          </div>
        ),
      },
    ],
    [sort, sortKey, order, openNode]
  )
  // TanStack Table exposes mutable methods that must not be compiler memoized.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    getRowId: (row) => row.name,
  })
  const currentCluster = clusterSelection.current
  const toggle = (name: string, checked: boolean) =>
    setSelection({
      scope,
      names: checked
        ? [...names, name].slice(0, 10)
        : names.filter((item) => item !== name),
    })
  const selectNode = (n: Node) =>
    canControl ? (
      <Checkbox
        aria-label={`选择节点 ${n.name}`}
        checked={names.includes(n.name)}
        disabled={names.length >= 10 && !names.includes(n.name)}
        onCheckedChange={(checked) => toggle(n.name, checked === true)}
      />
    ) : null
  const controls = (n: Node) => (
    <div className="flex justify-end gap-1">
      <Button size="sm" variant="outline" onClick={() => openNode(n.name)}>
        详情
      </Button>
      {canDrain && (
        <Button
          size="icon"
          variant="ghost"
          aria-label={`Drain 节点 ${n.name}`}
          disabled={n.states.includes("DRAIN")}
          onClick={() =>
            setTarget({ clusterId, scope, names: [n.name], action: "drain" })
          }
        >
          <PauseCircle className="text-warning" />
        </Button>
      )}
      {canResume && (
        <Button
          size="icon"
          variant="ghost"
          aria-label={`Resume 节点 ${n.name}`}
          disabled={!n.states.includes("DRAIN") && !n.states.includes("DOWN")}
          onClick={() =>
            setTarget({ clusterId, scope, names: [n.name], action: "resume" })
          }
        >
          <Play className="text-primary" />
        </Button>
      )}
    </div>
  )
  const afterControl = () => {
    for (const key of [
      ["nodes", clusterId],
      ["node", clusterId],
      ["cluster-overview", clusterId],
      ["node-operations", clusterId],
    ])
      void client.invalidateQueries({ queryKey: key })
  }
  return (
    <div className="mx-auto max-w-7xl space-y-5 p-6 sm:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="section-eyebrow">NODE OPERATIONS</p>
          <h1 className="mt-2 text-2xl font-semibold">节点管理</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {currentCluster?.name || "按集群查看节点资源、状态与维护信息。"}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Select
            value={automatic ? "automatic" : "paused"}
            onValueChange={(value) => setAutomatic(value === "automatic")}
          >
            <SelectTrigger aria-label="节点自动刷新" className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="automatic">每 10 秒刷新</SelectItem>
              <SelectItem value="paused">暂停刷新</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            disabled={nodes.isFetching || inventory.isFetching}
            onClick={() => {
              if (clusterId) void nodes.refetch()
              void inventory.refetch()
              if (nodeName)
                void client.invalidateQueries({
                  queryKey: ["node", clusterId, nodeName],
                })
            }}
          >
            <RefreshCw className={nodes.isFetching ? "animate-spin" : ""} />
            刷新节点
          </Button>
        </div>
      </header>
      <ClusterPicker
        selection={clusterSelection}
        label="节点"
        onSwitch={() => setTarget(undefined)}
      />
      {inventory.isError && (
        <p role="alert" className="panel p-5 text-xs text-destructive">
          集群列表暂不可用，请刷新重试。
        </p>
      )}
      {clusterId && (
        <NodeFilters
          key={JSON.stringify(filters)}
          initial={filters}
          onApply={(values) => update({ ...values, page: "1", node: null })}
        />
      )}
      {((inventory.isPending && !clusterId) ||
        (nodes.isPending && !!clusterId)) && (
        <div role="status" aria-label="正在读取节点列表">
          <Skeleton className="h-64" />
        </div>
      )}
      {nodes.isError && clusterId && (
        <p
          role="alert"
          className="panel p-5 text-xs leading-6 text-destructive"
        >
          {errorMessage(nodes.error)}
        </p>
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
          <div className="panel-heading flex-wrap gap-3">
            <h2>
              节点列表{" "}
              <Badge variant="secondary" className="ml-2">
                {data.totalItems}
              </Badge>
            </h2>
            <div className="flex items-center gap-3">
              <span className="text-2xs text-muted-foreground">
                {timestamp(data.fetchedAt)} ·{" "}
                {data.cached ? "缓存数据" : "已更新"}
              </span>
              <div className="flex gap-1">
                <Button
                  size="icon"
                  variant={matrix ? "ghost" : "secondary"}
                  aria-label="节点表格视图"
                  aria-pressed={!matrix}
                  onClick={() => update({ view: null })}
                >
                  <List />
                </Button>
                <Button
                  size="icon"
                  variant={matrix ? "secondary" : "ghost"}
                  aria-label="节点状态矩阵"
                  aria-pressed={matrix}
                  onClick={() => update({ view: "matrix" })}
                >
                  <Grid2X2 />
                </Button>
              </div>
            </div>
          </div>
          {canControl && (
            <div className="flex flex-wrap items-center justify-between gap-3 border-b bg-muted/20 px-4 py-3">
              <span className="text-2xs text-muted-foreground">
                已选 {names.length} / 10 · 仅本页选择
              </span>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={!names.length}
                  onClick={() => setSelection({ scope, names: [] })}
                >
                  清空选择
                </Button>
                {canDrain && (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={!names.length || nodes.isFetching}
                    onClick={() =>
                      setTarget({
                        clusterId,
                        scope,
                        names: [...names].sort(),
                        action: "drain",
                      })
                    }
                  >
                    <PauseCircle />
                    批量 Drain
                  </Button>
                )}
                {canResume && (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={!names.length || nodes.isFetching}
                    onClick={() =>
                      setTarget({
                        clusterId,
                        scope,
                        names: [...names].sort(),
                        action: "resume",
                      })
                    }
                  >
                    <Play />
                    批量 Resume
                  </Button>
                )}
              </div>
            </div>
          )}
          {matrix ? (
            <div
              className="grid gap-3 p-4 sm:grid-cols-2 xl:grid-cols-3"
              aria-label="当前页节点状态矩阵"
            >
              {rows.map((n) => (
                <article
                  key={n.name}
                  className="space-y-3 rounded-md border bg-muted/10 p-4"
                >
                  <div className="flex items-center gap-3">
                    {selectNode(n)}
                    <Button
                      variant="link"
                      className="h-auto p-0 font-mono text-xs break-all"
                      onClick={() => openNode(n.name)}
                    >
                      {n.name}
                    </Button>
                  </div>
                  <div className="flex flex-wrap gap-1">
                    {n.states.map((state) => (
                      <SlurmStatusBadge key={state} state={state} kind="node" />
                    ))}
                  </div>
                  <p className="font-mono text-2xs text-muted-foreground">
                    CPU {n.allocatedCpus}/{n.cpus} · GPU{" "}
                    {n.resources.gpus.allocated ?? "?"}/
                    {n.resources.gpus.total ?? "?"}
                  </p>
                  <p className="text-2xs text-muted-foreground">
                    内存 {n.allocatedMemoryMiB.toLocaleString()}/
                    {n.memoryMiB.toLocaleString()} MiB
                  </p>
                  <p className="text-2xs leading-5 break-all text-muted-foreground">
                    {n.reason || "没有额外状态原因"}
                  </p>
                  {controls(n)}
                </article>
              ))}
            </div>
          ) : (
            <Table>
              <TableHeader>
                {table.getHeaderGroups().map((group) => (
                  <TableRow key={group.id}>
                    {canControl && (
                      <TableHead className="w-10 pl-4">
                        <Checkbox
                          aria-label="选择本页前 10 个节点"
                          checked={
                            names.length > 0 &&
                            names.length === Math.min(rows.length, 10)
                              ? true
                              : names.length > 0
                                ? "indeterminate"
                                : false
                          }
                          disabled={!rows.length}
                          onCheckedChange={(checked) =>
                            setSelection({
                              scope,
                              names:
                                checked === true
                                  ? rows.slice(0, 10).map((n) => n.name)
                                  : [],
                            })
                          }
                        />
                      </TableHead>
                    )}
                    {group.headers.map((header) => (
                      <TableHead
                        key={header.id}
                        className={
                          ["cpus", "memory", "gpu"].includes(header.id)
                            ? "hidden lg:table-cell"
                            : ""
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
                    {canControl && (
                      <TableCell className="pl-4 align-top">
                        {selectNode(row.original)}
                      </TableCell>
                    )}
                    {row.getVisibleCells().map((cell) => (
                      <TableCell
                        key={cell.id}
                        className={`py-4 align-top ${["cpus", "memory", "gpu"].includes(cell.column.id) ? "hidden lg:table-cell" : ""}`}
                      >
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext()
                        )}
                      </TableCell>
                    ))}
                    <TableCell className="py-4 align-top">
                      {controls(row.original)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          {!rows.length && (
            <div className="space-y-3 p-12 text-center">
              <SearchX className="mx-auto size-7 text-muted-foreground" />
              <p className="text-sm font-medium">
                {data.totalItems > 0
                  ? "本页没有节点"
                  : "没有符合条件的可见节点"}
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
              {matrix && " · 矩阵仅展示当前页"}
            </span>
            <div className="flex items-center gap-2">
              <Select
                value={String(perPage)}
                onValueChange={(value) => update({ perPage: value, page: "1" })}
              >
                <SelectTrigger aria-label="每页节点数" className="w-28">
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
                aria-label="上一页节点"
                disabled={page <= 1 || nodes.isFetching}
                onClick={() => update({ page: String(page - 1) })}
              >
                <ChevronLeft />
              </Button>
              <Button
                size="icon"
                variant="outline"
                aria-label="下一页节点"
                disabled={page * perPage >= data.totalItems || nodes.isFetching}
                onClick={() => update({ page: String(page + 1) })}
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </section>
      )}
      {clusterId &&
        operationIds.length > 0 &&
        (validOperations ? (
          <NodeOperations
            key={`${clusterId}:${operationIds.join(",")}`}
            clusterId={clusterId}
            ids={operationIds}
            automatic={automatic}
          />
        ) : (
          <p role="alert" className="panel p-4 text-xs text-destructive">
            操作记录链接不合法，每次最多查询 10 条。
          </p>
        ))}
      <p className="text-2xs leading-6 text-muted-foreground">
        资源表示 Slurm 调度容量与分配量，GPU
        不是运行时利用率。节点控制要求集群动作权限，并由固定 REST 身份接受 Slurm
        上游授权；维护备注与预留由管理员维护。
      </p>
      {clusterId && nodeName && nodeNameValid(nodeName) && !activeTarget && (
        <NodeDetail
          key={`${clusterId}:${nodeName}`}
          clusterId={clusterId}
          name={nodeName}
          automatic={automatic}
          canDrain={canDrain}
          canResume={canResume}
          onControl={(action) =>
            setTarget({
              clusterId,
              scope,
              names: [nodeName],
              action,
              detail: nodeName,
            })
          }
          onClose={() => update({ node: null })}
        />
      )}
      {activeTarget && (
        <NodeControlDialog
          key={`${clusterId}:${activeTarget.action}:${activeTarget.names.join(",")}`}
          clusterId={clusterId}
          names={activeTarget.names}
          action={activeTarget.action}
          onClose={() => setTarget(undefined)}
          onStart={(ids) =>
            update({ cluster: clusterId, operations: ids.join(",") })
          }
          onResult={afterControl}
        />
      )}
    </div>
  )
}
