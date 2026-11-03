import { useEffect, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Sparkles } from "lucide-react"
import { ClientResponseError } from "pocketbase"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { useAuth } from "@/stores/auth"
import { api } from "@/services/api"
import { date } from "@/features/capacity/schema"
import {
  statusSchema,
  previewSchema,
  explanationSchema,
  type Preview,
  type Explanation,
} from "./schema"

export function AIAssistance({
  clusterId,
  source,
  previewPath,
}: {
  clusterId: string
  source: "controller" | "accounting"
  previewPath: string
}) {
  const user = useAuth((s) => s.user)
  const allows = (p: string) =>
    !!user &&
    (user.access.platformPermissions.includes(p) ||
      user.access.clusters.some(
        (c) => c.cluster.id === clusterId && c.permissions.includes(p)
      ))
  return allows("ai.explain") &&
    allows(source === "controller" ? "jobs.read" : "job_history.read") ? (
    <Panel
      key={`${user?.id}:${clusterId}:${previewPath}`}
      actor={user!.id}
      clusterId={clusterId}
      source={source}
      previewPath={previewPath}
    />
  ) : null
}
function Panel({
  actor,
  clusterId,
  source,
  previewPath,
}: {
  actor: string
  clusterId: string
  source: string
  previewPath: string
}) {
  const [preview, setPreview] = useState<Preview>(),
    [answer, setAnswer] = useState<Explanation>(),
    [reviewed, setReviewed] = useState<Preview>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [confirmed, setConfirmed] = useState(false),
    [confirmation, setConfirmation] = useState(""),
    [expired, setExpired] = useState(false)
  const status = useQuery({
    queryKey: ["ai-assistance", actor, clusterId, source],
    queryFn: async ({ signal }) =>
      statusSchema.parse(
        await api(`/clusters/${clusterId}/ai-assistance?source=${source}`, {
          signal,
        })
      ),
    retry: false,
    refetchOnWindowFocus: false,
  })
  useEffect(() => {
    if (!preview) return
    const timer = setTimeout(
      () => setExpired(true),
      Math.max(0, Date.parse(preview.expiresAt) - Date.now())
    )
    return () => clearTimeout(timer)
  }, [preview])
  const reset = () => {
    setPreview(undefined)
    setAnswer(undefined)
    setReviewed(undefined)
    setConfirmed(false)
    setConfirmation("")
    setExpired(false)
    setError("")
  }
  const failed = (e: unknown) => {
    setPreview(undefined)
    setAnswer(undefined)
    setReviewed(undefined)
    setConfirmed(false)
    setConfirmation("")
    setError(
      e instanceof ClientResponseError
        ? `${String(e.response?.error?.message || "AI 请求未完成。不会自动重试。")}${e.response?.requestId ? ` 请求标识：${String(e.response.requestId)}` : ""}`
        : "AI 返回格式或网络不可用，结果已丢弃。请求可能已经发送，不会自动重试。"
    )
  }
  const prepare = async () => {
    reset()
    setBusy(true)
    try {
      setPreview(
        previewSchema.parse(
          await api(previewPath, { method: "POST", body: {} })
        )
      )
    } catch (e) {
      failed(e)
    } finally {
      setBusy(false)
    }
  }
  const send = async () => {
    if (!preview || !confirmed || confirmation !== "SEND AI" || expired) return
    const current = preview
    setReviewed(current)
    setBusy(true)
    setPreview(undefined)
    setConfirmed(false)
    setConfirmation("")
    try {
      setAnswer(
        explanationSchema.parse(
          await api(
            `/clusters/${clusterId}/ai-assistance/${current.id}/explain`,
            {
              method: "POST",
              body: { confirmation: "SEND AI", digest: current.digest },
            }
          )
        )
      )
    } catch (e) {
      failed(e)
    } finally {
      setBusy(false)
    }
  }
  const destination =
    !status.isError && status.data?.enabled
      ? status.data.destination
      : undefined
  return (
    <section aria-label="AI 运维辅助" className="space-y-4 border-t pt-5">
      <h3 className="flex items-center gap-2 font-semibold">
        <Sparkles className="size-5" />
        AI 运维辅助{" "}
        <span className="text-xs font-normal text-muted-foreground">
          可选 · 只读建议
        </span>
      </h3>
      <p className="text-sm leading-6 text-muted-foreground">
        仅发送预览中列出的归一化状态、数值和规则提示。不发送作业名称、用户名、节点名、日志、脚本或凭据。建议未经人工核实，不会执行任何操作。
      </p>
      {status.isPending ? (
        <p role="status">正在检查 AI 配置…</p>
      ) : status.isError ? (
        <p role="alert">AI 配置或访问权限不可用，请刷新诊断。</p>
      ) : !destination ? (
        <p className="rounded-md bg-muted/40 p-3 text-sm">
          此集群尚未启用 AI 服务。管理员配置服务、模型与集群允许列表后可使用。
        </p>
      ) : (
        <>
          <p className="text-sm">
            服务：{destination.label} · 模型：
            {destination.model}
          </p>
          {!preview && (
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => void prepare()}
            >
              {busy ? "正在处理…" : "预览发送内容"}
            </Button>
          )}
          {preview && (
            <div
              aria-label="发送前预览"
              className="space-y-4 rounded-lg border p-4"
            >
              <h4 className="font-semibold">确认本次发送内容</h4>
              <p className="text-sm leading-6 break-all">
                接收地址：{preview.destination.endpoint}
                <br />
                模型：{preview.destination.model}
                <br />
                证据观测：{date(preview.observedAt)} · 预览到期：
                {date(preview.expiresAt)}
              </p>
              <p className="text-sm text-muted-foreground">
                下方是将发送的完整请求正文。认证由服务端添加。服务方如何留存请求取决于其政策；请确认允许将这些证据发送到该服务。
              </p>
              <pre
                aria-label="完整请求正文"
                className="max-h-96 overflow-auto rounded-md bg-muted p-3 text-xs break-all whitespace-pre-wrap"
              >
                {JSON.stringify(preview.request, null, 2)}
              </pre>
              <p className="font-mono text-xs break-all text-muted-foreground">
                SHA-256：{preview.digest}
              </p>
              <label className="flex items-start gap-3 text-sm leading-6">
                <Checkbox
                  checked={confirmed}
                  onCheckedChange={(v) => setConfirmed(v === true)}
                  disabled={busy || expired}
                />
                <span>我已核对接收服务和证据，确认本次发送。</span>
              </label>
              <label className="block space-y-2 text-sm">
                <span>输入 SEND AI 确认</span>
                <Input
                  aria-label="输入 SEND AI 确认"
                  value={confirmation}
                  onChange={(e) => setConfirmation(e.target.value)}
                  disabled={busy || expired}
                  autoComplete="off"
                />
              </label>
              {expired && <p role="alert">预览已到期，请取消后重新预览。</p>}
              <div className="flex flex-wrap gap-2">
                <Button
                  disabled={
                    busy || expired || !confirmed || confirmation !== "SEND AI"
                  }
                  onClick={() => void send()}
                >
                  确认发送并生成建议
                </Button>
                <Button variant="outline" disabled={busy} onClick={reset}>
                  取消预览
                </Button>
              </div>
            </div>
          )}
          {answer && (
            <article
              aria-label="AI 生成建议"
              className="space-y-4 rounded-lg border p-4"
            >
              <h4 className="font-semibold">AI 生成建议 · 未核实</h4>
              <p className="text-sm leading-7 whitespace-pre-wrap">
                {answer.answer.summary}
              </p>
              <details className="text-sm">
                <summary className="cursor-pointer font-medium">
                  本次发送的证据（含引用标识）
                </summary>
                <pre className="mt-3 max-h-72 overflow-auto rounded-md bg-muted p-3 text-xs break-all whitespace-pre-wrap">
                  {reviewed?.request.messages[1]?.content}
                </pre>
              </details>
              <h5 className="text-sm font-semibold">证据解释</h5>
              {answer.answer.observations.map((o, i) => (
                <p className="text-sm leading-7 whitespace-pre-wrap" key={i}>
                  {o.text}
                  <span className="ml-2 text-xs text-muted-foreground">
                    依据：{o.evidenceIds.join("、")}
                  </span>
                </p>
              ))}
              <h5 className="text-sm font-semibold">建议人工核查</h5>
              {answer.answer.checks.map((c, i) => (
                <p className="text-sm leading-7 whitespace-pre-wrap" key={i}>
                  {c.audience === "admin" ? "管理员" : "用户"}：{c.text}
                  <span className="ml-2 text-xs text-muted-foreground">
                    依据：{c.evidenceIds.join("、")}
                  </span>
                </p>
              ))}
              {answer.answer.limitations.map((v, i) => (
                <p className="text-xs leading-6 text-muted-foreground" key={i}>
                  {v}
                </p>
              ))}
              <p className="text-xs leading-6 text-muted-foreground">
                仅针对发送时预览的证据快照。服务：{answer.destination.label} ·
                模型：{answer.destination.model} · 观测：
                {date(answer.observedAt)}
                <br />
                操作编号：{answer.operationId}
                。刷新、切换或收起诊断后清除本页建议。
              </p>
            </article>
          )}
        </>
      )}
      {error && (
        <p role="alert" className="text-sm leading-6 text-destructive">
          {error}
        </p>
      )}
    </section>
  )
}
