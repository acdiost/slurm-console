import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Stethoscope, RefreshCw } from "lucide-react"
import { ClientResponseError } from "pocketbase"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { useAuth } from "@/stores/auth"
import { api } from "@/services/api"
import { date, number, percent } from "@/features/capacity/schema"
import { quantity } from "@/features/jobs/schema"
import { diagnosisSchema, type Diagnosis } from "./schema"
export function DiagnosticPanel({
  clusterId,
  jobId,
  source,
  recordKey = "",
  start = 0,
  end = 0,
}: {
  clusterId: string
  jobId: string
  source: "controller" | "accounting"
  recordKey?: string
  start?: number
  end?: number
}) {
  const user = useAuth((s) => s.user),
    [expanded, setExpanded] = useState(false)
  const permission = source === "controller" ? "jobs.read" : "job_history.read"
  const allowed =
    !!user &&
    (user.access.platformPermissions.includes(permission) ||
      user.access.clusters.some(
        (c) => c.cluster.id === clusterId && c.permissions.includes(permission)
      ))
  const path =
    source === "controller"
      ? `/clusters/${clusterId}/jobs/${encodeURIComponent(jobId)}/diagnosis`
      : `/clusters/${clusterId}/job-history/${encodeURIComponent(recordKey)}/diagnosis?${new URLSearchParams({ jobId, start: String(start), end: String(end) })}`
  const result = useQuery<Diagnosis>({
    queryKey: [
      "job-diagnosis",
      user?.id,
      clusterId,
      source,
      jobId,
      recordKey,
      start,
      end,
    ],
    enabled: expanded && allowed,
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 0,
    queryFn: async ({ signal }) =>
      diagnosisSchema.parse(await api(path, { signal })),
  })
  const data = allowed && !result.isError ? result.data : undefined,
    r = data?.report
  return (
    <section
      aria-label="作业异常诊断"
      className="space-y-4 rounded-lg border p-4"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="flex items-center gap-2 font-semibold">
          <Stethoscope className="size-5" />
          作业异常诊断
        </h2>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={!allowed || result.isFetching}
            onClick={() => {
              if (expanded) void result.refetch()
              else setExpanded(true)
            }}
          >
            {expanded ? <RefreshCw /> : <Stethoscope />}
            {expanded ? "重新生成诊断" : "诊断作业"}
          </Button>
          {expanded && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setExpanded(false)}
            >
              收起诊断
            </Button>
          )}
        </div>
      </div>
      <p className="text-sm leading-6 text-muted-foreground">
        按需读取此条{source === "controller" ? "控制器作业" : "历史记账记录"}
        ，列出事实、解释和待验证项。不会执行作业控制或自动读取日志。
      </p>
      {!allowed && (
        <p role="alert" className="text-sm text-destructive">
          当前没有此诊断来源的读取权限。
        </p>
      )}
      {expanded && allowed && (
        <>
          {result.isFetching && (
            <p role="status" className="text-sm">
              正在读取来源并生成诊断…
            </p>
          )}
          {result.isError ? (
            <p role="alert" className="text-sm text-destructive">
              {result.error instanceof ClientResponseError &&
              [403, 404].includes(result.error.status)
                ? "作业记录或来源权限不可用，请重新核对当前集群及具体记录。"
                : "诊断来源暂不可用或配置已变化，请刷新详情后重试。"}
            </p>
          ) : (
            r && (
              <>
                <header className="space-y-2">
                  <h3 className="font-semibold">{r.headline}</h3>
                  <p className="text-xs leading-6 text-muted-foreground">
                    {r.source === "controller" ? "控制器" : "记账库"} · 来源观测{" "}
                    {date(r.observedAt)} · {data.cached ? "来源缓存" : "已读取"}
                    <br />
                    诊断生成 {date(r.generatedAt)} · 规则 v{r.version}
                  </p>
                  <p className="text-sm break-all">
                    #{r.identity.jobId} · {r.identity.name || "未命名"} ·{" "}
                    {r.identity.user || "未知用户"}
                  </p>
                  {r.identity.recordKey && (
                    <p className="font-mono text-xs break-all text-muted-foreground">
                      历史记录标识 {r.identity.recordKey}
                    </p>
                  )}
                  <p className="text-xs text-muted-foreground">
                    数组 Job / Task：{quantity(r.identity.arrayJobId)} /{" "}
                    {quantity(r.identity.arrayTaskId)} · 异构 Job / Offset：
                    {quantity(r.identity.hetJobId)} /{" "}
                    {quantity(r.identity.hetJobOffset)}
                  </p>
                </header>
                <dl className="grid gap-3 sm:grid-cols-2">
                  {[
                    [
                      "提交至开始间隔",
                      r.timeline.submitToStartSeconds === null
                        ? "—"
                        : `${number(r.timeline.submitToStartSeconds)} 秒`,
                    ],
                    [
                      r.source === "controller"
                        ? "提交至观测间隔"
                        : "记账已运行时长",
                      `${number(r.source === "controller" ? r.timeline.sinceSubmitSeconds : r.timeline.reportedElapsedSeconds)}${(r.source === "controller" ? r.timeline.sinceSubmitSeconds : r.timeline.reportedElapsedSeconds) === null ? "" : " 秒"}`,
                    ],
                    ...(r.source === "controller"
                      ? [
                          [
                            "开始至观测间隔",
                            r.timeline.observedRunSeconds === null
                              ? "—"
                              : `${number(r.timeline.observedRunSeconds)} 秒`,
                          ],
                        ]
                      : [
                          [
                            "粗略 CPU 记账占比",
                            percent(r.metrics.cpuAccountingPercent),
                          ],
                          [
                            "分配 CPU / 作业总 CPU 时间",
                            `${number(r.metrics.allocatedCpus)} / ${r.metrics.totalCpuSeconds === null ? "—" : `${number(r.metrics.totalCpuSeconds)} CPU 秒`}`,
                          ],
                        ]),
                  ].map(([k, v]) => (
                    <div key={k} className="rounded-md bg-muted/40 p-3">
                      <dt className="text-xs text-muted-foreground">{k}</dt>
                      <dd className="mt-2 text-sm font-medium tabular-nums">
                        {v}
                      </dd>
                    </div>
                  ))}
                </dl>
                <p className="text-xs leading-6 text-muted-foreground">
                  时间间隔不等于程序 CPU
                  时间或有效排队时间。置信度针对每条描述，不能据此确认唯一根因。
                </p>
                <div className="space-y-4" aria-label="诊断发现">
                  {r.findings.map((f) => (
                    <article
                      key={f.code}
                      className="space-y-3 rounded-md border p-4"
                    >
                      <div className="flex flex-wrap items-center gap-2">
                        <Badge
                          variant={
                            f.severity === "warning" ? "secondary" : "outline"
                          }
                        >
                          {f.kind === "fact" ? "已观察事实" : "解释 / 待验证"}
                        </Badge>
                        <span className="text-xs text-muted-foreground">
                          置信度：
                          {
                            { high: "高", medium: "中", low: "低" }[
                              f.confidence
                            ]
                          }
                        </span>
                      </div>
                      <h4 className="font-medium">{f.title}</h4>
                      <p className="text-sm leading-6">{f.explanation}</p>
                      <details>
                        <summary className="cursor-pointer text-sm text-primary">
                          支撑证据（{f.evidenceIds.length}）
                        </summary>
                        <dl className="mt-3 space-y-3">
                          {f.evidenceIds.map((id) => {
                            const fact = r.evidence.find((v) => v.id === id)
                            return fact ? (
                              <div key={id}>
                                <dt className="text-xs text-muted-foreground">
                                  {fact.label} · {fact.field}
                                </dt>
                                <dd className="mt-1 text-sm break-all">
                                  {fact.value || "未提供"}
                                </dd>
                              </div>
                            ) : null
                          })}
                        </dl>
                      </details>
                      {f.checks.length > 0 && (
                        <div className="space-y-2 border-t pt-3">
                          <h5 className="text-sm font-medium">
                            最小下一步核查
                          </h5>
                          {f.checks.map((c, i) => (
                            <p key={i} className="text-sm leading-6">
                              <span className="mr-2 text-xs font-medium text-muted-foreground">
                                {c.audience === "admin"
                                  ? "管理员核查"
                                  : "作业用户核查"}
                              </span>
                              {c.text}
                            </p>
                          ))}
                        </div>
                      )}
                    </article>
                  ))}
                </div>
                {r.source === "accounting" && (
                  <p className="text-xs leading-6 text-muted-foreground">
                    来源步骤 {r.coverage.totalSteps} 个 · 异常{" "}
                    {r.coverage.abnormalSteps} 个 · 已列出前{" "}
                    {r.coverage.displayedSteps}{" "}
                    个异常步骤证据。完整步骤请查看本条历史详情。
                  </p>
                )}
                <details>
                  <summary className="cursor-pointer text-sm">
                    全部来源事实（{r.evidence.length}）
                  </summary>
                  <dl className="mt-3 space-y-3">
                    {r.evidence.map((f) => (
                      <div key={f.id}>
                        <dt className="text-xs text-muted-foreground">
                          {f.label} · {f.field}
                        </dt>
                        <dd className="mt-1 text-sm break-all">
                          {f.value || "未提供"}
                        </dd>
                      </div>
                    ))}
                  </dl>
                </details>
                <div
                  className="space-y-2 border-t pt-4"
                  aria-label="诊断证据缺口"
                >
                  <h4 className="text-sm font-medium">证据边界与缺口</h4>
                  {r.limitations.map((v, i) => (
                    <p
                      key={i}
                      className="text-xs leading-6 text-muted-foreground"
                    >
                      {v}
                    </p>
                  ))}
                </div>
              </>
            )
          )}
        </>
      )}
    </section>
  )
}
