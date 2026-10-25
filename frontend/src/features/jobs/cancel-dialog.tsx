import { useState } from "react"
import { ClientResponseError } from "pocketbase"
import { useMutation, useQuery } from "@tanstack/react-query"
import { LoaderCircle, RefreshCw, ShieldAlert } from "lucide-react"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { api } from "@/services/api"
import { timestamp } from "@/features/overview/schema"
import {
  errorMessage,
  operationLabel,
  operationSchema,
  preparedSchema,
  type Operation,
} from "./schema"

export function CancelJobDialog({
  clusterId,
  jobId,
  onClose,
  onStart,
  onResult,
}: {
  clusterId: string
  jobId: string
  onClose: () => void
  onStart: (id: string) => void
  onResult: (operation: Operation) => void
}) {
  const [reason, setReason] = useState("")
  const [confirmation, setConfirmation] = useState("")
  const [result, setResult] = useState<Operation>()
  const [error, setError] = useState("")
  const [blocked, setBlocked] = useState(false)
  const prepared = useQuery({
    queryKey: ["job-cancel-context", clusterId, jobId],
    queryFn: async ({ signal }) =>
      preparedSchema.parse(
        await api(`/clusters/${clusterId}/jobs/${jobId}/cancel-context`, {
          signal,
        })
      ),
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  })
  const context = prepared.isError ? undefined : prepared.data
  const mutation = useMutation({
    mutationFn: async () => {
      if (!context?.allowed || prepared.isFetching)
        throw new Error("No current confirmation")
      onStart(context.operationId)
      return operationSchema.parse(
        await api(`/clusters/${clusterId}/jobs/${jobId}/cancel`, {
          method: "POST",
          body: {
            proof: context.proof,
            confirmation: confirmation.trim(),
            reason: reason.trim(),
          },
        })
      )
    },
    onSuccess: (value) => {
      setResult(value)
      onResult(value)
    },
    onError: async (cause) => {
      setError(errorMessage(cause))
      if (
        cause instanceof ClientResponseError &&
        [400, 401, 403, 404, 409, 429].includes(cause.status)
      ) {
        setBlocked(true)
        onStart("")
        return
      }
      if (!context?.operationId) return
      // Reconcile a lost response by READ, never by replaying a destructive POST.
      try {
        const known = operationSchema.parse(
          await api(
            `/clusters/${clusterId}/job-operations/${context.operationId}`
          )
        )
        setResult(known)
        onResult(known)
      } catch {
        setResult({
          operationId: context.operationId,
          jobId,
          status: "unknown",
          message:
            "尚未取得可靠操作结果。请核对作业状态和操作编号，勿重复提交。",
          reason: reason.trim(),
          states: [],
          createdAt: "",
          updatedAt: "",
        })
        onResult({
          operationId: context.operationId,
          jobId,
          status: "unknown",
          message: "操作结果待核实。",
          reason: reason.trim(),
          states: [],
          createdAt: "",
          updatedAt: "",
        })
      }
    },
  })
  const valid =
    !!context?.allowed &&
    !blocked &&
    !prepared.isFetching &&
    !prepared.isError &&
    confirmation.trim() === jobId &&
    Array.from(reason.trim()).length >= 2 &&
    Array.from(reason.trim()).length <= 300
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onClose()
      }}
    >
      <AlertDialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-xl">
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <ShieldAlert className="size-5 text-destructive" />
            取消作业 #{jobId}
          </AlertDialogTitle>
          <AlertDialogDescription>
            取消可能中断计算并丢失未保存的结果。请核对最新作业信息后确认。
          </AlertDialogDescription>
        </AlertDialogHeader>
        {prepared.isPending && (
          <p
            role="status"
            className="flex items-center gap-2 py-5 text-xs text-muted-foreground"
          >
            <LoaderCircle className="size-4 animate-spin" />
            正在检查最新状态与操作权限…
          </p>
        )}
        {prepared.isError && (
          <p role="alert" className="text-xs leading-6 text-destructive">
            {errorMessage(prepared.error)}
          </p>
        )}
        {context && (
          <div className="space-y-3 rounded-md border bg-muted/20 p-4 text-xs">
            <p className="font-medium break-all">
              {context.job.name || "未命名作业"}
            </p>
            <p className="font-mono text-2xs text-muted-foreground">
              {context.job.user} · UID {context.job.userId ?? "—"} ·{" "}
              {context.job.partition || "—"}
            </p>
            <div className="flex flex-wrap gap-1">
              <span className="mr-1 self-center text-2xs text-muted-foreground">
                确认时状态
              </span>
              {context.job.states.map((state) => (
                <SlurmStatusBadge key={state} state={state} />
              ))}
            </div>
            <p className="text-2xs leading-6 text-muted-foreground">
              {context.message}
            </p>
            {context.identity && (
              <p className="text-2xs text-muted-foreground">
                关联身份：{context.identity.username} ·{" "}
                {context.identity.scope === "own"
                  ? "仅本人作业"
                  : "管理员显式授予的集群作业范围"}
              </p>
            )}
            {context.allowed && (
              <p className="text-2xs text-muted-foreground">
                确认有效至 {timestamp(context.expiresAt)}
                ，状态变化后需重新校验。
              </p>
            )}
          </div>
        )}
        {!result && context?.allowed && (
          <fieldset
            disabled={mutation.isPending || prepared.isFetching}
            className="space-y-4"
          >
            <div className="space-y-2">
              <Label htmlFor="cancel-reason">取消原因</Label>
              <Input
                id="cancel-reason"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                maxLength={600}
                autoComplete="off"
                placeholder="2–300 字，记录到操作审计"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="cancel-confirmation">
                输入 Job ID {jobId} 以确认
              </Label>
              <Input
                id="cancel-confirmation"
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
                autoComplete="off"
                className="font-mono"
                placeholder={jobId}
              />
            </div>
          </fieldset>
        )}
        {error && (
          <p role="alert" className="text-xs leading-6 text-destructive">
            {error}
          </p>
        )}
        {result && (
          <div role="status" className="space-y-3 rounded-md border p-4">
            <Badge
              variant="outline"
              className="status-badge"
              data-tone={
                result.status === "confirmed"
                  ? "success"
                  : result.status === "failed"
                    ? "danger"
                    : "warning"
              }
            >
              {operationLabel(result.status)}
            </Badge>
            <p className="text-xs leading-6">{result.message}</p>
            <div className="flex flex-wrap gap-1">
              {result.states.map((state) => (
                <SlurmStatusBadge key={state} state={state} />
              ))}
            </div>
            <p className="text-2xs break-all text-muted-foreground">
              操作编号 <span className="font-mono">{result.operationId}</span>
            </p>
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            {result ? "关闭" : "返回"}
          </AlertDialogCancel>
          {!result && (
            <Button
              variant="outline"
              disabled={mutation.isPending || prepared.isFetching}
              onClick={() => {
                setConfirmation("")
                setError("")
                setBlocked(false)
                void prepared.refetch()
              }}
            >
              <RefreshCw />
              重新校验
            </Button>
          )}
          {!result && (
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              disabled={!valid || mutation.isPending}
              onClick={(event) => {
                event.preventDefault()
                setError("")
                mutation.mutate()
              }}
            >
              {mutation.isPending ? (
                <LoaderCircle className="animate-spin" />
              ) : (
                <ShieldAlert />
              )}
              确认取消作业
            </AlertDialogAction>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
