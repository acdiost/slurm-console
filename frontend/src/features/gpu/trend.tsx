import type { History } from "./schema"
import { metrics } from "./schema"
export function GPUTrend({
  history,
  metric,
}: {
  history: History
  metric: string
}) {
  const points = history.series[metric] || [],
    values = points.flatMap((p) => (p.value === null ? [] : [p.value])),
    maxValue = metric === "utilization" ? 100 : Math.max(...values, 1) * 1.1,
    width = 720,
    height = 200,
    pad = 32
  const x = (t: number) =>
      pad +
      ((t - history.start) / (history.end - history.start)) * (width - pad * 2),
    y = (v: number) => height - pad - (v / maxValue) * (height - pad * 2)
  let path = "",
    open = false
  for (const point of points) {
    if (point.value === null) {
      open = false
      continue
    }
    path += `${open ? "L" : "M"}${x(point.time).toFixed(2)},${y(point.value).toFixed(2)} `
    open = true
  }
  return (
    <div className="space-y-2">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={`${metrics[metric]?.label}趋势，${values.length} 个有效点，${points.length - values.length} 个缺失点`}
        className="w-full rounded-md border bg-muted/20"
      >
        <path
          d={`M${pad},${pad}V${height - pad}H${width - pad}`}
          fill="none"
          stroke="currentColor"
          opacity=".25"
        />
        <text
          x={pad + 4}
          y={pad + 4}
          className="fill-muted-foreground text-[10px]"
        >
          {maxValue.toFixed(0)} {metrics[metric]?.unit}
        </text>
        <path
          d={path}
          fill="none"
          stroke="var(--color-primary)"
          strokeWidth="2"
        />
        {points
          .filter((p) => p.value !== null)
          .map((p) => (
            <circle
              key={p.time}
              cx={x(p.time)}
              cy={y(p.value!)}
              r="1.5"
              fill="var(--color-primary)"
            >
              <title>
                {new Date(p.time * 1000).toLocaleTimeString("zh-CN")} ·{" "}
                {p.value} {metrics[metric]?.unit}
              </title>
            </circle>
          ))}
        <text
          x={pad}
          y={height - 8}
          className="fill-muted-foreground text-[10px]"
        >
          {new Date(history.start * 1000).toLocaleTimeString("zh-CN", {
            hour12: false,
          })}
        </text>
        <text
          x={width - pad}
          y={height - 8}
          textAnchor="end"
          className="fill-muted-foreground text-[10px]"
        >
          {new Date(history.end * 1000).toLocaleTimeString("zh-CN", {
            hour12: false,
          })}
        </text>
      </svg>
      <p className="text-xs text-muted-foreground">
        {values.length} 个有效点 · {points.length - values.length}{" "}
        个缺失点。缺失段断开显示；采样步长 {history.step} 秒，仅采用每个时点前
        60 秒内的值。{values.length === 0 ? " 当前窗口无有效数据。" : ""}
      </p>
    </div>
  )
}
