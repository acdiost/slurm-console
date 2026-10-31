import { useState, useEffect, useRef } from "react"
import { useMutation, useQuery } from "@tanstack/react-query"
import { ShieldAlert, RefreshCw } from "lucide-react"
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { api } from "@/services/api"
import { timestamp } from "@/features/overview/schema"
import {
  viewSchema,
  label,
  statusLabel,
  phrase,
  validReason,
  errorMessage,
  type BulkAction,
  type BulkView,
} from "./schema"
export function BulkControlDialog({
  clusterId,
  action,
  targets,
  onClose,
  onStart,
  onResult,
}: {
  clusterId: string
  action: BulkAction
  targets: string[]
  onClose: () => void
  onStart: (ids: string[], batchId: string) => void
  onResult: () => void
}) {
  const [reason, setReason] = useState(""),
    [confirmation, setConfirmation] = useState(""),
    [submitted, setSubmitted] = useState(false),
    [error, setError] = useState("")
  const base = `/clusters/${encodeURIComponent(clusterId)}/bulk-control`
  const prepared = useQuery({
    queryKey: ["bulk-prepare", clusterId, action, targets.join(",")],
    queryFn: async ({ signal }) =>
      viewSchema.parse(
        await api(`${base}/prepare`, {
          method: "POST",
          body: { action, targets },
          signal,
        })
      ),
    retry: false,
    gcTime: 0,
    staleTime: 0,
    refetchOnWindowFocus: false,
  })
  const context = prepared.isError ? undefined : prepared.data,
    expected = phrase(action, targets.length)
  const mutation = useMutation({
    retry: false,
    mutationFn: async () => {
      if (!context?.allowed || !context.id || prepared.isFetching)
        throw new Error("Confirmation unavailable")
      setSubmitted(true)
      onStart(
        context.items.map((i) => i.operationId),
        context.id
      )
      return viewSchema.parse(
        await api(`${base}/${context.id}/execute`, {
          method: "POST",
          body: { confirmation, reason: reason.trim() },
        })
      )
    },
    onSuccess: onResult,
    onError: (cause) => {
      setError(errorMessage(cause))
      onResult()
    },
  })
  const current = useQuery({
    queryKey: ["bulk-state", clusterId, context?.id],
    enabled: submitted && !!context?.id && !mutation.isPending,
    queryFn: async ({ signal }) =>
      viewSchema.parse(await api(`${base}/${context!.id}`, { signal })),
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: false,
    refetchInterval: (q) =>
      !q.state.error && q.state.data?.status === "processing" ? 1500 : false,
  })
  const value: BulkView | undefined = submitted
    ? current.isError
      ? mutation.data
      : current.data || mutation.data || context
    : context
  const reported = useRef(false)
  const finished = value?.status === "completed"
  useEffect(() => {
    if (finished && !reported.current) {
      reported.current = true
      onResult()
    }
  }, [finished, onResult])
  const valid =
    context?.allowed &&
    !prepared.isFetching &&
    !prepared.isError &&
    !submitted &&
    confirmation === expected &&
    validReason(reason)
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onClose()
      }}
    >
      <AlertDialogContent
        className="max-h-[90svh] overflow-y-auto"
        style={{
          width: "min(calc(100vw - 2rem), 48rem)",
          maxWidth: "calc(100vw - 2rem)",
        }}
      >
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <ShieldAlert className="size-5 text-destructive" />
            {label(action)} · {targets.length} 个目标
          </AlertDialogTitle>
          <AlertDialogDescription>
            {action === "jobs.cancel"
              ? "取消会中断计算并可能丢失未保存结果。"
              : action === "nodes.resume"
                ? "恢复节点后可能立即调度排队作业。"
                : "阻止分配新作业；已有作业继续运行，DRAIN 不表示作业已排空。"}
            各目标独立复查、执行和审计，部分失败不会回滚其他目标。
          </AlertDialogDescription>
        </AlertDialogHeader>
        {prepared.isPending && (
          <p role="status" className="text-sm">
            正在逐项读取最新状态并校验…
          </p>
        )}
        {prepared.isError && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(prepared.error)}
          </p>
        )}
        {value && (
          <>
            <div aria-label="批量结果汇总" className="flex flex-wrap gap-2">
              {Object.entries(value.counts)
                .filter(([, n]) => n > 0)
                .map(([s, n]) => (
                  <Badge key={s} variant="outline">
                    {statusLabel(s)} {n}
                  </Badge>
                ))}
            </div>
            <div className="max-h-80 space-y-3 overflow-y-auto">
              {value.items.map((item, i) => (
                <article
                  key={targets[i]}
                  className="space-y-2 rounded-md border p-3 text-sm"
                >
                  <p className="flex flex-wrap items-center justify-between gap-2">
                    <strong className="font-mono">
                      {item.target || targets[i]}
                    </strong>
                    <Badge variant="outline">{statusLabel(item.status)}</Badge>
                  </p>
                  {item.job && (
                    <p className="text-xs text-muted-foreground">
                      {item.job.name || "未命名作业"} · {item.job.user} · UID{" "}
                      {item.job.userId ?? "未知"}
                    </p>
                  )}
                  {item.node && (
                    <p className="text-xs text-muted-foreground">
                      {item.node.reason || "未设置状态原因"} · 已分配{" "}
                      {item.node.allocatedCpus} CPU
                    </p>
                  )}
                  <div className="flex flex-wrap gap-1">
                    {item.states.map((s) => (
                      <SlurmStatusBadge
                        key={s}
                        state={s}
                        kind={action === "jobs.cancel" ? "job" : "node"}
                      />
                    ))}
                  </div>
                  <p
                    className={
                      item.allowed || submitted
                        ? "text-xs text-muted-foreground"
                        : "text-xs text-destructive"
                    }
                  >
                    {item.message}
                  </p>
                  {item.operationId && (
                    <p className="font-mono text-xs break-all text-muted-foreground">
                      操作编号 {item.operationId}
                    </p>
                  )}
                </article>
              ))}
            </div>
          </>
        )}
        {!submitted && (
          <fieldset
            disabled={prepared.isFetching || !context?.allowed}
            className="space-y-4"
          >
            <div className="space-y-2">
              <Label htmlFor="bulk-reason">统一操作原因</Label>
              <Input
                id="bulk-reason"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                maxLength={600}
                autoComplete="off"
                placeholder="2–300 字单行原因"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="bulk-confirm">输入 {expected} 以确认</Label>
              <Input
                id="bulk-confirm"
                value={confirmation}
                onChange={(e) => setConfirmation(e.target.value)}
                placeholder={expected}
                autoComplete="off"
                className="font-mono"
              />
            </div>
            {context?.allowed && (
              <p className="text-xs text-muted-foreground">
                确认有效至 {timestamp(context.expiresAt)}
                ；排队期间状态、权限或有效期变化将逐项阻止执行。
              </p>
            )}
          </fieldset>
        )}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        {current.isError && submitted && (
          <p role="alert" className="text-sm text-destructive">
            批量上下文暂不可读。关闭后可凭已保存的逐项操作编号核实，勿重复提交。
          </p>
        )}
        {submitted && (
          <p className="text-xs text-muted-foreground">
            刷新或关闭页面不会重发控制命令。服务重启后未完成项目不会自动继续；请凭操作编号核实。
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            {submitted ? "关闭" : "返回"}
          </AlertDialogCancel>
          {!submitted && (
            <Button
              variant="outline"
              disabled={prepared.isFetching}
              onClick={() => {
                setConfirmation("")
                void prepared.refetch()
              }}
            >
              <RefreshCw />
              重新校验全部
            </Button>
          )}
          {!submitted && (
            <Button
              variant="destructive"
              disabled={!valid || mutation.isPending}
              onClick={() => {
                setError("")
                mutation.mutate()
              }}
            >
              确认批量{label(action)}
            </Button>
          )}
          {submitted && (
            <Button
              variant="outline"
              disabled={current.isFetching || mutation.isPending}
              onClick={() => void current.refetch()}
            >
              <RefreshCw />
              只读刷新结果
            </Button>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
