import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ChartNoAxesCombined, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { useAuth } from "@/stores/auth"
import { api } from "@/services/api"
import {
  ClusterPicker,
  useAccessibleClusters,
  pageNumber,
} from "@/features/clusters/cluster-picker"
import {
  efficiencySchema,
  statuses,
  trendSegments,
  denied,
  date,
  percent,
  number,
  type Analysis,
  type EfficiencyPage,
} from "@/features/gpu-efficiency/schema"
const selectStyle = "h-10 rounded-md border bg-background px-3 text-sm"
export function GPUEfficiencyPage() {
  const user = useAuth((s) => s.user),
    selection = useAccessibleClusters(),
    id = selection.id
  const [params, setParams] = useSearchParams(),
    [automatic, setAutomatic] = useState(true)
  const allowed =
    !!user &&
    (user.access.platformPermissions.includes("metrics.read") ||
      user.access.clusters.some(
        (c) => c.cluster.id === id && c.permissions.includes("metrics.read")
      ))
  const q = params.get("q") || "",
    range = params.get("range") || "1h",
    sort = params.get("sort") || "node",
    order = params.get("order") || "asc",
    status = params.get("status") || "",
    page = pageNumber(params.get("page"))
  const query = new URLSearchParams({
    range,
    sort,
    order,
    page: String(page),
    perPage: "20",
  })
  if (q) query.set("q", q)
  if (status) query.set("status", status)
  const result = useQuery<EfficiencyPage>({
    queryKey: ["gpu-efficiency", user?.id, id, query.toString()],
    enabled: !!id && allowed,
    retry: false,
    refetchOnWindowFocus: false,
    refetchInterval: (state) =>
      automatic && !denied(state.state.error) ? 60000 : false,
    queryFn: async ({ signal }) =>
      efficiencySchema.parse(
        await api(`/clusters/${id}/metrics/gpu/efficiency?${query}`, { signal })
      ),
  })
  const data = allowed && !result.isError ? result.data : undefined,
    a = data?.analysis,
    known = a?.status === "ok" || a?.status === "no_data"
  const update = (key: string, value: string) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      if (value) next.set(key, value)
      else next.delete(key)
      next.set("page", "1")
      return next
    })
  return (
    <div className="space-y-6 p-5 sm:p-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="eyebrow">GPU RESOURCE EFFICIENCY</p>
          <h1 className="mt-2 flex items-center gap-2 text-2xl font-semibold">
            <ChartNoAxesCombined className="size-6" />
            GPU 资源效率分析
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {selection.current?.name || "选择集群查看物理 GPU 的采样效率线索。"}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Label className="flex items-center gap-2">
            <Checkbox
              aria-label="自动刷新 GPU 效率"
              checked={automatic}
              onCheckedChange={(v) => setAutomatic(v === true)}
            />
            自动刷新
          </Label>
          <Button
            variant="outline"
            disabled={!id || !allowed || result.isFetching}
            onClick={() => void result.refetch()}
          >
            <RefreshCw />
            刷新分析
          </Button>
          <Button variant="outline" asChild>
            <Link to={`/gpu?${new URLSearchParams({ cluster: id })}`}>
              GPU 监控
            </Link>
          </Button>
        </div>
      </header>
      <ClusterPicker selection={selection} label="GPU 效率" />
      {!id ? (
        <p role="status" className="panel p-5">
          {selection.inventory.isPending
            ? "正在读取可用集群…"
            : "暂无可用集群，请检查授权和连接。"}
        </p>
      ) : !allowed ? (
        <p role="alert" className="panel p-5 text-destructive">
          需要此集群的监控读取权限。
        </p>
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <Label className="flex items-center gap-3">
              分析窗口
              <select
                aria-label="GPU 效率窗口"
                className={selectStyle}
                value={range}
                onChange={(e) => update("range", e.target.value)}
              >
                <option value="1h">最近 1 小时</option>
                <option value="6h">最近 6 小时</option>
                <option value="24h">最近 24 小时</option>
              </select>
            </Label>
            <p className="text-xs text-muted-foreground">
              {a
                ? `${a.source || "Prometheus / DCGM"} · ${a.cached ? "缓存分析" : "已更新"} · ${date(a.fetchedAt)}`
                : "正在读取监控来源"}
            </p>
          </div>
          {result.isError ? (
            <p role="alert" className="panel p-5 text-destructive">
              {denied(result.error)
                ? "GPU 监控读取权限不可用，请检查当前集群授权。"
                : "GPU 效率来源暂不可用，请检查监控连接、采样和标签；缺失数据不会补零。"}
            </p>
          ) : result.isPending ? (
            <p role="status" className="panel p-5">
              正在分析窗口内的 GPU 采样…
            </p>
          ) : (
            a && (
              <>
                {a.status !== "ok" && (
                  <p role="status" className="panel p-5">
                    {a.status === "not_configured"
                      ? "尚未配置 Prometheus/DCGM 监控源，请由管理员接入既有来源。"
                      : "此窗口没有可识别的物理 GPU 样本，无法推断硬件数量。"}
                  </p>
                )}
                <section
                  aria-label="GPU 效率概览"
                  className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4"
                >
                  <Card
                    title="窗口观测 GPU"
                    value={known ? number(a.summary.observedDevices) : "—"}
                    detail="由窗口内可识别 UUID 统计，非硬件库存"
                  />
                  <Card
                    title="有效样本平均利用率"
                    value={known ? percent(a.summary.meanUtilization) : "—"}
                    detail={`${known ? number(a.summary.validSamples) : "—"} 个有效 GPU 样本，按样本等权`}
                  />
                  <Card
                    title="低利用率线索"
                    value={known ? number(a.summary.low) : "—"}
                    detail={`覆盖 ≥ ${a.minimumCoveragePercent}%，低于 ${a.lowThresholdPercent}% 的有效样本 ≥ ${a.minimumLowSamplePercent}%`}
                  />
                  <Card
                    title="不足以分析的 GPU"
                    value={
                      known
                        ? number(a.summary.insufficient + a.summary.ambiguous)
                        : "—"
                    }
                    detail={`采样不足 ${known ? a.summary.insufficient : "—"} · 身份冲突 ${known ? a.summary.ambiguous : "—"}`}
                  />
                </section>
                <p className="text-sm leading-6 text-muted-foreground">
                  本页描述物理 GPU 的窗口采样，不代表作业产出或 Slurm
                  分配效率。低利用率只提供排查线索，需结合工作负载、显存和调度情况判断；不会自动释放资源。窗口整体统计不受下方表格筛选影响。
                </p>
                {known && <EfficiencyTrend analysis={a} />}
                {known && (
                  <section
                    className="panel space-y-4 p-5"
                    aria-label="GPU 效率明细"
                  >
                    <h2 className="font-semibold">逐卡效率与覆盖</h2>
                    <div className="flex flex-wrap items-end gap-3">
                      <Search
                        key={`${id}:${q}`}
                        value={q}
                        onSearch={(v) => update("q", v)}
                      />
                      <Label className="grid gap-2">
                        分析结果
                        <select
                          aria-label="GPU 效率结果筛选"
                          className={selectStyle}
                          value={status}
                          onChange={(e) => update("status", e.target.value)}
                        >
                          <option value="">全部结果</option>
                          {Object.entries(statuses).map(([k, v]) => (
                            <option key={k} value={k}>
                              {v}
                            </option>
                          ))}
                        </select>
                      </Label>
                      <Label className="grid gap-2">
                        排序
                        <select
                          aria-label="GPU 效率排序"
                          className={selectStyle}
                          value={sort}
                          onChange={(e) => update("sort", e.target.value)}
                        >
                          {Object.entries({
                            node: "节点 / UUID",
                            mean: "平均利用率",
                            coverage: "利用率覆盖",
                            low: "低利用率样本占比",
                            memory: "平均显存占用",
                            power: "平均功耗",
                          }).map(([k, v]) => (
                            <option key={k} value={k}>
                              {v}
                            </option>
                          ))}
                        </select>
                      </Label>
                      <select
                        aria-label="GPU 效率排序方向"
                        className={selectStyle}
                        value={order}
                        onChange={(e) => update("order", e.target.value)}
                      >
                        <option value="asc">升序</option>
                        <option value="desc">降序</option>
                      </select>
                    </div>
                    <div className="overflow-x-auto">
                      <table className="w-full text-left text-sm whitespace-nowrap">
                        <thead className="border-b text-muted-foreground">
                          <tr>
                            {[
                              "节点 / GPU",
                              "结果",
                              "利用率均值 / P95 / 峰值",
                              "利用率覆盖",
                              "低利用率样本",
                              "显存占用均值",
                              "功耗均值",
                              "监控",
                            ].map((h) => (
                              <th key={h} className="p-3 font-medium">
                                {h}
                              </th>
                            ))}
                          </tr>
                        </thead>
                        <tbody>
                          {a.items.map((d) => (
                            <tr
                              key={`${d.node}/${d.uuid}`}
                              className="border-b"
                            >
                              <td className="p-3">
                                <p className="font-medium">
                                  {d.node} · {d.index || "—"}
                                </p>
                                <p className="text-xs text-muted-foreground">
                                  {d.model || "未知型号"}
                                </p>
                                <p className="mt-1 font-mono text-xs">
                                  {d.uuid}
                                </p>
                              </td>
                              <td className="p-3">
                                <Badge
                                  variant={
                                    d.status === "low" ? "secondary" : "outline"
                                  }
                                >
                                  {statuses[d.status]}
                                </Badge>
                              </td>
                              <td className="p-3 tabular-nums">
                                {percent(d.utilization.mean)} /{" "}
                                {percent(d.utilization.p95)} /{" "}
                                {percent(d.utilization.peak)}
                              </td>
                              <td className="p-3 tabular-nums">
                                {percent(d.utilization.coveragePercent)}
                                <p className="text-xs text-muted-foreground">
                                  {d.utilization.samples} / {a.expectedSamples}{" "}
                                  点
                                </p>
                              </td>
                              <td className="p-3">
                                {percent(d.lowSamplePercent)}
                                <p className="text-xs text-muted-foreground">
                                  {d.lowSamples} / {d.utilization.samples}{" "}
                                  有效点
                                </p>
                              </td>
                              <td className="p-3">
                                {percent(d.memoryPercent.mean)}
                                <p className="text-xs text-muted-foreground">
                                  {d.memoryPercent.samples} /{" "}
                                  {a.expectedSamples} 点
                                </p>
                              </td>
                              <td className="p-3">
                                {d.powerW.mean === null
                                  ? "—"
                                  : `${number(d.powerW.mean)} W`}
                                <p className="text-xs text-muted-foreground">
                                  {d.powerW.samples} / {a.expectedSamples} 点
                                </p>
                              </td>
                              <td className="p-3">
                                <Link
                                  className="text-primary underline underline-offset-4"
                                  to={`/gpu?${new URLSearchParams({ cluster: id, node: d.node, gpu: d.uuid, range, filterMode: "custom" })}`}
                                >
                                  查看趋势
                                </Link>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                    {!a.items.length && (
                      <p role="status" className="py-6 text-muted-foreground">
                        没有符合条件的 GPU。
                      </p>
                    )}
                    <div className="flex items-center justify-between gap-3 text-sm">
                      <span>
                        共 {data?.totalItems} 张 · 第 {page} 页
                      </span>
                      <div className="flex gap-2">
                        <Button
                          variant="outline"
                          disabled={page <= 1}
                          onClick={() =>
                            setParams((p) => {
                              const n = new URLSearchParams(p)
                              n.set("page", String(page - 1))
                              return n
                            })
                          }
                        >
                          上一页
                        </Button>
                        <Button
                          variant="outline"
                          disabled={page * 20 >= (data?.totalItems || 0)}
                          onClick={() =>
                            setParams((p) => {
                              const n = new URLSearchParams(p)
                              n.set("page", String(page + 1))
                              return n
                            })
                          }
                        >
                          下一页
                        </Button>
                      </div>
                    </div>
                  </section>
                )}
                <p className="text-xs leading-6 text-muted-foreground">
                  窗口 {date(a.start)} 至 {date(a.end)}，每 {a.step}{" "}
                  秒评估一次，每次仅取最近 60 秒的样本。覆盖率为有效点数 /
                  应有点数，6/24
                  小时窗口为稀疏采样，不等于连续时间覆盖或耗电量；缺口不补零。P95
                  使用有效样本的最近秩。显存按同一点的已用 /
                  总量计算，缺失总量时仅在整条 TOTAL 序列未采集时尝试 USED +
                  FREE。忽略 MIG 或未映射序列 {a.ignoredSeries} 条。
                </p>
              </>
            )
          )}
        </>
      )}
    </div>
  )
}
function Card({
  title,
  value,
  detail,
}: {
  title: string
  value: string
  detail: string
}) {
  return (
    <div className="panel space-y-3 p-5">
      <p className="text-sm text-muted-foreground">{title}</p>
      <p className="text-3xl font-semibold tabular-nums">{value}</p>
      <p className="text-xs leading-5 text-muted-foreground">{detail}</p>
    </div>
  )
}
function Search({
  value,
  onSearch,
}: {
  value: string
  onSearch: (v: string) => void
}) {
  const [q, setQ] = useState(value)
  return (
    <form
      className="flex items-end gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        onSearch(q)
      }}
    >
      <Label className="grid gap-2">
        搜索节点、UUID 或型号
        <Input
          aria-label="搜索 GPU 效率"
          value={q}
          maxLength={128}
          onChange={(e) => setQ(e.target.value)}
        />
      </Label>
      <Button variant="outline" type="submit">
        筛选
      </Button>
      <Button
        variant="ghost"
        type="button"
        onClick={() => {
          setQ("")
          onSearch("")
        }}
      >
        清除
      </Button>
    </form>
  )
}
function EfficiencyTrend({ analysis: a }: { analysis: Analysis }) {
  return (
    <section className="panel space-y-4 p-5" aria-label="GPU 窗口整体趋势">
      <h2 className="font-semibold">有效 GPU 样本的平均利用率</h2>
      <p className="text-xs text-muted-foreground">
        每点参与卡数可能变化；未知点断开，不表示零利用率。
      </p>
      <svg
        viewBox="0 0 1000 205"
        className="w-full text-primary"
        role="img"
        aria-label="有效 GPU 的窗口利用率趋势，缺失点断开"
      >
        <title>每个评估点按有效 GPU 等权平均，参与卡数见明细</title>
        {[0, 50, 100].map((v) => (
          <g key={v}>
            <line
              x1="40"
              x2="960"
              y1={180 - v * 1.6}
              y2={180 - v * 1.6}
              stroke="currentColor"
              opacity="0.15"
            />
            <text x="3" y={184 - v * 1.6} fontSize="12" fill="currentColor">
              {v}%
            </text>
          </g>
        ))}
        {trendSegments(a.points).map((s, i) =>
          s.includes(" ") ? (
            <polyline
              key={i}
              points={s}
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            />
          ) : (
            <circle
              key={i}
              cx={s.split(",")[0]}
              cy={s.split(",")[1]}
              r="3"
              fill="currentColor"
            />
          )
        )}
      </svg>
      <div className="flex justify-between text-xs text-muted-foreground">
        <span>{date(a.start)}</span>
        <span>{date(a.end)}</span>
      </div>
      <details>
        <summary className="cursor-pointer text-sm">
          查看窗口采样明细与参与卡数
        </summary>
        <div className="mt-3 max-h-64 overflow-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr>
                <th className="p-2">评估时间</th>
                <th className="p-2">平均利用率</th>
                <th className="p-2">有效 GPU 数</th>
              </tr>
            </thead>
            <tbody>
              {a.points.map((p) => (
                <tr key={p.time} className="border-t">
                  <td className="p-2">{date(p.time)}</td>
                  <td className="p-2">{percent(p.meanUtilization)}</td>
                  <td className="p-2">{p.devices}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </section>
  )
}
