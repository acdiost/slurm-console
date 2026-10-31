import { DiagnosticPanel } from "@/features/diagnostics/panel"
import { useState } from "react"
import { useSearchParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { RefreshCw, History } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
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
  TableCell,
  TableBody,
} from "@/components/ui/table"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import {
  ClusterPicker,
  useAccessibleClusters,
  pageNumber,
} from "@/features/clusters/cluster-picker"
import {
  historyPageSchema,
  historyDetailSchema,
  states,
  errorMessage,
  cpu,
  resourceText,
  localDate,
  type HistoryJob,
} from "@/features/history/schema"
import { quantity, jobTime } from "@/features/jobs/schema"
import { timestamp } from "@/features/overview/schema"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
const fields = ["user", "account", "partition", "qos", "jobId", "q"] as const
const names = {
  user: "用户",
  account: "账户",
  partition: "分区",
  qos: "QOS",
  jobId: "Job ID",
  q: "名称、节点或文本",
}
function Filters({
  params,
  start,
  end,
  apply,
}: {
  params: URLSearchParams
  start: number
  end: number
  apply: (v: Record<string, string | null>) => void
}) {
  const [error, setError] = useState("")
  return (
    <form
      className="panel space-y-4 p-4"
      aria-label="历史作业筛选"
      onSubmit={(e) => {
        e.preventDefault()
        const f = new FormData(e.currentTarget),
          a = Math.floor(new Date(String(f.get("from"))).valueOf() / 1000),
          b = Math.floor(new Date(String(f.get("to"))).valueOf() / 1000)
        if (
          !Number.isFinite(a) ||
          !Number.isFinite(b) ||
          a <= 0 ||
          b <= a ||
          b - a > 31 * 86400
        ) {
          setError("请选择最多 31 天的有效提交时间范围。")
          return
        }
        setError("")
        const values: Record<string, string | null> = {
          start: String(a),
          end: String(b),
          page: "1",
          record: null,
        }
        for (const k of [...fields, "state"])
          values[k] = String(f.get(k) || "").trim()
        apply(values)
      }}
    >
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <div className="space-y-1.5">
          <Label htmlFor="history-from">提交时间起（本地时间）</Label>
          <Input
            id="history-from"
            name="from"
            type="datetime-local"
            required
            defaultValue={localDate(start)}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="history-to">提交时间止（不含）</Label>
          <Input
            id="history-to"
            name="to"
            type="datetime-local"
            required
            defaultValue={localDate(end)}
          />
        </div>
        {fields.map((k) => (
          <div className="space-y-1.5" key={k}>
            <Label htmlFor={`history-${k}`}>{names[k]}</Label>
            <Input
              id={`history-${k}`}
              name={k}
              maxLength={k === "q" ? 128 : 64}
              defaultValue={params.get(k) || ""}
              placeholder={k === "q" ? "包含匹配" : "单个精确值"}
            />
          </div>
        ))}
        <div className="space-y-1.5">
          <Label htmlFor="history-state">作业状态</Label>
          <select
            id="history-state"
            name="state"
            className="h-10 w-full rounded-md border bg-background px-3 text-sm"
            defaultValue={params.get("state") || ""}
          >
            <option value="">全部状态</option>
            {states.map((s) => (
              <option key={s}>{s}</option>
            ))}
          </select>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit">查询历史</Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            const now = Math.floor(Date.now() / 60000) * 60
            apply({
              start: String(now - 86400),
              end: String(now),
              ...Object.fromEntries(
                [...fields, "state", "record"].map((k) => [k, null])
              ),
              page: "1",
            })
          }}
        >
          最近 24 小时
        </Button>
        <p className="text-xs text-muted-foreground">
          按提交时间筛选，单次最多 31 天；不会自动轮询。
        </p>
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </form>
  )
}
function Detail({
  job,
  clusterId,
  start,
  end,
}: {
  job: HistoryJob
  clusterId: string
  start: number
  end: number
}) {
  const details = [
    ["Slurm 集群", job.cluster],
    ["用户 / 账户", `${job.user} / ${job.account}`],
    ["分区 / QOS", `${job.partition || "—"} / ${job.qos || "—"}`],
    ["节点", job.nodes || "未分配"],
    ["提交时间", jobTime(job.submitTime)],
    ["开始时间", jobTime(job.startTime)],
    ["结束时间", jobTime(job.endTime)],
    ["已运行时长", quantity(job.elapsedSeconds, " 秒")],
    ["挂起时长", quantity(job.suspendedSeconds, " 秒")],
    ["时间限制", quantity(job.timeLimitMinutes, " 分钟")],
    ["总 CPU 记账时间", cpu(job.totalCpuSeconds)],
    ["状态原因", job.reason || "未记录"],
  ]
  return (
    <div className="space-y-6 px-5 pb-6">
      <DiagnosticPanel
        key={`${clusterId}:${job.key}:${start}:${end}`}
        clusterId={clusterId}
        jobId={job.id}
        source="accounting"
        recordKey={job.key}
        start={start}
        end={end}
      />
      <div className="flex flex-wrap gap-2">
        {job.states.map((s) => (
          <SlurmStatusBadge key={s} state={s} />
        ))}
      </div>
      <dl className="grid gap-4 sm:grid-cols-2">
        {details.map(([k, v]) => (
          <div className="rounded-md border p-3" key={k}>
            <dt className="text-xs text-muted-foreground">{k}</dt>
            <dd className="mt-1 text-sm break-all">{v}</dd>
          </div>
        ))}
      </dl>
      <div className="space-y-2 text-sm">
        <p>
          数组作业 / Task：{quantity(job.arrayJobId)} /{" "}
          {quantity(job.arrayTaskId)}
        </p>
        <p>
          异构作业 / Offset：{quantity(job.hetJobId)} /{" "}
          {quantity(job.hetJobOffset)}
        </p>
        <p>
          退出状态：{job.exit.status.join(", ") || "未知"} · 退出码{" "}
          {quantity(job.exit.code)} · 信号 {quantity(job.exit.signal)}{" "}
          {job.exit.signalName}
        </p>
        <p>
          步骤派生退出码：{quantity(job.derivedExit.code)} ·{" "}
          {job.derivedExit.status.join(", ") || "未知"}
        </p>
      </div>
      <section className="space-y-2">
        <h3 className="font-medium">资源申请与分配</h3>
        <p className="text-sm break-all">
          申请：{resourceText(job.requestedTres)}
        </p>
        <p className="text-sm break-all">
          分配：{resourceText(job.allocatedTres)}
        </p>
        <p className="text-xs text-muted-foreground">
          mem 分配单位为 MiB；分配量不代表实际消耗。GPU TRES
          是记账资源，不等同实际 GPU 利用率。
        </p>
      </section>
      <section className="space-y-3">
        <h3 className="font-medium">作业步骤（{job.steps.length}）</h3>
        <p className="text-xs text-muted-foreground">
          步骤单独展示，不再次计入作业数量或累计时间。CPU
          时间来自采集配置，零值不表示已验证无资源消耗。
        </p>
        {!job.steps.length && (
          <p className="text-sm text-muted-foreground">
            未记录步骤，作业可能未启动或步骤数据不可用。
          </p>
        )}
        {job.steps.map((s) => (
          <article
            key={s.id}
            className="space-y-2 rounded-lg border p-4 text-sm"
          >
            <h4 className="font-mono font-medium">
              {s.id} · {s.name}
            </h4>
            <div className="flex gap-2">
              {s.states.map((state) => (
                <SlurmStatusBadge state={state} key={state} />
              ))}
            </div>
            <p>
              节点：{s.nodes || "—"} · 任务数：{quantity(s.tasks)}
            </p>
            <p>
              {jobTime(s.startTime)} — {jobTime(s.endTime)}
            </p>
            <p>
              时长：{quantity(s.elapsedSeconds, " 秒")} ·{" "}
              {cpu(s.totalCpuSeconds)}
            </p>
            <p>
              退出码 {quantity(s.exit.code)} · 信号 {quantity(s.exit.signal)} ·{" "}
              {s.exit.status.join(", ") || "未知"}
            </p>
            <p className="break-all">分配：{resourceText(s.allocatedTres)}</p>
            <p className="break-all">
              消耗平均：{resourceText(s.consumedAverage, true)}
            </p>
            <p className="break-all">
              消耗总值：{resourceText(s.consumedTotal, true)}
            </p>
            <p className="text-xs text-muted-foreground">
              消耗 TRES
              为来源原始计量值，单位依赖采集项；不跨指标汇总或换算为实时利用率。
            </p>
          </article>
        ))}
      </section>
      <p className="font-mono text-xs break-all text-muted-foreground">
        记录标识 {job.key}
      </p>
    </div>
  )
}
export function HistoryPage() {
  const [params, setParams] = useSearchParams(),
    selection = useAccessibleClusters(),
    cid = selection.id,
    user = useAuth((s) => s.user)
  const [defaults] = useState(() => {
    const end = Math.floor(Date.now() / 60000) * 60
    return { start: end - 86400, end }
  })
  const start = Number(params.get("start") || defaults.start),
    end = Number(params.get("end") || defaults.end),
    page = pageNumber(params.get("page")),
    record = params.get("record") || ""
  const allowed =
    !!user &&
    (user.access.platformPermissions.includes("job_history.read") ||
      user.access.clusters.some(
        (c) =>
          c.cluster.id === cid && c.permissions.includes("job_history.read")
      ))
  const q = new URLSearchParams({
    start: String(start),
    end: String(end),
    page: String(page),
    perPage: "20",
    sort: params.get("sort") || "submitTime",
    order: params.get("order") || "desc",
  })
  for (const k of [...fields, "state"])
    if (params.get(k)) q.set(k, params.get(k)!)
  const query = q.toString(),
    base = `/clusters/${encodeURIComponent(cid)}/job-history`
  const list = useQuery({
    queryKey: ["job-history", cid, query],
    enabled: !!cid && allowed,
    queryFn: async ({ signal }) =>
      historyPageSchema.parse(await api(`${base}?${query}`, { signal })),
    retry: false,
    staleTime: 30000,
    refetchOnWindowFocus: false,
  })
  const detail = useQuery({
    queryKey: ["job-history-detail", cid, record, query],
    enabled: !!cid && allowed && !!record,
    queryFn: async ({ signal }) =>
      historyDetailSchema.parse(
        await api(`${base}/${encodeURIComponent(record)}?${query}`, { signal })
      ),
    retry: false,
    staleTime: 30000,
    refetchOnWindowFocus: false,
  })
  const data = allowed && !list.isError ? list.data : undefined
  function update(changes: Record<string, string | null>) {
    setParams((p) => {
      const n = new URLSearchParams(p)
      n.set("cluster", cid)
      n.set("start", String(start))
      n.set("end", String(end))
      for (const [k, v] of Object.entries(changes))
        if (v) n.set(k, v)
        else n.delete(k)
      return n
    })
  }
  return (
    <div className="space-y-6 p-5 lg:p-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="eyebrow">JOB ACCOUNTING</p>
          <h1 className="mt-2 flex items-center gap-3 text-3xl font-semibold">
            <History className="size-7 text-primary" />
            历史作业与记账
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            查询 slurmdbd 中的作业记录、退出结果和资源记账。
          </p>
        </div>
        <Button
          variant="outline"
          disabled={!allowed || list.isFetching}
          onClick={() => {
            void list.refetch()
            if (record) void detail.refetch()
          }}
        >
          <RefreshCw />
          刷新记账
        </Button>
      </header>
      <ClusterPicker selection={selection} label="历史作业" />
      {!!cid && !allowed ? (
        <p role="alert" className="panel p-5">
          你没有该集群的历史作业读取权限。
        </p>
      ) : !cid ? (
        <p className="panel p-5">
          {selection.inventory.isError
            ? errorMessage(selection.inventory.error)
            : "请选择有权限的集群。"}
        </p>
      ) : (
        <>
          <Filters
            key={`${cid}:${params.toString()}`}
            params={params}
            start={start}
            end={end}
            apply={update}
          />
          <section
            className="panel space-y-2 p-4 text-sm"
            aria-label="记账来源"
          >
            <div className="flex flex-wrap items-center gap-3">
              <Badge variant="outline">SlurmDBD</Badge>
              <span>
                提交时间范围：{jobTime({ value: start, infinite: false })} —{" "}
                {jobTime({ value: end, infinite: false })}
              </span>
            </div>
            <p className="text-muted-foreground">
              数据受记账保留策略及固定连接身份可见范围限制；可能包含尚未结束的作业，与控制器状态存在同步延迟。
            </p>
            {data && (
              <p className="text-muted-foreground">
                Slurm 集群 {data.cluster} · 查询 {timestamp(data.fetchedAt)} ·{" "}
                {data.cached ? "缓存" : "实时读取"} · {data.totalItems}{" "}
                条匹配作业记录
              </p>
            )}
          </section>
          <div className="flex flex-wrap items-center gap-3">
            <Label htmlFor="history-sort">排序</Label>
            <select
              id="history-sort"
              className="h-10 rounded-md border bg-background px-3 text-sm"
              value={params.get("sort") || "submitTime"}
              onChange={(e) =>
                update({ sort: e.target.value, page: "1", record: null })
              }
            >
              {[
                ["submitTime", "提交时间"],
                ["startTime", "开始时间"],
                ["endTime", "结束时间"],
                ["elapsedSeconds", "已运行时长"],
                ["id", "Job ID"],
              ].map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </select>
            <Button
              variant="ghost"
              onClick={() =>
                update({
                  order: params.get("order") === "asc" ? "desc" : "asc",
                  page: "1",
                })
              }
            >
              排序：{params.get("order") === "asc" ? "升序" : "降序"}
            </Button>
          </div>
          {list.isPending ? (
            <p role="status" className="panel p-8">
              正在查询记账…
            </p>
          ) : list.isError ? (
            <p role="alert" className="panel p-5 text-destructive">
              {errorMessage(list.error)}
            </p>
          ) : (
            data && (
              <section className="overflow-hidden rounded-lg border bg-card">
                <Table aria-label="历史作业记录">
                  <TableHeader>
                    <TableRow>
                      {[
                        "作业 / 记录",
                        "用户 / 账户",
                        "状态 / 退出码",
                        "提交 / 结束时间",
                        "时长 / CPU 记账",
                        "分配资源",
                      ].map((h) => (
                        <TableHead key={h}>{h}</TableHead>
                      ))}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.items.map((j) => (
                      <TableRow key={j.key}>
                        <TableCell>
                          <Button
                            variant="link"
                            className="px-0 font-mono"
                            onClick={() => update({ record: j.key })}
                          >
                            #{j.id}
                          </Button>
                          <p className="max-w-48 truncate" title={j.name}>
                            {j.name || "未命名作业"}
                          </p>
                          <p className="mt-1 font-mono text-xs text-muted-foreground">
                            {j.key.slice(0, 12)}
                          </p>
                        </TableCell>
                        <TableCell>
                          {j.user || "—"}
                          <p className="text-xs text-muted-foreground">
                            {j.account || "—"} · {j.partition || "—"}
                          </p>
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1">
                            {j.states.map((s) => (
                              <SlurmStatusBadge key={s} state={s} />
                            ))}
                          </div>
                          <p className="mt-1 text-xs">
                            退出码 {quantity(j.exit.code)} · 信号{" "}
                            {quantity(j.exit.signal)}
                          </p>
                        </TableCell>
                        <TableCell className="text-xs">
                          <p>{jobTime(j.submitTime)}</p>
                          <p className="mt-1 text-muted-foreground">
                            {jobTime(j.endTime)}
                          </p>
                        </TableCell>
                        <TableCell className="text-xs">
                          <p>{quantity(j.elapsedSeconds, " 秒")}</p>
                          <p className="mt-1 text-muted-foreground">
                            {cpu(j.totalCpuSeconds)}
                          </p>
                        </TableCell>
                        <TableCell className="max-w-60 text-xs break-all whitespace-normal">
                          {resourceText(j.allocatedTres)}
                        </TableCell>
                      </TableRow>
                    ))}
                    {!data.items.length && (
                      <TableRow>
                        <TableCell
                          colSpan={6}
                          className="p-10 text-center text-muted-foreground"
                        >
                          当前提交时间范围与筛选条件下没有可见记账记录。
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
                <div className="flex items-center justify-between gap-3 border-t p-4">
                  <Button
                    variant="outline"
                    disabled={page === 1 || list.isFetching}
                    onClick={() =>
                      update({ page: String(page - 1), record: null })
                    }
                  >
                    上一页历史
                  </Button>
                  <p className="text-xs">
                    第 {page} 页 · {data.totalItems} 条记录
                  </p>
                  <Button
                    variant="outline"
                    disabled={page * 20 >= data.totalItems || list.isFetching}
                    onClick={() =>
                      update({ page: String(page + 1), record: null })
                    }
                  >
                    下一页历史
                  </Button>
                </div>
              </section>
            )
          )}
        </>
      )}
      <Sheet
        open={!!record && !!cid && allowed}
        onOpenChange={(open) => {
          if (!open) update({ record: null })
        }}
      >
        <SheetContent
          className="overflow-y-auto"
          style={{ width: "min(100vw, 52rem)", maxWidth: "100vw" }}
        >
          <SheetHeader>
            <SheetTitle>
              历史作业{" "}
              {detail.data && !detail.isError
                ? `#${detail.data.item.id}`
                : "详情"}
            </SheetTitle>
            <SheetDescription>
              独立记账记录与步骤；仅提供查询。
            </SheetDescription>
          </SheetHeader>
          {detail.isPending ? (
            <p role="status" className="p-5">
              正在读取记录…
            </p>
          ) : detail.isError ? (
            <p role="alert" className="p-5 text-destructive">
              {errorMessage(detail.error)}
            </p>
          ) : (
            detail.data && (
              <Detail
                job={detail.data.item}
                clusterId={cid}
                start={detail.data.start}
                end={detail.data.end}
              />
            )
          )}
        </SheetContent>
      </Sheet>
    </div>
  )
}
