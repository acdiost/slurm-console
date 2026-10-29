import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
} from "@/components/ui/alert-dialog"
import { api } from "@/services/api"
import {
  type Action,
  type Prepared,
  type Reservation,
  actions,
  localInput,
  dateTime,
  timezone,
  reservationPreparedSchema,
  reservationOperationSchema,
  errorMessage,
} from "./schema"
export function ReservationEditor({
  clusterId,
  action,
  target,
  onClose,
  onOperation,
}: {
  clusterId: string
  action: Action
  target?: Reservation
  onClose: () => void
  onOperation: (id: string) => void
}) {
  const [fields, setFields] = useState(() => {
    const now = Math.ceil(Date.now() / 60000) * 60 + 3600
    return {
      name: target?.name || "",
      start: localInput(target?.startTime ?? now),
      end: localInput(target?.endTime ?? now + 3600),
      nodes: "",
      users: "",
      accounts: "",
      maintenance: false,
    }
  })
  const [prepared, setPrepared] = useState<Prepared | null>(null),
    [reason, setReason] = useState(""),
    [confirmation, setConfirmation] = useState(""),
    [attempted, setAttempted] = useState(false),
    [validation, setValidation] = useState(""),
    client = useQueryClient()
  const change = (key: keyof typeof fields, value: string | boolean) => {
    setFields({ ...fields, [key]: value })
    setPrepared(null)
    setValidation("")
    setConfirmation("")
  }
  const list = (s: string) => s.split(/[\s,]+/).filter(Boolean)
  const spec = () => ({
    name: fields.name,
    startTime:
      action === "reservation_create"
        ? Math.floor(new Date(fields.start).getTime() / 1000)
        : 0,
    endTime:
      action === "reservation_delete"
        ? 0
        : Math.floor(new Date(fields.end).getTime() / 1000),
    nodes: action === "reservation_create" ? list(fields.nodes) : [],
    users: action === "reservation_create" ? list(fields.users) : [],
    accounts: action === "reservation_create" ? list(fields.accounts) : [],
    maintenance: action === "reservation_create" && fields.maintenance,
  })
  const prepare = useMutation({
    retry: false,
    mutationFn: async () =>
      reservationPreparedSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/reservation-controls/prepare`,
          { method: "POST", body: { action, spec: spec() } }
        )
      ),
    onSuccess: setPrepared,
  })
  const execute = useMutation({
    retry: false,
    mutationFn: async () => {
      if (!prepared) throw new Error("confirmation")
      return reservationOperationSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/reservation-controls/execute`,
          {
            method: "POST",
            body: { proof: prepared.proof, reason, confirmation },
          }
        )
      )
    },
    onSuccess: (r) => {
      void client.invalidateQueries({ queryKey: ["reservations"] })
      void client.invalidateQueries({ queryKey: ["nodes"] })
      onOperation(r.operationId)
      onClose()
    },
  })
  const busy = prepare.isPending || execute.isPending
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose()
      }}
    >
      <AlertDialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-xl">
        <AlertDialogHeader>
          <AlertDialogTitle>{actions[action]}</AlertDialogTitle>
          <AlertDialogDescription>
            {target?.name || "为指定用户或 Account 保留明确节点。"}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <p className="border-warning/30 bg-warning/5 rounded-md border p-3 text-sm">
          预留会影响节点可调度时间。时间使用 {timezone}
          。调整和删除前会核对未结束作业，结果未知时只读核实。
        </p>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            setPrepared(null)
            setValidation("")
            const v = spec()
            if (
              !v.name ||
              !Number.isFinite(v.endTime) ||
              (action === "reservation_create" &&
                (!Number.isFinite(v.startTime) ||
                  !v.nodes.length ||
                  (!v.users.length && !v.accounts.length)))
            ) {
              setValidation(
                "请填写名称、有效时间、明确节点，以及至少一种访问名单。"
              )
              return
            }
            prepare.mutate()
          }}
        >
          {action === "reservation_create" ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="res-name">预留名称</Label>
                <Input
                  id="res-name"
                  required
                  maxLength={64}
                  value={fields.name}
                  onChange={(e) => change("name", e.target.value)}
                  disabled={busy || attempted}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="res-start">开始时间</Label>
                <Input
                  id="res-start"
                  type="datetime-local"
                  step="1"
                  required
                  value={fields.start}
                  onChange={(e) => change("start", e.target.value)}
                  disabled={busy || attempted}
                />
                <p className="text-xs text-muted-foreground">
                  一分钟后、一年内；窗口不超过三十天。
                </p>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="res-nodes">节点名称</Label>
                <Input
                  id="res-nodes"
                  required
                  placeholder="node001,node002"
                  value={fields.nodes}
                  onChange={(e) => change("nodes", e.target.value)}
                  disabled={busy || attempted}
                />
                <p className="text-xs text-muted-foreground">
                  逐一填写节点，用逗号或空格分隔；不接受 ALL 或范围表达式。
                </p>
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label htmlFor="res-users">允许的 Linux 用户</Label>
                  <Input
                    id="res-users"
                    value={fields.users}
                    onChange={(e) => change("users", e.target.value)}
                    disabled={busy || attempted}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="res-accounts">允许的 Slurm Account</Label>
                  <Input
                    id="res-accounts"
                    value={fields.accounts}
                    onChange={(e) => change("accounts", e.target.value)}
                    disabled={busy || attempted}
                  />
                </div>
              </div>
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={fields.maintenance}
                  onChange={(e) => change("maintenance", e.target.checked)}
                  disabled={busy || attempted}
                />
                维护预留（MAINT），允许与已有预留重叠；请核对影响范围。
              </label>
            </>
          ) : (
            <p className="text-sm">
              {target?.nodes} · {dateTime(target?.startTime ?? null)} 至{" "}
              {dateTime(target?.endTime ?? null)}
              {action === "reservation_delete"
                ? "。删除后释放此预留；不会取消作业。"
                : "。本次只调整结束时间，其他配置保留。"}
            </p>
          )}
          {action !== "reservation_delete" && (
            <div className="space-y-1.5">
              <Label htmlFor="res-end">结束时间</Label>
              <Input
                id="res-end"
                type="datetime-local"
                step="1"
                required
                value={fields.end}
                onChange={(e) => change("end", e.target.value)}
                disabled={busy || attempted}
              />
            </div>
          )}
          {!attempted && (
            <Button type="submit" disabled={busy}>
              校验最新预留
            </Button>
          )}
        </form>
        {validation && (
          <p role="alert" className="text-sm text-destructive">
            {validation}
          </p>
        )}
        {prepare.isError && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(prepare.error)}
          </p>
        )}
        {prepared && (
          <p className="rounded-md border p-3 text-sm">
            {prepared.message}
            {prepared.allowed && (
              <>
                {" "}
                集群：{prepared.slurmCluster}；{prepared.spec.reservation.name}
                ，
                {dateTime(
                  prepared.spec.reservation.startTime ||
                    target?.startTime ||
                    null
                )}{" "}
                至{" "}
                {action === "reservation_delete"
                  ? "删除"
                  : dateTime(prepared.spec.reservation.endTime)}
                。
                {prepared.spec.reservation.maintenance
                  ? "维护模式 MAINT。"
                  : ""}
                {prepared.spec.reservation.nodes?.join(", ")}
                {prepared.spec.reservation.users?.length
                  ? `；用户：${prepared.spec.reservation.users.join(", ")}`
                  : ""}
                {prepared.spec.reservation.accounts?.length
                  ? `；Account：${prepared.spec.reservation.accounts.join(", ")}`
                  : ""}
              </>
            )}
          </p>
        )}
        {prepared?.allowed && !attempted && (
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault()
              if (
                confirmation !== prepared.confirmation ||
                reason.trim().length < 2
              ) {
                setValidation("请填写原因并精确输入预留名称。")
                return
              }
              setAttempted(true)
              execute.mutate()
            }}
          >
            <div className="space-y-1.5">
              <Label htmlFor="res-reason">操作原因</Label>
              <Input
                id="res-reason"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                required
                minLength={2}
                maxLength={300}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="res-confirm">
                输入 {prepared.confirmation} 确认
              </Label>
              <Input
                id="res-confirm"
                value={confirmation}
                onChange={(e) => setConfirmation(e.target.value)}
                autoComplete="off"
                required
              />
            </div>
            <Button
              type="submit"
              variant={
                action === "reservation_delete" ? "destructive" : "default"
              }
              disabled={busy}
            >
              确认{actions[action]}
            </Button>
          </form>
        )}
        {execute.isError && (
          <div role="alert" className="space-y-2 text-sm">
            <p>{errorMessage(execute.error)}</p>
            <p>写请求可能已发送，请查询原操作；不要重新创建确认。</p>
            <Button
              variant="outline"
              onClick={() => {
                if (prepared) onOperation(prepared.operationId)
                onClose()
              }}
            >
              只读核实本次结果
            </Button>
          </div>
        )}
        <div className="flex justify-end">
          <Button variant="outline" disabled={busy} onClick={onClose}>
            关闭
          </Button>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  )
}
