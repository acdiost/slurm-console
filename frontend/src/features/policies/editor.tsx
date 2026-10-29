import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { z } from "zod"
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
import { api } from "@/services/api"
import { emptySpec } from "@/features/accounting/schema"
import {
  confirmationSchema,
  qosFormSchema,
  policyPreparedSchema,
  policyOperationSchema,
  errorMessage,
  type Prepared,
  type QOS,
} from "./schema"

export function QOSEditor({
  clusterId,
  initial,
  onClose,
  onOperation,
}: {
  clusterId: string
  initial?: QOS
  onClose: () => void
  onOperation: (id: string) => void
}) {
  const [prepared, setPrepared] = useState<Prepared | null>(null),
    [attempted, setAttempted] = useState(false),
    client = useQueryClient()
  const action = initial ? "qos_update" : "qos_create",
    title = initial ? "编辑 QOS" : "创建 QOS"
  const initialNumber = (n: QOS["priority"] | undefined, fallback: string) =>
    n ? (n.value === null ? "" : String(n.value)) : fallback
  const form = useForm<z.infer<typeof qosFormSchema>>({
    resolver: zodResolver(qosFormSchema),
    defaultValues: {
      name: initial?.name || "",
      description: initial?.description || "",
      priority: initialNumber(initial?.priority, "0"),
      usageFactor: initialNumber(initial?.usageFactor, "1"),
      maxJobsPerUser: initialNumber(initial?.maxJobsPerUser, ""),
      maxSubmitJobsPerUser: initialNumber(initial?.maxSubmitJobsPerUser, ""),
      maxWallMinutes: initialNumber(initial?.maxWallMinutes, ""),
    },
  })
  const confirm = useForm<z.infer<typeof confirmationSchema>>({
    resolver: zodResolver(confirmationSchema),
    defaultValues: { confirmation: "", reason: "" },
  })
  const prepare = useMutation({
    retry: false,
    mutationFn: async (v: z.infer<typeof qosFormSchema>) => {
      const limit = (s: string) => (s === "" ? null : Number(s))
      return policyPreparedSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/accounting-controls/prepare`,
          {
            method: "POST",
            body: {
              action,
              spec: {
                ...emptySpec,
                qos: {
                  id: initial?.id || "",
                  name: v.name,
                  description: v.description,
                  priority: Number(v.priority),
                  usageFactor: Number(v.usageFactor),
                  maxJobsPerUser: limit(v.maxJobsPerUser),
                  maxSubmitJobsPerUser: limit(v.maxSubmitJobsPerUser),
                  maxWallMinutes: limit(v.maxWallMinutes),
                },
              },
            },
          }
        )
      )
    },
    onSuccess: setPrepared,
  })
  const execute = useMutation({
    retry: false,
    mutationFn: async (v: z.infer<typeof confirmationSchema>) => {
      if (!prepared || v.confirmation !== prepared.confirmation)
        throw new Error("confirmation")
      return policyOperationSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/accounting-controls/execute`,
          { method: "POST", body: { ...v, proof: prepared.proof } }
        )
      )
    },
    onSuccess: (result) => {
      void client.invalidateQueries({ queryKey: ["policies"] })
      void client.invalidateQueries({ queryKey: ["accounting"] })
      onOperation(result.operationId)
      onClose()
    },
  })
  const field = (key: keyof z.infer<typeof qosFormSchema>, label: string) => (
    <div className="space-y-1.5" key={key}>
      <Label htmlFor={`qos-${key}`}>{label}</Label>
      <Input
        id={`qos-${key}`}
        readOnly={key === "name" && !!initial}
        disabled={prepare.isPending || attempted}
        inputMode={
          key === "usageFactor"
            ? "decimal"
            : key !== "name" && key !== "description"
              ? "numeric"
              : undefined
        }
        {...form.register(key)}
      />
      {form.formState.errors[key] && (
        <p role="alert" className="text-xs text-destructive">
          {form.formState.errors[key]?.message}
        </p>
      )}
    </div>
  )
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !execute.isPending) onClose()
      }}
    >
      <AlertDialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-xl">
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>
            QOS 定义由共享 SlurmDBD 管理，可能影响多个集群中使用此 QOS 的作业。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <p className="border-warning/30 bg-warning/5 rounded-md border p-3 text-sm">
          此操作会设置表单中的全部字段。作业数和时限留空表示无限制，0
          保留为零限制。抢占、Flags 与 TRES 配额由现有配置决定。
        </p>
        {!attempted && (
          <form
            className="space-y-3"
            onChange={() => setPrepared(null)}
            onSubmit={form.handleSubmit((v) => {
              if (!initial && v.name !== v.name.toLowerCase()) {
                form.setError("name", { message: "新 QOS 请使用小写名称" })
                return
              }
              confirm.reset()
              prepare.mutate(v)
            })}
          >
            {field("name", "QOS 名称")}
            {field("description", "描述")}
            <div className="grid gap-3 sm:grid-cols-2">
              {field("priority", "QOS 优先级")}
              {field("usageFactor", "用量计费因子")}
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              {field("maxJobsPerUser", "每用户运行作业上限")}
              {field("maxSubmitJobsPerUser", "每用户提交作业上限")}
            </div>
            {field("maxWallMinutes", "单作业时限（分钟）")}
            <Button
              type="submit"
              variant="outline"
              disabled={prepare.isPending}
            >
              {prepare.isPending ? "正在校验…" : "校验最新 QOS 状态"}
            </Button>
          </form>
        )}
        {prepare.isError && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(prepare.error)}
          </p>
        )}
        {prepared && (
          <div role="status" className="rounded-md border p-3 text-sm">
            <p>{prepared.message}</p>
            <p className="mt-1 text-muted-foreground">
              连接的 Slurm 集群：{prepared.slurmCluster}；变更范围：共享记账库。
            </p>
            {prepared.allowed && (
              <p className="mt-1 text-muted-foreground">
                确认有效至{" "}
                {new Date(prepared.expiresAt).toLocaleTimeString("zh-CN", {
                  hour12: false,
                })}
              </p>
            )}
          </div>
        )}
        {prepared?.allowed && (
          <form
            className="space-y-3"
            onSubmit={confirm.handleSubmit((v) => {
              if (v.confirmation !== prepared.confirmation) {
                confirm.setError("confirmation", {
                  message: "确认文本与 QOS 名称不一致",
                })
                return
              }
              setAttempted(true)
              onOperation(prepared.operationId)
              execute.mutate(v)
            })}
          >
            <div className="space-y-1.5">
              <Label htmlFor="qos-reason">操作原因</Label>
              <Input
                id="qos-reason"
                disabled={attempted}
                {...confirm.register("reason")}
              />
              {confirm.formState.errors.reason && (
                <p role="alert" className="text-xs text-destructive">
                  {confirm.formState.errors.reason.message}
                </p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="qos-confirm">
                输入 {prepared.confirmation} 确认
              </Label>
              <Input
                id="qos-confirm"
                autoComplete="off"
                disabled={attempted}
                {...confirm.register("confirmation")}
              />
              {confirm.formState.errors.confirmation && (
                <p role="alert" className="text-xs text-destructive">
                  {confirm.formState.errors.confirmation.message}
                </p>
              )}
            </div>
            <Button type="submit" disabled={attempted}>
              {execute.isPending
                ? "正在执行…"
                : attempted
                  ? "已提交，请核实结果"
                  : `确认${title}`}
            </Button>
          </form>
        )}
        {execute.isError && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(execute.error)} 操作编号：{prepared?.operationId}
            。关闭后可只读核实结果。
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={execute.isPending} onClick={onClose}>
            关闭
          </AlertDialogCancel>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
