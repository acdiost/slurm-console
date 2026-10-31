import { useState } from "react"
import { Button } from "@/components/ui/button"
import {
  resource,
  percentage,
  percent,
  memory,
  number,
  date,
  type History,
  type Resource,
} from "./schema"
export function segments(
  history: History,
  key: Resource,
  metric: "allocated" | "idle" | "unavailable" = "allocated"
) {
  const groups: { x: number; y: number }[][] = []
  let group: { x: number; y: number }[] = []
  history.points.forEach((p, i) => {
    const r = p.frame ? resource(p.frame, key) : null,
      v = r ? percentage(r[metric], r.total) : null
    if (v === null) {
      if (group.length) groups.push(group)
      group = []
    } else
      group.push({
        x: 50 + (900 * (i + 0.5)) / history.points.length,
        y: 200 - v * 1.6,
      })
  })
  if (group.length) groups.push(group)
  return groups
}
export function CapacityTrend({
  history,
  selected,
}: {
  history: History
  selected: Resource
}) {
  const [page, setPage] = useState(1),
    groups = segments(history, selected),
    stat =
      selected === "cpu"
        ? history.cpus
        : selected === "memory"
          ? history.memoryMiB
          : history.gpus,
    points = [...history.points].reverse(),
    items = points.slice((page - 1) * 20, page * 20)
  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-3">
        {[
          ["容量加权采样分配比", percent(stat.weightedAllocatedPercent)],
          ["采样峰值分配比", percent(stat.peakAllocatedPercent)],
          [
            "采样最小未分配可用量",
            selected === "memory"
              ? memory(stat.minimumIdle)
              : number(stat.minimumIdle),
          ],
        ].map(([label, value]) => (
          <div key={label} className="rounded-lg border bg-muted/20 p-4">
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className="mt-2 text-xl font-semibold tabular-nums">{value}</p>
          </div>
        ))}
      </div>
      <div className="rounded-lg border bg-background p-3">
        <div className="flex flex-wrap justify-end gap-4 text-xs">
          <span className="text-primary">● 已分配</span>
          <span className="text-emerald-600">● 节点余量</span>
          <span className="text-amber-600">● 不可用</span>
        </div>
        <svg
          role="img"
          aria-label={`资源分配趋势，${history.availableSamples}/${history.expectedSamples} 个有效样本，缺口留空`}
          viewBox="0 0 1000 240"
          className="h-60 w-full"
        >
          <title>
            Slurm 调度分配、节点余量和不可用容量占比；每 5
            分钟一次采样，缺失区间断开。
          </title>
          {[0, 25, 50, 75, 100].map((v) => (
            <g key={v}>
              <line
                x1="50"
                x2="950"
                y1={200 - v * 1.6}
                y2={200 - v * 1.6}
                className="stroke-border"
              />
              <text
                x="42"
                y={204 - v * 1.6}
                textAnchor="end"
                className="fill-muted-foreground text-[12px]"
              >
                {v}%
              </text>
            </g>
          ))}
          {(["allocated", "idle", "unavailable"] as const).flatMap((metric) =>
            segments(history, selected, metric).map((g, i) =>
              g.length === 1 ? (
                <circle
                  key={`${metric}:${i}`}
                  cx={g[0].x}
                  cy={g[0].y}
                  r="3"
                  className={
                    metric === "allocated"
                      ? "fill-primary"
                      : metric === "idle"
                        ? "fill-emerald-500"
                        : "fill-amber-500"
                  }
                />
              ) : (
                <path
                  key={`${metric}:${i}`}
                  d={g
                    .map(
                      (p, j) =>
                        `${j ? "L" : "M"}${p.x.toFixed(2)},${p.y.toFixed(2)}`
                    )
                    .join(" ")}
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  className={
                    metric === "allocated"
                      ? "text-primary"
                      : metric === "idle"
                        ? "text-emerald-500"
                        : "text-amber-500"
                  }
                />
              )
            )
          )}
          <text x="50" y="225" className="fill-muted-foreground text-[12px]">
            {new Date(history.start * 1000).toLocaleString("zh-CN", {
              month: "2-digit",
              day: "2-digit",
              hour: "2-digit",
              minute: "2-digit",
              hour12: false,
            })}
          </text>
          <text
            x="950"
            y="225"
            textAnchor="end"
            className="fill-muted-foreground text-[12px]"
          >
            {new Date(history.end * 1000).toLocaleString("zh-CN", {
              month: "2-digit",
              day: "2-digit",
              hour: "2-digit",
              minute: "2-digit",
              hour12: false,
            })}
          </text>
        </svg>
        {!groups.length && (
          <p className="pb-3 text-center text-sm text-muted-foreground">
            该窗口暂无可计算分配比例的样本。
          </p>
        )}
      </div>
      <p className="text-xs leading-6 text-muted-foreground">
        覆盖 {history.availableSamples}/{history.expectedSamples} 个完整 5
        分钟窗口 · 该资源已知样本 {stat.knownSamples}{" "}
        个。历史从服务上线后积累，当前未结束的窗口稍后纳入；断点保持为空，采样比例按配置容量加权。
      </p>
      <details>
        <summary className="cursor-pointer text-sm text-primary">
          查看采样明细
        </summary>
        <div className="mt-3 overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b text-muted-foreground">
                <th className="p-3">窗口起点</th>
                <th className="p-3">实际观测</th>
                <th className="p-3">配置量</th>
                <th className="p-3">已分配</th>
                <th className="p-3">分配比</th>
              </tr>
            </thead>
            <tbody>
              {items.map((p) => {
                const r = p.frame ? resource(p.frame, selected) : null
                const format = selected === "memory" ? memory : number
                return (
                  <tr key={p.bucket} className="border-b">
                    <td className="p-3 whitespace-nowrap">{date(p.bucket)}</td>
                    <td className="p-3 whitespace-nowrap">
                      {p.frame ? date(p.observedAt) : "缺失"}
                    </td>
                    <td className="p-3 tabular-nums">{format(r?.total)}</td>
                    <td className="p-3 tabular-nums">{format(r?.allocated)}</td>
                    <td className="p-3 tabular-nums">
                      {r ? percent(percentage(r.allocated, r.total)) : "—"}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
        <div className="mt-3 flex items-center justify-end gap-3 text-sm">
          <span>第 {page} 页</span>
          <Button
            size="sm"
            variant="outline"
            disabled={page <= 1}
            onClick={() => setPage((v) => v - 1)}
          >
            上一页采样
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={page * 20 >= points.length}
            onClick={() => setPage((v) => v + 1)}
          >
            下一页采样
          </Button>
        </div>
      </details>
    </div>
  )
}
