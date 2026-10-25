import {
  Activity,
  CheckCircle2,
  CircleHelp,
  Cpu,
  Gauge,
  HardDrive,
  Layers3,
  Server,
  ShieldOff,
  TriangleAlert,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { resolveStatus } from "@/components/slurm/status"
import {
  memory,
  timeLimit,
  timestamp,
  type Overview,
  type Resource,
  type Section,
} from "./schema"

export function SourceState({ section }: { section: Section }) {
  const Icon = section.status === "forbidden" ? ShieldOff : TriangleAlert
  return (
    <div className="flex items-start gap-2 py-5 text-xs leading-6 text-muted-foreground">
      <Icon className="mt-1 size-4 shrink-0" />
      <span>{section.message || "暂时无法读取数据，请刷新重试。"}</span>
    </div>
  )
}
export function SourceTime({ section }: { section: Section }) {
  return section.status === "ok" ? (
    <p className="flex items-center gap-1.5 text-2xs text-muted-foreground">
      <span className="size-1.5 rounded-full bg-primary/60" />
      {timestamp(section.fetchedAt)} · {section.cached ? "缓存数据" : "已更新"}
    </p>
  ) : null
}
function Capacity({
  label,
  resource,
  format = String,
}: {
  label: string
  resource: Resource
  format?: (value: number) => string
}) {
  const values = [
    { label: "已分配", value: resource.allocated, color: "bg-[var(--blue-9)]" },
    { label: "空闲", value: resource.idle, color: "bg-[var(--grass-9)]" },
    {
      label: "不可用/预留",
      value: resource.unavailable,
      color: "bg-[var(--amber-9)]",
    },
  ]
  return (
    <>
      <div
        role="img"
        aria-label={`${label}：总量 ${format(resource.total)}，已分配 ${format(resource.allocated)}，空闲 ${format(resource.idle)}，不可用或预留 ${format(resource.unavailable)}`}
        className="my-5 flex h-1.5 overflow-hidden rounded-full bg-muted"
      >
        {values.map((value) => (
          <span
            key={value.label}
            className={value.color}
            style={{
              width: `${resource.total ? (value.value / resource.total) * 100 : 0}%`,
            }}
          />
        ))}
      </div>
      <div className="space-y-2">
        {values.map((value) => (
          <div
            key={value.label}
            className="flex items-center justify-between gap-3 text-2xs"
          >
            <span className="flex items-center gap-2 text-muted-foreground">
              <span className={`size-1.5 rounded-full ${value.color}`} />
              {value.label}
            </span>
            <span className="font-mono tabular-nums">
              {format(value.value)}
            </span>
          </div>
        ))}
      </div>
    </>
  )
}
export function ResourceCards({ section }: { section: Overview["nodes"] }) {
  const nodes = section.status === "ok" ? section.item : null
  const gpu = nodes?.gpus
  const cards = [
    {
      title: "CPU 容量",
      icon: Cpu,
      value: nodes?.cpus.total.toLocaleString("zh-CN") ?? "—",
      unit: "CPU",
      resource: nodes?.cpus,
    },
    {
      title: "内存容量",
      icon: HardDrive,
      value: nodes ? memory(nodes.memoryMiB.total).replace(" GiB", "") : "—",
      unit: "GiB",
      resource: nodes?.memoryMiB,
      format: memory,
    },
    {
      title: "GPU 调度资源",
      icon: Gauge,
      value: gpu?.total?.toLocaleString("zh-CN") ?? "—",
      unit: "GPU",
      resource:
        gpu?.total != null &&
        gpu.allocated != null &&
        gpu.idle != null &&
        gpu.unavailable != null
          ? {
              total: gpu.total,
              allocated: gpu.allocated,
              idle: gpu.idle,
              unavailable: gpu.unavailable,
            }
          : undefined,
    },
    {
      title: "计算节点",
      icon: Server,
      value: nodes?.total.toLocaleString("zh-CN") ?? "—",
      unit: "节点",
    },
  ]
  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {cards.map(({ title, icon: Icon, value, unit, resource, format }) => (
        <section key={title} className="panel p-5">
          <div className="flex items-center justify-between gap-3">
            <h2 className="text-xs font-medium text-muted-foreground">
              {title}
            </h2>
            <Icon className="size-4 text-muted-foreground" />
          </div>
          <p className="mt-5 flex items-baseline gap-2">
            <span className="font-mono text-3xl font-semibold tracking-tight tabular-nums">
              {value}
            </span>
            <span className="text-2xs text-muted-foreground">{unit}</span>
          </p>
          {!nodes ? (
            <SourceState section={section} />
          ) : title === "计算节点" ? (
            <>
              <div className="my-5 flex items-center justify-between rounded-md border bg-muted/20 px-3 py-2 text-2xs">
                <span>不可用节点</span>
                <span
                  className={
                    nodes.unavailable
                      ? "font-mono text-[var(--amber-11)]"
                      : "font-mono"
                  }
                >
                  {nodes.unavailable}
                </span>
              </div>
              <div className="grid grid-cols-3 gap-2 text-center">
                {[
                  ["空闲", nodes.idle],
                  ["分配", nodes.allocated],
                  ["混合", nodes.mixed],
                ].map(([label, count]) => (
                  <div key={label}>
                    <p className="font-mono text-sm tabular-nums">{count}</p>
                    <p className="mt-1 text-2xs text-muted-foreground">
                      {label}
                    </p>
                  </div>
                ))}
              </div>
            </>
          ) : title === "GPU 调度资源" && gpu?.total === 0 ? (
            <p className="my-5 text-2xs leading-6 text-muted-foreground">
              当前节点未配置 GPU 调度资源。
            </p>
          ) : resource ? (
            <Capacity label={title} resource={resource} format={format} />
          ) : (
            <p className="my-5 text-2xs leading-6 text-[var(--amber-11)]">
              {gpu?.unknownNodes} 个节点的 GPU 数量或分配信息无法确认。
            </p>
          )}
        </section>
      ))}
    </div>
  )
}
export function ControllerPanel({ section }: { section: Overview["health"] }) {
  const health = section.status === "ok" ? section.item : null
  const connected = health?.status === "connected"
  return (
    <section className="panel flex flex-wrap items-center justify-between gap-5 p-5">
      <div className="flex items-start gap-3">
        <div
          className={`mt-0.5 rounded-lg border p-2.5 ${connected ? "border-[var(--grass-6)] bg-[var(--grass-3)] text-[var(--grass-11)]" : "border-border bg-muted text-muted-foreground"}`}
        >
          {connected ? (
            <CheckCircle2 className="size-5" />
          ) : (
            <Activity className="size-5" />
          )}
        </div>
        <div>
          <h2 className="text-sm font-semibold">
            {health
              ? connected
                ? "控制器在线"
                : "控制器不可达"
              : "控制器状态不可用"}
          </h2>
          {health ? (
            <p className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1 font-mono text-2xs text-muted-foreground">
              <span>RESTD {health.serverVersion}</span>
              <span>API {health.apiVersion}</span>
              <span>{health.cluster}</span>
            </p>
          ) : (
            <SourceState section={section} />
          )}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-4">
        {health?.controllers.map((controller) => (
          <div
            key={`${controller.hostname}-${controller.role}`}
            className="text-right"
          >
            <p className="flex items-center gap-2 text-2xs">
              <span
                className={`size-1.5 rounded-full ${controller.state === "UP" ? "bg-[var(--grass-9)]" : "bg-[var(--red-9)]"}`}
              />
              <span className="font-mono">{controller.hostname}</span>
              <Badge variant="outline" className="font-mono font-normal">
                {controller.state}
              </Badge>
            </p>
            <p className="mt-1 text-2xs text-muted-foreground">
              {controller.role === "primary"
                ? "主控制器"
                : controller.role === "backup"
                  ? "备用控制器"
                  : controller.role}
            </p>
          </div>
        ))}
        <SourceTime section={section} />
      </div>
    </section>
  )
}
export function JobsPanel({ section }: { section: Overview["jobs"] }) {
  const jobs = section.status === "ok" ? section.item : null
  return (
    <section className="panel">
      <div className="panel-heading">
        <h2 className="flex items-center gap-2">
          <Layers3 className="size-4 text-primary" />
          作业状态
        </h2>
        <SourceTime section={section} />
      </div>
      <div className="p-5">
        {jobs ? (
          <>
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
              {[
                {
                  label: "当前作业",
                  value: jobs.total,
                  color: "text-foreground",
                },
                {
                  label: "运行中",
                  value: jobs.running,
                  color: "text-[var(--grass-11)]",
                },
                {
                  label: "排队中",
                  value: jobs.pending,
                  color: "text-[var(--amber-11)]",
                },
                {
                  label: "失败/异常结束",
                  value: jobs.failed,
                  color: "text-[var(--red-11)]",
                },
              ].map(({ label, value, color }) => (
                <div key={label} className="rounded-md border bg-muted/15 p-4">
                  <p className="text-2xs text-muted-foreground">{label}</p>
                  <p
                    className={`mt-3 font-mono text-2xl font-semibold tabular-nums ${color}`}
                  >
                    {value}
                  </p>
                </div>
              ))}
            </div>
            <div className="mt-5 flex flex-wrap gap-x-6 gap-y-2 text-2xs text-muted-foreground">
              {[
                ["收尾中", jobs.completing],
                ["已完成", jobs.completed],
                ["已取消", jobs.cancelled],
                ["其他状态", jobs.other],
              ].map(([label, count]) => (
                <span key={label}>
                  {label}{" "}
                  <span className="ml-1 font-mono text-foreground">
                    {count}
                  </span>
                </span>
              ))}
            </div>
            {jobs.total === 0 && (
              <p className="mt-5 text-2xs text-muted-foreground">
                当前没有可见作业。
              </p>
            )}
            <p className="mt-5 border-t pt-4 text-2xs leading-6 text-muted-foreground">
              统计范围为控制器当前保留的可见作业，包含已结束记录，不代表历史记账。
            </p>
          </>
        ) : (
          <SourceState section={section} />
        )}
      </div>
    </section>
  )
}
export function NodeStatesPanel({ section }: { section: Overview["nodes"] }) {
  const nodes = section.status === "ok" ? section.item : null
  return (
    <section className="panel">
      <div className="panel-heading">
        <h2 className="flex items-center gap-2">
          <Server className="size-4 text-primary" />
          节点状态
        </h2>
        <SourceTime section={section} />
      </div>
      <div className="space-y-4 p-5">
        {nodes ? (
          <>
            {nodes.states.map(({ state, count }) => (
              <div key={state} className="space-y-2">
                <div className="flex items-center justify-between gap-3">
                  <SlurmStatusBadge state={state} kind="node" />
                  <span className="font-mono text-2xs tabular-nums">
                    {count} / {nodes.total}
                  </span>
                </div>
                <Progress
                  aria-label={`${state} 节点比例`}
                  value={nodes.total ? (count / nodes.total) * 100 : 0}
                  className="h-1"
                />
              </div>
            ))}
            {nodes.total === 0 && (
              <p className="text-xs text-muted-foreground">
                当前没有可见节点。
              </p>
            )}
            <p className="text-2xs leading-6 text-muted-foreground">
              状态标记可能重叠；容量按节点去重汇总。
            </p>
          </>
        ) : (
          <SourceState section={section} />
        )}
      </div>
    </section>
  )
}
export function PartitionsPanel({
  section,
}: {
  section: Overview["partitions"]
}) {
  const partitions = section.status === "ok" ? section.item : null
  return (
    <section className="panel">
      <div className="panel-heading">
        <h2 className="text-xs">
          分区概况
          {partitions && (
            <Badge variant="secondary" className="ml-2">
              {partitions.up} / {partitions.total} UP
            </Badge>
          )}
        </h2>
        <SourceTime section={section} />
      </div>
      {partitions ? (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>分区</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">节点</TableHead>
                <TableHead className="text-right">CPU</TableHead>
                <TableHead className="text-right">最长运行</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {partitions.items.map((partition) => (
                <TableRow key={partition.name}>
                  <TableCell className="font-mono">{partition.name}</TableCell>
                  <TableCell>
                    <Badge
                      variant="outline"
                      data-tone={
                        partition.states.includes("UP") ? "success" : "warning"
                      }
                      className="status-badge font-mono text-2xs font-normal"
                    >
                      {partition.states.join(" / ")}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right font-mono">
                    {partition.nodeCount}
                  </TableCell>
                  <TableCell className="text-right font-mono">
                    {partition.cpus}
                  </TableCell>
                  <TableCell className="text-right text-2xs">
                    {timeLimit(partition.maxTimeMinutes)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!partitions.total && (
            <p className="p-5 text-xs text-muted-foreground">
              当前没有可见分区。
            </p>
          )}
          <p className="border-t px-5 py-3 text-2xs leading-6 text-muted-foreground">
            {partitions.total > partitions.items.length &&
              `显示前 ${partitions.items.length} / ${partitions.total} 个分区。`}
            分区间可能共享节点，容量不能直接相加。
          </p>
        </>
      ) : (
        <div className="px-5">
          <SourceState section={section} />
        </div>
      )}
    </section>
  )
}
export function AttentionPanel({ overview }: { overview: Overview }) {
  const failures = [
    { name: "控制器", section: overview.health },
    { name: "作业", section: overview.jobs },
    { name: "节点", section: overview.nodes },
    { name: "分区", section: overview.partitions },
  ].filter(({ section }) => section.status === "error")
  const hasData = [
    overview.health,
    overview.jobs,
    overview.nodes,
    overview.partitions,
  ].some((section) => section.status === "ok")
  const nodes = overview.nodes.status === "ok" ? overview.nodes.item : null
  const jobs = overview.jobs.status === "ok" ? overview.jobs.item : null
  const controllerDown =
    overview.health.status === "ok" &&
    overview.health.item.status === "controller_down"
  const hasAttention =
    failures.length > 0 ||
    !!nodes?.unavailable ||
    !!nodes?.gpus.unknownNodes ||
    !!jobs?.failed ||
    controllerDown
  return (
    <section className="panel">
      <div className="panel-heading">
        <h2 className="flex items-center gap-2">
          <TriangleAlert className="size-4 text-[var(--amber-11)]" />
          需关注
        </h2>
        <Badge variant="outline">当前快照</Badge>
      </div>
      <div className="divide-y px-5">
        {failures.map(({ name, section }) => (
          <div key={name} className="flex items-start gap-3 py-4 text-xs">
            <TriangleAlert className="mt-0.5 size-4 shrink-0 text-[var(--amber-11)]" />
            <div>
              <p className="font-medium">{name}数据暂不可用</p>
              <p className="mt-1 text-2xs leading-6 text-muted-foreground">
                {section.message}
                {hasData ? "其他数据仍可查看。" : "可稍后刷新重试。"}
              </p>
            </div>
          </div>
        ))}
        {controllerDown && (
          <p className="py-4 text-xs leading-6 text-[var(--red-11)]">
            REST 服务可达，但 Slurm 控制器未响应。
          </p>
        )}
        {nodes?.issues.map((node) => (
          <div
            key={node.name}
            className="flex items-start justify-between gap-4 py-4"
          >
            <div className="min-w-0">
              <p className="font-mono text-xs font-medium break-all">
                {node.name}
              </p>
              <p className="mt-1 text-2xs leading-6 break-words text-muted-foreground">
                {node.reason || "节点当前不具备常规调度条件。"}
              </p>
            </div>
            <div className="flex shrink-0 flex-wrap justify-end gap-1">
              {node.states.slice(0, 3).map((state) => (
                <Badge
                  key={state}
                  variant="outline"
                  data-tone={resolveStatus(state, "node").tone}
                  className="status-badge font-mono text-2xs"
                >
                  {state}
                </Badge>
              ))}
            </div>
          </div>
        ))}
        {nodes && nodes.unavailable > nodes.issues.length && (
          <p className="py-3 text-2xs text-muted-foreground">
            另有 {nodes.unavailable - nodes.issues.length} 个不可用节点。
          </p>
        )}
        {!!nodes?.gpus.unknownNodes && (
          <p className="py-4 text-2xs leading-6 text-[var(--amber-11)]">
            {nodes.gpus.unknownNodes} 个节点的 GPU
            配置或分配信息无法确认，相关合计显示为未知。
          </p>
        )}
        {!!jobs?.failed && (
          <p className="py-4 text-xs leading-6">
            当前保留记录中有{" "}
            <span className="font-mono text-[var(--red-11)]">
              {jobs.failed}
            </span>{" "}
            个失败或异常结束的作业。
          </p>
        )}
        {!hasAttention && (
          <div className="flex items-center gap-2 py-6 text-xs text-muted-foreground">
            <CircleHelp className="size-4" />
            当前可见数据未发现需关注项。
          </div>
        )}
      </div>
    </section>
  )
}
