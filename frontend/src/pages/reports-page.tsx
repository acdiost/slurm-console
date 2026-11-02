import { useEffect, useRef, useState } from "react"
import { useSearchParams } from "react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ChartColumn, Download, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
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
} from "@/features/clusters/cluster-picker"
import { localDate } from "@/features/history/schema"
import {
  reportSchema,
  hours,
  sorted,
  csv,
  dimensions,
  errorMessage,
  type ReportMetric,
} from "@/features/reports/schema"
import { date } from "@/features/capacity/schema"
import { useAuth } from "@/stores/auth"
import { api } from "@/services/api"
const selectStyle = "h-10 rounded-md border bg-background px-3 text-sm"
function allowed(id: string) {
  const user = useAuth.getState().user
  return ["reports.read", "job_history.read"].every(
    (p) =>
      user?.access.platformPermissions.includes(p) ||
      user?.access.clusters.some(
        (c) => c.cluster.id === id && c.permissions.includes(p)
      )
  )
}
export function ReportsPage() {
  const selection = useAccessibleClusters(),
    user = useAuth((s) => s.user),
    id = selection.id
  return (
    <div className="space-y-6 p-5 sm:p-8">
      <header>
        <p className="eyebrow">RESOURCE USAGE REPORT</p>
        <h1 className="mt-2 flex items-center gap-3 text-3xl font-semibold">
          <ChartColumn className="size-7 text-primary" />
          资源使用报表
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          按运行窗口汇总记账分配时长，核对计量覆盖并导出。
        </p>
      </header>
      <ClusterPicker selection={selection} label="资源报表" />
      {!id ? (
        <p role="status" className="panel p-5">
          请选择有权限的集群。
        </p>
      ) : !allowed(id) ? (
        <p role="alert" className="panel p-5">
          需要此集群的报表和历史记账读取权限。
        </p>
      ) : (
        <Workspace key={`${user?.id}:${id}`} id={id} />
      )}
    </div>
  )
}
function Metric({
  value,
  memory = false,
}: {
  value: ReportMetric
  memory?: boolean
}) {
  return (
    <div>
      <span className="font-mono tabular-nums">
        {hours(value.resourceSeconds, memory)}
      </span>
      <p className="mt-1 text-xs text-muted-foreground">
        已知 {value.knownRecords} / {value.knownRecords + value.unknownRecords}{" "}
        条
      </p>
    </div>
  )
}
function Workspace({ id }: { id: string }) {
  const user = useAuth((s) => s.user),
    [params, setParams] = useSearchParams(),
    client = useQueryClient(),
    mounted = useRef(false)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const [defaults] = useState(() => {
    const end = Math.floor(Date.now() / 60000) * 60
    return { start: end - 86400, end }
  })
  const [formError, setFormError] = useState(""),
    [page, setPage] = useState(1),
    [sort, setSort] = useState("cpu"),
    [order, setOrder] = useState("desc")
  const start = Number(params.get("start") || defaults.start),
    end = Number(params.get("end") || defaults.end),
    by = params.get("groupBy") || "user"
  const valid =
    Number.isSafeInteger(start) &&
    Number.isSafeInteger(end) &&
    start > 0 &&
    end > start &&
    end - start <= 31 * 86400 &&
    ["user", "account", "partition"].includes(by)
  const q = new URLSearchParams({
    start: String(start),
    end: String(end),
    groupBy: by,
  })
  for (const k of ["user", "account", "partition"])
    if (params.get(k)) q.set(k, params.get(k)!)
  const url = `/clusters/${encodeURIComponent(id)}/reports/usage?${q}`,
    key = ["usage-report", user?.id, id, q.toString()]
  const list = useQuery({
    queryKey: key,
    enabled: valid && allowed(id),
    queryFn: async ({ signal }) =>
      reportSchema.parse(await api(url, { signal })),
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 30000,
  })
  const exporter = useMutation({
    retry: false,
    mutationFn: async (v: {
      url: string
      key: typeof key
      owner: string | undefined
    }) => reportSchema.parse(await api(v.url)),
    onSuccess: (data, v) => {
      if (
        !mounted.current ||
        useAuth.getState().user?.id !== v.owner ||
        !allowed(id)
      )
        return
      client.setQueryData(v.key, data)
      const blob = new Blob([csv(data, id)], {
          type: "text/csv;charset=utf-8",
        }),
        href = URL.createObjectURL(blob),
        anchor = document.createElement("a")
      anchor.href = href
      anchor.download = `slurm-usage-${data.report.groupBy}-${data.report.start}-${data.report.end}.csv`
      anchor.click()
      URL.revokeObjectURL(href)
    },
  })
  const data =
      !list.isError && !exporter.isError && allowed(id) ? list.data : undefined,
    r = data?.report,
    rows = r ? sorted(r.groups, sort, order) : [],
    totalPages = Math.max(1, Math.ceil(rows.length / 20)),
    currentPage = Math.min(page, totalPages)
  const refresh = () => {
    exporter.reset()
    void list.refetch()
  }
  return (
    <>
      <form
        className="panel space-y-4 p-5"
        aria-label="资源报表条件"
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
            b - a > 31 * 86400 ||
            b > Math.floor(Date.now() / 1000)
          ) {
            setFormError("请选择截至当前、最多 31 天的有效运行窗口。")
            return
          }
          setFormError("")
          setPage(1)
          exporter.reset()
          setParams((p) => {
            const n = new URLSearchParams(p)
            n.set("cluster", id)
            n.set("start", String(a))
            n.set("end", String(b))
            for (const k of ["user", "account", "partition", "groupBy"]) {
              const v = String(f.get(k) || "").trim()
              if (v) n.set(k, v)
              else n.delete(k)
            }
            return n
          })
        }}
      >
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          <Label className="grid gap-2">
            运行窗口起点
            <Input
              key={`from:${start}`}
              name="from"
              type="datetime-local"
              required
              defaultValue={Number.isFinite(start) ? localDate(start) : ""}
            />
          </Label>
          <Label className="grid gap-2">
            运行窗口终点
            <Input
              key={`to:${end}`}
              name="to"
              type="datetime-local"
              required
              defaultValue={Number.isFinite(end) ? localDate(end) : ""}
            />
          </Label>
          <Label className="grid gap-2">
            汇总维度
            <select
              key={by}
              name="groupBy"
              defaultValue={by}
              className={selectStyle}
            >
              {Object.entries(dimensions).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </Label>
          {(["user", "account", "partition"] as const).map((k) => (
            <Label className="grid gap-2" key={`${k}:${params.get(k)}`}>
              {dimensions[k]}精确筛选
              <Input
                name={k}
                defaultValue={params.get(k) || ""}
                maxLength={64}
                placeholder="全部；或一个精确值"
              />
            </Label>
          ))}
        </div>
        <div className="flex flex-wrap gap-3">
          <Button>生成报表</Button>
          <Button
            type="button"
            variant="outline"
            disabled={!valid || list.isFetching || exporter.isPending}
            onClick={refresh}
          >
            <RefreshCw />
            刷新报表
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={!data || list.isFetching || exporter.isPending}
            onClick={() => exporter.mutate({ url, key, owner: user?.id })}
          >
            <Download />
            {exporter.isPending ? "正在核验并导出…" : "导出全部分组 CSV"}
          </Button>
        </div>
        {(formError || !valid) && (
          <p role="alert" className="text-sm text-destructive">
            {formError || "运行窗口或汇总维度不合法，请重新选择。"}
          </p>
        )}
      </form>
      <section
        className="panel space-y-2 p-5 text-sm"
        aria-label="报表统计口径"
      >
        <p className="font-medium">
          运行区间交集 · 分配时长估算 · 记账可见范围
        </p>
        <p className="leading-6 text-muted-foreground">
          使用已结束且时间核查通过的记录，将记账分配量视为固定值乘以窗口内时长。历史变更分配可能影响估算；结果不代表实际
          CPU/GPU
          利用率、内存峰值或计费额度。运行中、异构作业、数组父记录及有歧义的记录不计量，独立数组任务可计量；缺失资源不补零。CPU
          按 Slurm 配置单位计量，GPU 使用 gres/gpu 计数，可能包含 MIG 单元。
        </p>
        <p className="text-muted-foreground">
          受固定连接身份、记账延迟与保留策略限制；不跨逻辑集群合计。CSV
          重新校验权限，包含本次全部分组及一条 summary
          汇总行，保留整数资源秒和覆盖数。
        </p>
        {data && (
          <p>
            实际集群 {data.report.cluster} · 来源 {date(data.fetchedAt)} ·{" "}
            {data.cached ? "缓存（最多 30 秒）" : "实时读取"} ·{" "}
            {data.report.sourceRecords} 条来源记录，{data.report.outsideRecords}{" "}
            条在窗口外
          </p>
        )}
      </section>
      {(list.isError || exporter.isError) && (
        <p role="alert" className="panel p-5 text-destructive">
          {errorMessage(exporter.error || list.error)}
        </p>
      )}
      {list.isPending && valid && <p role="status">正在生成记账报表…</p>}
      {r && (
        <>
          <section
            className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4"
            aria-label="报表汇总"
          >
            <article className="panel p-5">
              <p className="text-sm text-muted-foreground">窗口来源记录</p>
              <p className="mt-3 text-3xl font-semibold tabular-nums">
                {r.summary.records}
              </p>
              <p className="mt-2 text-xs text-muted-foreground">
                时间核查通过 {r.summary.countedRecords} 条 · {r.groups.length}{" "}
                个分组
              </p>
            </article>
            {(
              [
                ["cpu", "CPU 分配单位·时"],
                ["memory", "内存分配 GiB·时"],
                ["gpu", "GPU 分配单位·时"],
              ] as const
            ).map(([k, title]) => (
              <article className="panel p-5" key={k}>
                <p className="mb-3 text-sm text-muted-foreground">
                  {title}（估算）
                </p>
                <Metric value={r.summary[k]} memory={k === "memory"} />
                {r.summary[k].unknownRecords > 0 && (
                  <p className="mt-2 text-xs text-amber-600">
                    仅已知部分，非完整总量
                  </p>
                )}
              </article>
            ))}
          </section>
          <section className="panel p-5 text-sm" aria-label="未计量记录">
            <p>
              未计量：活动记录 {r.summary.excluded.active} · 复杂记录{" "}
              {r.summary.excluded.complex} · 状态/时间不明{" "}
              {r.summary.excluded.uncertain} · 重叠重复身份{" "}
              {r.summary.excluded.overlapping}
            </p>
            <p className="mt-2 text-muted-foreground">
              “记录数”保留重复 Job ID
              的独立记录，不等于去重作业数；步骤不另行累计。资源缺失可使各指标覆盖数不同。
            </p>
          </section>
          <section className="panel overflow-hidden" aria-label="报表分组明细">
            <div className="flex flex-wrap items-center justify-between gap-3 p-5">
              <h2 className="font-semibold">按{dimensions[r.groupBy]}汇总</h2>
              <div className="flex gap-2">
                <Label className="sr-only" htmlFor="report-sort">
                  报表排序
                </Label>
                <select
                  id="report-sort"
                  className={selectStyle}
                  value={sort}
                  onChange={(e) => {
                    setSort(e.target.value)
                    setPage(1)
                  }}
                >
                  {[
                    ["cpu", "CPU 分配时长"],
                    ["memory", "内存分配时长"],
                    ["gpu", "GPU 分配时长"],
                    ["records", "记录数"],
                    ["key", "分组名称"],
                  ].map(([k, v]) => (
                    <option key={k} value={k}>
                      {v}
                    </option>
                  ))}
                </select>
                <Button
                  variant="outline"
                  onClick={() =>
                    setOrder((v) => (v === "asc" ? "desc" : "asc"))
                  }
                >
                  {order === "asc" ? "升序" : "降序"}
                </Button>
              </div>
            </div>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{dimensions[r.groupBy]}</TableHead>
                  <TableHead>记录 / 时间通过</TableHead>
                  <TableHead>CPU 单位·时（估算）</TableHead>
                  <TableHead>内存 GiB·时（估算）</TableHead>
                  <TableHead>GPU 单位·时（估算）</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows
                  .slice((currentPage - 1) * 20, currentPage * 20)
                  .map((row) => (
                    <TableRow key={row.key}>
                      <TableCell className="max-w-52 font-medium break-all whitespace-pre-wrap">
                        {row.key || "未记录维度"}
                      </TableCell>
                      <TableCell>
                        {row.records} / {row.countedRecords}
                      </TableCell>
                      <TableCell>
                        <Metric value={row.cpu} />
                      </TableCell>
                      <TableCell>
                        <Metric value={row.memory} memory />
                      </TableCell>
                      <TableCell>
                        <Metric value={row.gpu} />
                      </TableCell>
                    </TableRow>
                  ))}
              </TableBody>
            </Table>
            {!rows.length && (
              <p
                role="status"
                className="p-8 text-center text-muted-foreground"
              >
                此窗口和筛选下没有可见记账记录。
              </p>
            )}
            <div className="flex items-center justify-between p-5 text-sm">
              <span>
                第 {currentPage} / {totalPages} 页 · 共 {rows.length} 个分组
              </span>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  disabled={currentPage <= 1}
                  onClick={() => setPage(currentPage - 1)}
                >
                  上一页
                </Button>
                <Button
                  variant="outline"
                  disabled={currentPage >= totalPages}
                  onClick={() => setPage(currentPage + 1)}
                >
                  下一页
                </Button>
              </div>
            </div>
          </section>
        </>
      )}
    </>
  )
}
