import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { z } from "zod"
import { CalendarRange, Plus, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  ClusterPicker,
  useAccessibleClusters,
  pageNumber,
} from "@/features/clusters/cluster-picker"
import { ReservationEditor } from "@/features/reservations/editor"
import {
  type Action,
  actions,
  states,
  pageSchema,
  detailSchema,
  reservationOperationSchema,
  dateTime,
  timezone,
  errorMessage,
  statuses,
} from "@/features/reservations/schema"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
function Search({ q, onSearch }: { q: string; onSearch: (v: string) => void }) {
  const [value, setValue] = useState(q)
  return (
    <form
      className="flex flex-wrap items-end gap-3"
      onSubmit={(e) => {
        e.preventDefault()
        onSearch(value)
      }}
    >
      <div className="min-w-52 flex-1 space-y-1.5">
        <Label htmlFor="reservation-search">搜索预留</Label>
        <Input
          id="reservation-search"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="名称、节点、用户或 Account"
          maxLength={200}
        />
      </div>
      <Button variant="outline" type="submit">
        筛选
      </Button>
      <Button
        variant="ghost"
        type="button"
        onClick={() => {
          setValue("")
          onSearch("")
        }}
      >
        清除搜索
      </Button>
    </form>
  )
}
export function ReservationsPage() {
  const [params, setParams] = useSearchParams(),
    selection = useAccessibleClusters(),
    cid = selection.id,
    user = useAuth((s) => s.user),
    client = useQueryClient(),
    [editing, setEditing] = useState<Action | null>(null)
  const perms = [
      ...(user?.access.platformPermissions ?? []),
      ...(user?.access.clusters.find((c) => c.cluster.id === cid)
        ?.permissions ?? []),
    ],
    has = (...v: string[]) => v.every((p) => perms.includes(p)),
    canRead = has("reservations.read")
  const item = params.get("item") || "",
    operation = params.get("operation") || "",
    page = pageNumber(params.get("page")),
    q = params.get("q") || ""
  const update = (v: Record<string, string | null>) =>
    setParams((old) => {
      const n = new URLSearchParams(old)
      for (const [k, x] of Object.entries(v)) {
        if (x) n.set(k, x)
        else n.delete(k)
      }
      return n
    })
  const base = `/clusters/${encodeURIComponent(cid)}`,
    filter = new URLSearchParams({
      page: String(page),
      perPage: "20",
      sort: params.get("sort") || "name",
      order: params.get("order") || "asc",
    })
  if (q) filter.set("q", q)
  if (params.get("state")) filter.set("state", params.get("state")!)
  const list = useQuery({
    queryKey: ["reservations", cid, "list", filter.toString()],
    enabled: !!cid && canRead,
    retry: false,
    staleTime: 15000,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      pageSchema.parse(await api(`${base}/reservations?${filter}`, { signal })),
  })
  const detail = useQuery({
    queryKey: ["reservations", cid, "detail", item],
    enabled: !!cid && canRead && !!item,
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      detailSchema.parse(
        await api(`${base}/reservations/${encodeURIComponent(item)}`, {
          signal,
        })
      ),
  })
  const recent = useQuery({
    queryKey: ["reservations", cid, "recent"],
    enabled: !!cid && canRead,
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      z
        .object({ items: z.array(reservationOperationSchema) })
        .parse(await api(`${base}/reservation-operations`, { signal })),
  })
  const result = useQuery({
    queryKey: ["reservations", cid, "operation", operation],
    enabled: !!cid && canRead && !!operation,
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      reservationOperationSchema.parse(
        await api(
          `${base}/reservation-operations/${encodeURIComponent(operation)}`,
          { signal }
        )
      ),
  })
  const target = canRead && !detail.isError ? detail.data?.item : undefined,
    rows = canRead && !list.isError ? (list.data?.items ?? []) : []
  const openOperation = (id: string) =>
    update({ operation: id, item: null, cluster: cid })
  const refresh = () =>
    void client.invalidateQueries({ queryKey: ["reservations", cid] })
  return (
    <div className="space-y-6 p-4 md:p-6 lg:p-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="font-mono text-xs font-semibold tracking-[0.2em] text-primary">
            ADVANCED RESERVATIONS
          </p>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight sm:text-3xl">
            Reservation 资源预留
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            按时间窗口保留节点，查看访问范围及维护策略。时间使用 {timezone}。
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={refresh}
            disabled={!canRead || list.isFetching}
          >
            <RefreshCw className="size-4" />
            刷新预留
          </Button>
          {cid &&
            has("reservations.read", "reservations.create", "nodes.read") && (
              <Button onClick={() => setEditing("reservation_create")}>
                <Plus className="size-4" />
                新建预留
              </Button>
            )}
        </div>
      </div>
      <ClusterPicker
        selection={selection}
        label="预留"
        onSwitch={() => {
          setEditing(null)
          update({ item: null, operation: null })
        }}
      />
      {cid && (
        <>
          <p className="rounded-lg border bg-card p-3 text-sm text-muted-foreground">
            预留会影响可调度资源。支持明确节点的新建、尚未开始预留的结束时间调整，以及经作业检查的删除；周期、动态资源和其他高级策略保留查询，由管理员维护。
          </p>
          <Search
            key={`${cid}:${q}`}
            q={q}
            onSearch={(q) => update({ q, page: "1", item: null })}
          />
          <div className="flex flex-wrap items-center gap-3">
            <Label htmlFor="reservation-state">时间状态</Label>
            <Select
              value={params.get("state") || "all"}
              onValueChange={(state) =>
                update({
                  state: state === "all" ? null : state,
                  page: "1",
                  item: null,
                })
              }
            >
              <SelectTrigger id="reservation-state" className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                {Object.entries(states).map(([k, v]) => (
                  <SelectItem key={k} value={k}>
                    {v}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={params.get("sort") || "name"}
              onValueChange={(sort) => update({ sort, page: "1", item: null })}
            >
              <SelectTrigger aria-label="预留排序" className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="name">名称</SelectItem>
                <SelectItem value="startTime">开始时间</SelectItem>
                <SelectItem value="endTime">结束时间</SelectItem>
              </SelectContent>
            </Select>
            <Button
              variant="ghost"
              onClick={() =>
                update({
                  order: params.get("order") === "desc" ? "asc" : "desc",
                })
              }
            >
              排序：{params.get("order") === "desc" ? "降序" : "升序"}
            </Button>
          </div>
          {!canRead ? (
            <p role="alert">没有此集群的预留读取权限。</p>
          ) : list.isError ? (
            <p role="alert" className="text-destructive">
              {errorMessage(list.error)}
            </p>
          ) : list.isPending ? (
            <Skeleton className="h-64" />
          ) : (
            <section className="overflow-hidden rounded-lg border bg-card">
              <div className="flex items-center justify-between border-b p-4">
                <h2 className="font-semibold">
                  资源预留 · {list.data?.totalItems ?? 0}
                </h2>
                <span className="text-xs text-muted-foreground">
                  {list.data?.cached ? "缓存采样" : "实时查询"} ·{" "}
                  {list.data?.fetchedAt
                    ? new Date(list.data.fetchedAt).toLocaleTimeString(
                        "zh-CN",
                        { hour12: false }
                      )
                    : "—"}
                </span>
              </div>
              <Table aria-label="资源预留列表">
                <TableHeader>
                  <TableRow>
                    <TableHead>名称 / 状态</TableHead>
                    <TableHead>时间窗口</TableHead>
                    <TableHead>节点</TableHead>
                    <TableHead>用户 / Account</TableHead>
                    <TableHead>策略</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((r) => (
                    <TableRow key={r.name}>
                      <TableCell>
                        <Button
                          variant="link"
                          className="px-0 font-mono"
                          onClick={() => update({ item: r.name, cluster: cid })}
                        >
                          {r.name}
                        </Button>
                        <div>
                          <Badge variant="outline">{states[r.state]}</Badge>
                        </div>
                      </TableCell>
                      <TableCell className="text-xs whitespace-nowrap">
                        {dateTime(r.startTime)}
                        <br />
                        {dateTime(r.endTime)}
                      </TableCell>
                      <TableCell className="max-w-48 break-all">
                        <p className="font-mono text-xs">
                          {r.nodes || "未限定节点"}
                        </p>
                        <p className="text-xs text-muted-foreground">
                          {r.nodeCount} 节点 · {r.coreCount} 核
                        </p>
                      </TableCell>
                      <TableCell className="max-w-48 text-xs break-all">
                        {r.users || "—"}
                        <br />
                        {r.accounts || "—"}
                      </TableCell>
                      <TableCell className="max-w-48 text-xs">
                        {r.flags.join(", ") || "普通预留"}
                      </TableCell>
                    </TableRow>
                  ))}
                  {rows.length === 0 && (
                    <TableRow>
                      <TableCell
                        colSpan={5}
                        className="h-28 text-center text-muted-foreground"
                      >
                        <CalendarRange className="mx-auto mb-2 size-5" />
                        暂无符合条件的预留。
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
              <div className="flex justify-end gap-3 border-t p-3">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page <= 1}
                  onClick={() => update({ page: String(page - 1), item: null })}
                >
                  上一页预留
                </Button>
                <span className="self-center text-xs">第 {page} 页</span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page * 20 >= (list.data?.totalItems ?? 0)}
                  onClick={() => update({ page: String(page + 1), item: null })}
                >
                  下一页预留
                </Button>
              </div>
            </section>
          )}
          {operation && canRead && (
            <section
              aria-label="预留操作结果"
              className="space-y-3 rounded-lg border bg-card p-4"
            >
              <div className="flex items-center justify-between">
                <h2 className="font-semibold">预留操作结果</h2>
                <Button
                  variant="outline"
                  onClick={() => void result.refetch()}
                  disabled={result.isFetching}
                >
                  只读核实结果
                </Button>
              </div>
              <p className="font-mono text-xs break-all">{operation}</p>
              {result.isError ? (
                <p role="alert" className="text-destructive">
                  {errorMessage(result.error)}
                </p>
              ) : (
                result.data && (
                  <>
                    <Badge variant="outline">
                      {statuses[result.data.status] || result.data.status}
                    </Badge>
                    <p className="text-sm">{result.data.message}</p>
                    <p className="text-sm">
                      {result.data.spec.reservation.name} · {result.data.reason}
                    </p>
                  </>
                )
              )}
            </section>
          )}
          {canRead && (
            <section
              aria-label="最近预留操作"
              className="space-y-3 rounded-lg border p-4"
            >
              <h2 className="font-semibold">本人最近预留操作</h2>
              {recent.isError ? (
                <p role="alert">{errorMessage(recent.error)}</p>
              ) : (
                <div className="flex flex-wrap gap-2">
                  {recent.data?.items.map((v) => (
                    <Button
                      key={v.operationId}
                      variant="outline"
                      size="sm"
                      onClick={() => openOperation(v.operationId)}
                    >
                      {v.spec.reservation.name} · {actions[v.action as Action]}{" "}
                      · {statuses[v.status] || v.status}
                    </Button>
                  ))}
                  {recent.data?.items.length === 0 && (
                    <p className="text-sm text-muted-foreground">
                      暂无预留操作。
                    </p>
                  )}
                </div>
              )}
            </section>
          )}
          {has("platform.audit.read") && (
            <Link className="text-sm text-primary" to="/audits">
              查看平台操作审计
            </Link>
          )}
        </>
      )}
      <Sheet
        open={!!item}
        onOpenChange={(open) => {
          if (!open) {
            setEditing(null)
            update({ item: null })
          }
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-2xl">
          <SheetHeader>
            <SheetTitle>{target?.name || "资源预留详情"}</SheetTitle>
            <SheetDescription>
              查看完整资源、时间及访问范围；过期预留可能已被 Slurm 自动移除。
            </SheetDescription>
          </SheetHeader>
          <div className="space-y-5 px-5 pb-6">
            {detail.isError ? (
              <p role="alert">{errorMessage(detail.error)}</p>
            ) : !target ? (
              <Skeleton className="h-48" />
            ) : (
              <>
                <Badge variant="outline">{states[target.state]}</Badge>
                <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-3 text-sm">
                  {Object.entries({
                    开始时间: dateTime(target.startTime),
                    结束时间: dateTime(target.endTime),
                    节点: target.nodes || "—",
                    容量: `${target.nodeCount} 节点 / ${target.coreCount} 核`,
                    用户: target.users || "—",
                    Account: target.accounts || "—",
                    用户组: target.groups || "—",
                    分区: target.partition || "—",
                    标记: target.flags.join(", ") || "—",
                    TRES: target.tres || "—",
                    特征: target.features || "—",
                    Licenses: target.licenses || "—",
                    BurstBuffer: target.burstBuffer || "—",
                  }).map(([k, v]) => (
                    <div className="contents" key={k}>
                      <dt className="text-muted-foreground">{k}</dt>
                      <dd className="font-mono break-all">{v}</dd>
                    </div>
                  ))}
                </dl>
                {!target.editable && (
                  <p className="text-sm text-muted-foreground">
                    包含高级或未知配置，本项仅支持查询。
                  </p>
                )}
                <div className="flex flex-wrap gap-3">
                  {target.editable &&
                    target.state === "scheduled" &&
                    has("reservations.update", "jobs.read") && (
                      <Button
                        variant="outline"
                        onClick={() => setEditing("reservation_update")}
                      >
                        调整结束时间
                      </Button>
                    )}
                  {target.editable &&
                    has("reservations.delete", "jobs.read") && (
                      <Button
                        variant="destructive"
                        onClick={() => setEditing("reservation_delete")}
                      >
                        删除预留
                      </Button>
                    )}
                </div>
              </>
            )}
          </div>
        </SheetContent>
      </Sheet>
      {editing && cid && (editing === "reservation_create" || target) && (
        <ReservationEditor
          key={`${cid}:${editing}:${item}`}
          clusterId={cid}
          action={editing}
          target={editing === "reservation_create" ? undefined : target}
          onClose={() => setEditing(null)}
          onOperation={openOperation}
        />
      )}
    </div>
  )
}
