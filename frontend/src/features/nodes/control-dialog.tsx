import { useState } from "react"
import { useMutation, useQuery } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { LoaderCircle, RefreshCw, ShieldAlert } from "lucide-react"
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
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { api } from "@/services/api"
import { timestamp } from "@/features/overview/schema"
import {
  actionLabel,
  contextSchema,
  errorMessage,
  operationLabel,
  operationSchema,
  pooled,
  type Action,
  type Operation,
} from "./schema"

export function NodeControlDialog({
  clusterId,
  names,
  action,
  onClose,
  onStart,
  onResult,
}: {
  clusterId: string
  names: string[]
  action: Action
  onClose: () => void
  onStart: (ids: string[]) => void
  onResult: () => void
}) {
  const [reason, setReason] = useState("")
  const [confirmation, setConfirmation] = useState("")
  const [progress, setProgress] = useState<Record<string, Operation | string>>(
    {}
  )
  const [error, setError] = useState("")
  const prepared = useQuery({
    queryKey: ["node-control-context", clusterId, action, names.join(",")],
    queryFn: ({ signal }) =>
      pooled(names, async (name) => {
        try {
          return {
            name,
            context: contextSchema.parse(
              await api(
                `/clusters/${clusterId}/nodes/${encodeURIComponent(name)}/control-context?action=${action}`,
                { signal }
              )
            ),
          }
        } catch (cause) {
          return { name, error: errorMessage(cause) }
        }
      }),
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  })
  const contexts = prepared.isError ? undefined : prepared.data
  const expected =
    names.length === 1 ? names[0] : `${action.toUpperCase()} ${names.length}`
  const valid =
    contexts?.length === names.length &&
    contexts.every((entry) => entry.context?.allowed) &&
    !prepared.isFetching &&
    confirmation === expected &&
    Array.from(reason.trim()).length >= 2 &&
    Array.from(reason.trim()).length <= 300 &&
    !["\r", "\n", String.fromCharCode(0)].some((char) => reason.includes(char))
  const mutation = useMutation({
    retry: false,
    mutationFn: async () => {
      if (!valid || !contexts) throw new Error("Confirmation unavailable")
      onStart(contexts.map((entry) => entry.context!.operationId))
      setProgress(Object.fromEntries(names.map((name) => [name, "等待发送"])))
      return pooled(contexts, async ({ name, context }) => {
        setProgress((previous) => ({
          ...previous,
          [name]: "正在执行并核对状态…",
        }))
        let result: Operation
        try {
          result = operationSchema.parse(
            await api(
              `/clusters/${clusterId}/nodes/${encodeURIComponent(name)}/${action}`,
              {
                method: "POST",
                body: {
                  proof: context!.proof,
                  confirmation: name,
                  reason: reason.trim(),
                },
              }
            )
          )
        } catch (cause) {
          const rejected =
            cause instanceof ClientResponseError &&
            [400, 401, 403, 404, 409, 429].includes(cause.status)
          result = {
            operationId: context!.operationId,
            nodeName: name,
            action,
            status: rejected ? "failed" : "unknown",
            message: rejected
              ? `请求已被拒绝：${errorMessage(cause)}`
              : "未取得可靠响应，请查询操作记录并核对节点状态，勿重复提交。",
            reason: reason.trim(),
            states: [],
            createdAt: "",
            updatedAt: "",
          }
          if (!rejected) {
            try {
              result = operationSchema.parse(
                await api(
                  `/clusters/${clusterId}/node-operations/${context!.operationId}`
                )
              )
            } catch {
              /* A read failure never permits another POST. */
            }
          }
        }
        setProgress((previous) => ({ ...previous, [name]: result }))
        return result
      })
    },
    onSuccess: onResult,
    onError: (cause) => setError(errorMessage(cause)),
  })
  const submitted = mutation.isPending || mutation.isSuccess
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onClose()
      }}
    >
      <AlertDialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-2xl">
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <ShieldAlert className="text-warning size-5" />
            {actionLabel(action)} · {names.length} 个节点
          </AlertDialogTitle>
          <AlertDialogDescription>
            {action === "drain"
              ? "停止分配新作业，已有作业继续运行；DRAIN 标记不代表运行作业已全部结束。"
              : "恢复后可能立即调度排队作业。请先确认故障已处理、维护已完成。"}
            {names.length > 1 &&
              "逐节点执行，各节点独立记录结果，部分失败不会回滚其他节点。"}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {prepared.isPending && (
          <p role="status" className="flex gap-2 text-xs">
            <LoaderCircle className="size-4 animate-spin" />
            正在读取最新节点状态并校验权限…
          </p>
        )}
        <div className="max-h-72 space-y-3 overflow-y-auto">
          {contexts?.map((entry) => (
            <div
              key={entry.name}
              className="space-y-2 rounded-md border bg-muted/20 p-3 text-xs"
            >
              <p className="font-mono font-semibold break-all">{entry.name}</p>
              {entry.context && (
                <>
                  <div className="flex flex-wrap items-center gap-1">
                    <span className="mr-1 text-2xs text-muted-foreground">
                      确认时状态
                    </span>
                    {entry.context.node.states.map((state) => (
                      <SlurmStatusBadge key={state} kind="node" state={state} />
                    ))}
                  </div>
                  <p className="text-2xs leading-5 text-muted-foreground">
                    状态原因：{entry.context.node.reason || "—"} · 已分配{" "}
                    {entry.context.node.allocatedCpus} CPU
                  </p>
                </>
              )}
              {entry.error || !entry.context?.allowed ? (
                <p role="alert" className="text-2xs leading-5 text-destructive">
                  {entry.error || entry.context?.message}
                </p>
              ) : (
                !submitted && (
                  <p className="text-2xs text-muted-foreground">
                    确认有效至 {timestamp(entry.context.expiresAt)}
                  </p>
                )
              )}
              {!!progress[entry.name] && (
                <div role="status" className="border-t pt-2 text-2xs leading-6">
                  {typeof progress[entry.name] === "string"
                    ? (progress[entry.name] as string)
                    : (() => {
                        const result = progress[entry.name] as Operation
                        return (
                          <>
                            <p className="font-medium">
                              {operationLabel(result.status)}
                            </p>
                            <p>{result.message}</p>
                            <p className="font-mono break-all text-muted-foreground">
                              {result.operationId}
                            </p>
                          </>
                        )
                      })()}
                </div>
              )}
            </div>
          ))}
        </div>
        {!submitted && (
          <fieldset
            disabled={mutation.isPending || prepared.isFetching}
            className="space-y-4"
          >
            <div className="space-y-2">
              <Label htmlFor="node-control-reason">操作原因</Label>
              <Input
                id="node-control-reason"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                maxLength={600}
                autoComplete="off"
                placeholder="2–300 字单行原因，记录到审计"
              />
              <p className="text-2xs text-muted-foreground">
                {action === "drain"
                  ? "Drain 原因同时写入 Slurm 节点状态原因。"
                  : "Resume 原因保留在平台审计，Slurm 按自身规则清理状态原因。"}
              </p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="node-control-confirmation">
                输入 {expected} 以确认
              </Label>
              <Input
                id="node-control-confirmation"
                value={confirmation}
                onChange={(e) => setConfirmation(e.target.value)}
                autoComplete="off"
                className="font-mono"
                placeholder={expected}
              />
            </div>
          </fieldset>
        )}
        {error && (
          <p role="alert" className="text-xs text-destructive">
            {error}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            {submitted ? "关闭并查看记录" : "返回"}
          </AlertDialogCancel>
          {!submitted && (
            <>
              <Button
                variant="outline"
                disabled={prepared.isFetching}
                onClick={() => {
                  setConfirmation("")
                  setError("")
                  void prepared.refetch()
                }}
              >
                <RefreshCw />
                重新校验
              </Button>
              <Button
                variant={action === "drain" ? "destructive" : "default"}
                disabled={!valid}
                onClick={() => mutation.mutate()}
              >
                <ShieldAlert />
                确认{actionLabel(action)}
              </Button>
            </>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
