import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { RefreshCw, Activity } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  SelectValue,
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
  TableHeader,
  TableHead,
  TableRow,
  TableBody,
  TableCell,
} from "@/components/ui/table"
import {
  ClusterPicker,
  useAccessibleClusters,
  pageNumber,
} from "@/features/clusters/cluster-picker"
import {
  pageSchema,
  historySchema,
  labels,
  metrics,
  value,
  time,
  type Device,
  errorMessage,
} from "@/features/gpu/schema"
import { GPUTrend } from "@/features/gpu/trend"
import { useAuth } from "@/stores/auth"
import { api } from "@/services/api"
function Search({ q, onSearch }: { q: string; onSearch: (q: string) => void }) {
  const [text, setText] = useState(q)
  return (
    <form
      className="flex items-end gap-3"
      onSubmit={(e) => {
        e.preventDefault()
        onSearch(text)
      }}
    >
      <div className="flex-1 space-y-1.5">
        <Label htmlFor="gpu-search">搜索 GPU</Label>
        <Input
          id="gpu-search"
          placeholder="节点、UUID 或型号"
          value={text}
          onChange={(e) => setText(e.target.value)}
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
          setText("")
          onSearch("")
        }}
      >
        清除
      </Button>
    </form>
  )
}
export function GPUPage() {
  const [params, setParams] = useSearchParams(),
    selection = useAccessibleClusters(),
    cid = selection.id,
    user = useAuth((s) => s.user),
    client = useQueryClient(),
    [refresh, setRefresh] = useState("30"),
    [metric, setMetric] = useState("utilization")
  const permissions = [
      ...(user?.access.platformPermissions ?? []),
      ...(user?.access.clusters.find((c) => c.cluster.id === cid)
        ?.permissions ?? []),
    ],
    allowed = permissions.includes("metrics.read"),
    nodesAllowed = permissions.includes("nodes.read"),
    page = pageNumber(params.get("page")),
    q = params.get("q") || "",
    uuid = params.get("gpu") || "",
    node = params.get("node") || "",
    window = params.get("range") || "1h"
  const update = (data: Record<string, string | null>) =>
    setParams((p) => {
      const n = new URLSearchParams(p)
      for (const [k, v] of Object.entries(data)) {
        if (v) n.set(k, v)
        else n.delete(k)
      }
      return n
    })
  const filter = new URLSearchParams({
    page: String(page),
    perPage: "20",
    sort: params.get("sort") || "node",
    order: params.get("order") || "asc",
  })
  if (q) filter.set("q", q)
  if (params.get("status")) filter.set("status", params.get("status")!)
  const list = useQuery({
    queryKey: ["gpu", cid, filter.toString()],
    enabled: !!cid && allowed,
    retry: false,
    staleTime: 15000,
    refetchOnWindowFocus: false,
    refetchInterval: refresh === "off" ? false : Number(refresh) * 1000,
    queryFn: async ({ signal }) =>
      pageSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(cid)}/metrics/gpu?${filter}`,
          { signal }
        )
      ),
  })
  const history = useQuery({
    queryKey: ["gpu", cid, "history", node, uuid, window],
    enabled: !!cid && allowed && !!uuid && !!node,
    retry: false,
    staleTime: 30000,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      historySchema.parse(
        await api(
          `/clusters/${encodeURIComponent(cid)}/metrics/gpu/${encodeURIComponent(uuid)}/history?${new URLSearchParams({ node, range: window })}`,
          { signal }
        )
      ),
  })
  const data = allowed && !list.isError ? list.data : undefined,
    target = data?.items.find((d) => d.uuid === uuid && d.node === node),
    allocation =
      nodesAllowed && data?.allocation.status === "ok"
        ? data.allocation.item?.gpus
        : undefined
  const open = (d: Device) =>
    update({ gpu: d.uuid, node: d.node, cluster: cid })
  return (
    <div className="space-y-6 p-4 md:p-6 lg:p-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="font-mono text-xs font-semibold tracking-[0.2em] text-primary">
            GPU TELEMETRY
          </p>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight sm:text-3xl">
            GPU 监控
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            区分调度分配与真实运行指标，查看物理 GPU 利用率、显存、温度及功耗。
          </p>
        </div>
        <div className="flex gap-2">
          <Select value={refresh} onValueChange={setRefresh}>
            <SelectTrigger aria-label="GPU 自动刷新" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="15">每 15 秒刷新</SelectItem>
              <SelectItem value="30">每 30 秒刷新</SelectItem>
              <SelectItem value="off">暂停自动刷新</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            disabled={!allowed || list.isFetching}
            onClick={() =>
              void client.invalidateQueries({ queryKey: ["gpu", cid] })
            }
          >
            <RefreshCw className="size-4" />
            刷新 GPU
          </Button>
        </div>
      </div>
      <ClusterPicker
        selection={selection}
        label="GPU"
        onSwitch={() => update({ gpu: null, node: null, range: null })}
      />
      {!allowed && cid ? (
        <p role="alert">没有此集群的监控读取权限。</p>
      ) : list.isError ? (
        <p role="alert" className="text-destructive">
          {errorMessage(list.error)}
        </p>
      ) : !data ? (
        <Skeleton className="h-64" />
      ) : (
        <>
          <section
            aria-label="GPU 数据来源"
            className="space-y-2 rounded-lg border bg-card p-4"
          >
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="outline">
                {labels[data.monitoring.status] || data.monitoring.status}
              </Badge>
              <span className="text-sm">
                {data.monitoring.source || "Prometheus / DCGM"}
              </span>
              <span className="text-xs text-muted-foreground">
                {data.monitoring.cached ? "缓存采样" : "查询时间"} ·{" "}
                {time(data.monitoring.fetchedAt)}
              </span>
            </div>
            {data.monitoring.message && (
              <p className="text-sm text-muted-foreground">
                {data.monitoring.message}
              </p>
            )}
            <p className="text-xs text-muted-foreground">
              监控源由系统管理员配置。超过 120
              秒的样本不作为当前值；不根据分配数量推断利用率，不展示历史记账量。MIG
              实例本项不聚合为物理卡。
              {data.monitoring.ignoredSeries > 0
                ? ` 已忽略 ${data.monitoring.ignoredSeries} 条 MIG 或未映射节点序列。`
                : ""}
            </p>
          </section>
          <section
            className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4"
            aria-label="GPU 资源与观测概况"
          >
            {[
              ["Slurm 配置 GPU", allocation?.total, "调度口径"],
              [
                "Slurm 已分配 GPU",
                allocation?.allocated,
                "分配量不是实际利用率",
              ],
              [
                "物理 GPU 观测数",
                ["ok", "no_data"].includes(data.monitoring.status)
                  ? data.totalItems
                  : null,
                "当前监控筛选范围",
              ],
              [
                "指标完整且新鲜",
                ["ok", "no_data"].includes(data.monitoring.status)
                  ? (data.summary.fresh ?? 0)
                  : null,
                "五类指标均可用",
              ],
            ].map(([label, v, note]) => (
              <article
                key={String(label)}
                className="rounded-lg border bg-card p-4"
              >
                <h2 className="text-sm text-muted-foreground">{label}</h2>
                <p className="my-2 font-mono text-3xl font-semibold">
                  {v === null || v === undefined ? "未知" : v}
                </p>
                <p className="text-xs text-muted-foreground">{note}</p>
              </article>
            ))}
          </section>
          {(!nodesAllowed || data.allocation.status !== "ok") && (
            <p className="text-xs text-muted-foreground">
              Slurm 分配数据：
              {nodesAllowed
                ? data.allocation.message
                : "未授予 nodes.read，未查询节点分配量。"}
            </p>
          )}
          {data.allocation.status === "ok" && nodesAllowed && (
            <p className="text-xs text-muted-foreground">
              Slurm 采样 {time(data.allocation.fetchedAt)} · 不可用/预留 GPU：
              {allocation?.unavailable ?? "未知"}，空闲可调度：
              {allocation?.idle ?? "未知"}。与监控来源分别采样，数量可能不同。
            </p>
          )}
          <Search
            key={`${cid}:${q}`}
            q={q}
            onSearch={(q) => update({ q, page: "1", gpu: null, node: null })}
          />
          <div className="flex flex-wrap gap-3">
            <Select
              value={params.get("status") || "all"}
              onValueChange={(v) =>
                update({
                  status: v === "all" ? null : v,
                  page: "1",
                  gpu: null,
                  node: null,
                })
              }
            >
              <SelectTrigger aria-label="GPU 数据状态" className="w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部数据状态</SelectItem>
                {["fresh", "partial", "stale", "ambiguous"].map((v) => (
                  <SelectItem key={v} value={v}>
                    {labels[v]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={params.get("sort") || "node"}
              onValueChange={(sort) => update({ sort, page: "1" })}
            >
              <SelectTrigger aria-label="GPU 排序" className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="node">节点</SelectItem>
                <SelectItem value="utilization">实际利用率</SelectItem>
                <SelectItem value="temperature">温度</SelectItem>
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
          <section className="overflow-hidden rounded-lg border bg-card">
            <Table aria-label="物理 GPU 监控">
              <TableHeader>
                <TableRow>
                  <TableHead>GPU / 节点</TableHead>
                  <TableHead>实际利用率</TableHead>
                  <TableHead>显存（MiB）</TableHead>
                  <TableHead>温度 / 功耗</TableHead>
                  <TableHead>数据状态</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.items.map((d) => (
                  <TableRow key={`${d.node}/${d.uuid}`}>
                    <TableCell>
                      <Button
                        variant="link"
                        className="px-0 font-mono"
                        onClick={() => open(d)}
                      >
                        {d.uuid}
                      </Button>
                      <p className="text-xs">
                        {d.node} · {d.model || "型号未知"} · GPU{" "}
                        {d.index || "未知"}
                      </p>
                    </TableCell>
                    <TableCell className="font-mono">
                      {value(d.utilization, "%")}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      已用 {value(d.memoryUsedMiB)}
                      <br />
                      总量 {value(d.memoryTotalMiB)}
                      {d.totalSource === "used_plus_free" && (
                        <p className="text-muted-foreground">已用 + 空闲合计</p>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {value(d.temperatureC, "°C")}
                      <br />
                      {value(d.powerW, "W")}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">
                        {labels[d.status] || d.status}
                      </Badge>
                    </TableCell>
                  </TableRow>
                ))}
                {data.items.length === 0 && (
                  <TableRow>
                    <TableCell
                      colSpan={5}
                      className="h-32 text-center text-muted-foreground"
                    >
                      <Activity className="mx-auto mb-2 size-5" />
                      {data.monitoring.status === "not_configured"
                        ? "尚未接入实际 GPU 指标。"
                        : "没有符合条件的物理 GPU 观测；不代表硬件数量为零。"}
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
                onClick={() =>
                  update({ page: String(page - 1), gpu: null, node: null })
                }
              >
                上一页 GPU
              </Button>
              <span className="self-center text-xs">
                第 {page} 页 · {data.totalItems} 条匹配观测
              </span>
              <Button
                variant="outline"
                size="sm"
                disabled={page * 20 >= data.totalItems}
                onClick={() =>
                  update({ page: String(page + 1), gpu: null, node: null })
                }
              >
                下一页 GPU
              </Button>
            </div>
          </section>
        </>
      )}
      <Sheet
        open={!!uuid && allowed}
        onOpenChange={(open) => {
          if (!open) update({ gpu: null, node: null })
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-3xl">
          <SheetHeader>
            <SheetTitle>{uuid || "GPU 详情"}</SheetTitle>
            <SheetDescription>
              {node} · 实际运行指标与固定窗口趋势；不推断单卡作业占用。
            </SheetDescription>
          </SheetHeader>
          <div className="space-y-5 px-5 pb-6">
            {target && (
              <>
                <p className="text-sm">
                  {target.model || "型号未知"} · {labels[target.status]}
                </p>
                <div className="grid grid-cols-2 gap-3">
                  {Object.entries(metrics).map(([key, m]) => {
                    const s = target[key as keyof Device]
                    return (
                      <div key={key} className="rounded-md border p-3">
                        <p className="text-xs text-muted-foreground">
                          {m.label}
                        </p>
                        <p className="mt-1 font-mono text-lg">
                          {typeof s === "object" ? value(s, m.unit) : "未知"}
                        </p>
                        {typeof s === "object" && (
                          <p className="mt-1 text-xs text-muted-foreground">
                            {s.timestamp
                              ? new Date(s.timestamp * 1000).toLocaleString(
                                  "zh-CN",
                                  { hour12: false }
                                )
                              : "无采样时间"}
                          </p>
                        )}
                      </div>
                    )
                  })}
                </div>
                {target.totalSource === "used_plus_free" && (
                  <p className="text-xs text-muted-foreground">
                    监测显存总量由同时间附近的已用与空闲值相加，不含未单独采集的保留显存，不能等同显卡标称容量。
                  </p>
                )}
                {nodesAllowed && (
                  <Link
                    className="text-sm text-primary"
                    to={`/nodes?${new URLSearchParams({ cluster: cid, node })}`}
                  >
                    查看 Slurm 节点
                  </Link>
                )}
              </>
            )}
            <div className="flex gap-3">
              <Select
                value={window}
                onValueChange={(range) => update({ range })}
              >
                <SelectTrigger aria-label="GPU 趋势窗口" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1h">最近 1 小时</SelectItem>
                  <SelectItem value="6h">最近 6 小时</SelectItem>
                  <SelectItem value="24h">最近 24 小时</SelectItem>
                </SelectContent>
              </Select>
              <Select value={metric} onValueChange={setMetric}>
                <SelectTrigger aria-label="GPU 趋势指标">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(metrics).map(([k, v]) => (
                    <SelectItem key={k} value={k}>
                      {v.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                variant="outline"
                onClick={() => void history.refetch()}
                disabled={history.isFetching}
              >
                刷新趋势
              </Button>
            </div>
            {history.isError ? (
              <p role="alert" className="text-sm text-destructive">
                {errorMessage(history.error)}
              </p>
            ) : history.isPending ? (
              <Skeleton className="h-52" />
            ) : (
              history.data && (
                <>
                  <GPUTrend history={history.data} metric={metric} />
                  <p className="text-xs text-muted-foreground">
                    趋势查询 {time(history.data.fetchedAt)} ·{" "}
                    {history.data.cached ? "缓存" : "实时查询"}
                    ；当前值和趋势分别采样。
                  </p>
                </>
              )
            )}
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}
