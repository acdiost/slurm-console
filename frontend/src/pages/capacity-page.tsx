import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { Activity, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { useAuth } from "@/stores/auth"
import { api } from "@/services/api"
import {
  ClusterPicker,
  useAccessibleClusters,
  pageNumber,
} from "@/features/clusters/cluster-picker"
import {
  reportSchema,
  historySchema,
  resource,
  percentage,
  number,
  memory,
  percent,
  date,
  denied,
  errorMessage,
  type Frame,
  type Resource,
} from "@/features/capacity/schema"
import { CapacityTrend } from "@/features/capacity/trend"
const selectStyle = "h-10 rounded-md border bg-background px-3 text-sm"
export function CapacityPage() {
  const user = useAuth((s) => s.user),
    selection = useAccessibleClusters(),
    [params, setParams] = useSearchParams(),
    [automatic, setAutomatic] = useState(true)
  const id = selection.id,
    allowed = ["capacity.read", "nodes.read"].every(
      (p) =>
        user?.access.platformPermissions.includes(p) ||
        user?.access.clusters.some(
          (c) => c.cluster.id === id && c.permissions.includes(p)
        )
    )
  const page = pageNumber(params.get("page")),
    perPage = [20, 50, 100].includes(Number(params.get("perPage")))
      ? Number(params.get("perPage"))
      : 20,
    q = params.get("q") || "",
    sort = params.get("sort") || "name",
    order = params.get("order") || "asc",
    range = params.get("range") || "24h",
    selected = (params.get("resource") || "cpu") as Resource
  const query = new URLSearchParams({
    page: String(page),
    perPage: String(perPage),
    sort,
    order,
  })
  if (q) query.set("q", q)
  const report = useQuery({
    queryKey: ["capacity", user?.id, id, query.toString()],
    enabled: !!id && allowed,
    queryFn: async ({ signal }) =>
      reportSchema.parse(
        await api(`/clusters/${id}/capacity?${query}`, { signal })
      ),
    retry: false,
    refetchInterval: (state) =>
      automatic && !denied(state.state.error) ? 30000 : false,
  })
  const history = useQuery({
    queryKey: ["capacity-history", user?.id, id, range],
    enabled: !!id && allowed,
    queryFn: async ({ signal }) =>
      historySchema.parse(
        await api(
          `/clusters/${id}/capacity/history?${new URLSearchParams({ range })}`,
          { signal }
        )
      ),
    retry: false,
    refetchInterval: (state) =>
      automatic && !denied(state.state.error) ? 60000 : false,
  })
  const blocked = denied(report.error) || denied(history.error),
    current = !blocked && !report.isError ? report.data : undefined,
    past = !blocked && !history.isError ? history.data : undefined
  const update = (key: string, value: string) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      if (value) next.set(key, value)
      else next.delete(key)
      if (!["range", "resource"].includes(key)) next.set("page", "1")
      return next
    })
  return (
    <div className="space-y-6 p-5 sm:p-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="eyebrow">CLUSTER CAPACITY</p>
          <h1 className="mt-2 flex items-center gap-2 text-2xl font-semibold">
            <Activity className="size-6" />
            集群容量分析
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {selection.current?.name ||
              "选择集群查看配置容量、调度分配及节点余量。"}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Label className="flex items-center gap-2 text-sm">
            <Checkbox
              aria-label="自动刷新容量"
              checked={automatic}
              onCheckedChange={(v) => setAutomatic(v === true)}
            />
            自动刷新
          </Label>
          <Button
            variant="outline"
            disabled={
              !id || !allowed || report.isFetching || history.isFetching
            }
            onClick={() => {
              void report.refetch()
              void history.refetch()
            }}
          >
            <RefreshCw />
            刷新容量
          </Button>
        </div>
      </header>
      <ClusterPicker selection={selection} label="容量分析" />
      {!id && (
        <p role="status" className="panel p-5 text-muted-foreground">
          {selection.inventory.isPending
            ? "正在读取可用集群…"
            : "暂无可用集群，请检查集群授权或连接配置。"}
        </p>
      )}
      {!allowed && id ? (
        <p role="alert" className="panel p-5 text-destructive">
          需要此集群的容量分析和节点读取权限。
        </p>
      ) : blocked ? (
        <p role="alert" className="panel p-5 text-destructive">
          {errorMessage(denied(report.error) ? report.error : history.error)}
        </p>
      ) : (
        allowed &&
        id && (
          <>
            <section className="space-y-4" aria-label="当前集群容量">
              <div className="flex flex-wrap justify-between gap-2">
                <h2 className="font-semibold">当前配置与分配</h2>
                <p className="text-xs text-muted-foreground">
                  {current
                    ? `${date(current.source.fetchedAt)} · ${current.source.cached ? "缓存采样" : "已更新"}`
                    : "正在读取当前来源"}
                </p>
              </div>
              {report.isError ? (
                <p role="alert" className="panel p-5 text-destructive">
                  {errorMessage(report.error)}
                </p>
              ) : report.isPending ? (
                <p role="status" className="panel p-5 text-muted-foreground">
                  正在读取节点容量…
                </p>
              ) : (
                current && (
                  <>
                    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                      <div className="panel space-y-3 p-5">
                        <p className="text-sm text-muted-foreground">节点</p>
                        <p className="text-3xl font-semibold tabular-nums">
                          {number(current.report.totals.nodes)}
                        </p>
                        <p className="text-sm">
                          状态可用{" "}
                          {number(current.report.headroom.stateAvailableNodes)}{" "}
                          · 不可用{" "}
                          {number(current.report.totals.unavailableNodes)}
                        </p>
                      </div>
                      <ResourceCard
                        frame={current.report.totals}
                        kind="cpu"
                        title="CPU"
                      />
                      <ResourceCard
                        frame={current.report.totals}
                        kind="memory"
                        title="内存"
                      />
                      <ResourceCard
                        frame={current.report.totals}
                        kind="gpu"
                        title="GPU"
                      />
                    </div>
                    <section
                      className="panel space-y-4 p-5"
                      aria-label="节点余量分布"
                    >
                      <h3 className="font-semibold">
                        状态可用节点的 CPU 余量分布
                      </h3>
                      <div className="grid gap-3 sm:grid-cols-5">
                        {current.report.headroom.cpuIdleBuckets.map((b) => (
                          <div key={b.label} className="rounded-lg border p-3">
                            <p className="text-xs text-muted-foreground">
                              {b.label} CPU 余量
                            </p>
                            <p className="mt-2 text-xl font-semibold">
                              {number(b.nodes)}{" "}
                              <span className="text-xs font-normal text-muted-foreground">
                                节点
                              </span>
                            </p>
                          </div>
                        ))}
                      </div>
                      <p className="text-sm">
                        单节点最大 CPU 余量{" "}
                        {number(current.report.headroom.largestCpuIdle)} ·
                        单节点最大内存余量{" "}
                        {memory(current.report.headroom.largestMemoryIdleMiB)}
                      </p>
                      <p className="text-xs leading-6 text-muted-foreground">
                        两个最大值分别计算，可能来自不同节点。余量还受分区、预留、QOS、拓扑及作业约束影响，具体调度由
                        Slurm 判定。
                      </p>
                    </section>
                  </>
                )
              )}
              <p className="text-xs leading-6 text-muted-foreground">
                已分配表示 Slurm 调度分配。CPU/内存实际使用率和 GPU
                实际利用率需对应监控指标；操作系统空闲内存与此处节点余量口径不同。
              </p>
            </section>
            <section className="panel space-y-4 p-5" aria-label="集群历史容量">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h2 className="font-semibold">集群整体容量趋势</h2>
                <div className="flex flex-wrap gap-3">
                  <select
                    aria-label="容量趋势窗口"
                    className={selectStyle}
                    value={range}
                    onChange={(e) => update("range", e.target.value)}
                  >
                    <option value="1h">最近 1 小时</option>
                    <option value="24h">最近 24 小时</option>
                    <option value="7d">最近 7 天</option>
                  </select>
                  <select
                    aria-label="容量趋势资源"
                    className={selectStyle}
                    value={selected}
                    onChange={(e) => update("resource", e.target.value)}
                  >
                    <option value="cpu">CPU</option>
                    <option value="memory">内存</option>
                    <option value="gpu">GPU</option>
                  </select>
                </div>
              </div>
              {history.isError ? (
                <p role="alert" className="text-destructive">
                  {errorMessage(history.error)}
                </p>
              ) : history.isPending ? (
                <p role="status">正在读取历史采样…</p>
              ) : (
                past &&
                ["cpu", "memory", "gpu"].includes(selected) && (
                  <CapacityTrend
                    key={`${id}:${range}:${selected}`}
                    history={past.history}
                    selected={selected}
                  />
                )
              )}
              <p className="text-xs text-muted-foreground">
                仅展示当前连接版本的已采样数据。统计覆盖完整的 5 分钟窗口，截至{" "}
                {past ? date(past.history.end) : "—"}
                ，全局卡片和趋势不受下方分区检索影响。
              </p>
            </section>
            <section className="panel space-y-4 p-5" aria-label="分区容量对比">
              <h2 className="font-semibold">分区容量对比</h2>
              <p className="text-xs leading-6 text-muted-foreground">
                按节点的分区归属分别汇总；共享节点会出现在多个分区，因此各行不能相加为集群总量。分区配置状态独立授权，节点余量仍需结合分区限制判断。
              </p>
              <div className="flex flex-wrap items-end gap-3">
                <PartitionSearch
                  key={`${id}:${q}`}
                  value={q}
                  onSearch={(v) => update("q", v)}
                />
                <Label className="grid gap-2">
                  排序
                  <select
                    aria-label="分区容量排序"
                    className={selectStyle}
                    value={sort}
                    onChange={(e) => update("sort", e.target.value)}
                  >
                    {Object.entries({
                      name: "分区名称",
                      nodes: "节点数",
                      cpuTotal: "CPU 配置量",
                      cpuIdle: "CPU 余量",
                      memoryTotal: "内存配置量",
                      gpuTotal: "GPU 配置量",
                    }).map(([k, v]) => (
                      <option key={k} value={k}>
                        {v}
                      </option>
                    ))}
                  </select>
                </Label>
                <select
                  aria-label="分区容量排序方向"
                  className={selectStyle}
                  value={order}
                  onChange={(e) => update("order", e.target.value)}
                >
                  <option value="asc">升序</option>
                  <option value="desc">降序</option>
                </select>
              </div>
              {current?.partitionSource.status !== "ok" && (
                <p className="text-xs text-muted-foreground">
                  {current?.partitionSource.message || "分区配置状态暂不可用。"}
                </p>
              )}
              {current && (
                <>
                  <div className="overflow-x-auto">
                    <table className="w-full text-left text-sm whitespace-nowrap">
                      <thead className="border-b text-muted-foreground">
                        <tr>
                          {[
                            "分区",
                            "配置状态",
                            "节点",
                            "CPU 配置 / 分配 / 余量",
                            "内存配置 / 余量",
                            "GPU 分配 / 配置",
                            "资源",
                          ].map((h) => (
                            <th key={h} className="p-3 font-medium">
                              {h}
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {current.report.partitions.map((p) => (
                          <tr key={p.name} className="border-b">
                            <td className="p-3 font-medium">
                              {p.name || "未归属分区"}
                            </td>
                            <td className="p-3">
                              {p.states
                                ? p.states.map((s) => (
                                    <Badge
                                      key={s}
                                      variant="outline"
                                      className="mr-1"
                                    >
                                      {s}
                                    </Badge>
                                  ))
                                : "—"}
                            </td>
                            <td className="p-3 tabular-nums">
                              {number(p.frame.nodes)}
                            </td>
                            <td className="p-3 font-mono">
                              {number(p.frame.cpus.total)} /{" "}
                              {number(p.frame.cpus.allocated)} /{" "}
                              {number(p.frame.cpus.idle)}
                            </td>
                            <td className="p-3">
                              {memory(p.frame.memoryMiB.total)} /{" "}
                              {memory(p.frame.memoryMiB.idle)}
                            </td>
                            <td className="p-3 font-mono">
                              {number(p.frame.gpus.allocated)} /{" "}
                              {number(p.frame.gpus.total)}
                            </td>
                            <td className="p-3">
                              {p.name && (
                                <Link
                                  className="text-primary underline underline-offset-4"
                                  to={`/nodes?${new URLSearchParams({ cluster: id, partition: p.name, filterMode: "custom" })}`}
                                >
                                  查看节点
                                </Link>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                    {!current.report.partitions.length && (
                      <p className="py-7 text-center text-muted-foreground">
                        没有匹配的分区容量。
                      </p>
                    )}
                  </div>
                  <div className="flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
                    <span>
                      共 {current.report.totalItems} 个分区归属 · 第 {page} 页
                    </span>
                    <select
                      className={`${selectStyle} ml-auto`}
                      aria-label="每页容量分区数"
                      value={perPage}
                      onChange={(e) => update("perPage", e.target.value)}
                    >
                      {[20, 50, 100].map((v) => (
                        <option key={v} value={v}>
                          {v} 条 / 页
                        </option>
                      ))}
                    </select>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={page <= 1 || report.isFetching}
                      onClick={() =>
                        setParams((p) => {
                          const n = new URLSearchParams(p)
                          n.set("page", String(page - 1))
                          return n
                        })
                      }
                    >
                      上一页分区容量
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={
                        page * perPage >= current.report.totalItems ||
                        report.isFetching
                      }
                      onClick={() =>
                        setParams((p) => {
                          const n = new URLSearchParams(p)
                          n.set("page", String(page + 1))
                          return n
                        })
                      }
                    >
                      下一页分区容量
                    </Button>
                  </div>
                </>
              )}
            </section>
          </>
        )
      )}
    </div>
  )
}
function PartitionSearch({
  value,
  onSearch,
}: {
  value: string
  onSearch: (q: string) => void
}) {
  const [draft, setDraft] = useState(value)
  return (
    <form
      className="flex items-end gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        onSearch(draft.trim())
      }}
    >
      <div className="space-y-2">
        <Label htmlFor="capacity-q">检索分区名称</Label>
        <Input
          id="capacity-q"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          maxLength={128}
        />
      </div>
      <Button type="submit" variant="outline">
        检索分区
      </Button>
    </form>
  )
}
function ResourceCard({
  frame,
  kind,
  title,
}: {
  frame: Frame
  kind: Resource
  title: string
}) {
  const v = resource(frame, kind),
    format = kind === "memory" ? memory : number
  return (
    <div className="panel space-y-3 p-5">
      <p className="text-sm text-muted-foreground">{title} 配置量</p>
      <p className="text-3xl font-semibold tabular-nums">{format(v.total)}</p>
      <div
        className="flex h-2 overflow-hidden rounded-full bg-muted"
        aria-hidden
      >
        {[
          [v.allocated, "bg-primary"],
          [v.idle, "bg-emerald-500"],
          [v.unavailable, "bg-amber-500"],
        ].map(([n, c], i) => (
          <span
            key={i}
            className={String(c)}
            style={{
              width: `${percentage(n as number | null, v.total) || 0}%`,
            }}
          />
        ))}
      </div>
      <div className="space-y-1 text-xs">
        <p>
          已分配 {format(v.allocated)} ·{" "}
          {percent(percentage(v.allocated, v.total))}
        </p>
        <p>
          节点余量 {format(v.idle)} · 不可用 {format(v.unavailable)}
        </p>
        {kind === "gpu" && frame.gpus.unknownNodes > 0 && (
          <p className="text-amber-700">
            {frame.gpus.unknownNodes} 个节点的 GPU 数量或分配量未知。
          </p>
        )}
      </div>
    </div>
  )
}
