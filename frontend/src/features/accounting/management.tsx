import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { z } from "zod"
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api } from "@/services/api"
import {
  actions,
  emptySpec,
  errorMessage,
  formSchema,
  preparedSchema,
  operationSchema,
  type Action,
  type Prepared,
  type Spec,
} from "./schema"
const confirmationSchema = z.object({
  confirmation: z.string().min(1, "请输入确认文本"),
  reason: z
    .string()
    .trim()
    .min(2, "至少 2 字")
    .max(300, "最多 300 字")
    .regex(/^[^\r\n\0]*$/, "请使用单行原因"),
})
export function AccountingManagement({
  clusterId,
  action,
  initial,
  onClose,
  onOperation,
}: {
  clusterId: string
  action: Action
  initial?: Partial<Spec>
  onClose: () => void
  onOperation: (id: string) => void
}) {
  const [prepared, setPrepared] = useState<Prepared | null>(null),
    [attempted, setAttempted] = useState(false),
    client = useQueryClient()
  const form = useForm<Spec>({
    resolver: zodResolver(formSchema(action)),
    defaultValues: { ...emptySpec, ...initial },
  })
  const confirm = useForm<z.infer<typeof confirmationSchema>>({
    resolver: zodResolver(confirmationSchema),
    defaultValues: { confirmation: "", reason: "" },
  })
  const prepare = useMutation({
    mutationFn: async (spec: Spec) =>
      preparedSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/accounting-controls/prepare`,
          { method: "POST", body: { action, spec } }
        )
      ),
    retry: false,
    onSuccess: setPrepared,
  })
  const execute = useMutation({
    mutationFn: async (input: z.infer<typeof confirmationSchema>) => {
      if (!prepared || input.confirmation !== prepared.confirmation)
        throw new Error("confirmation")
      return operationSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/accounting-controls/execute`,
          { method: "POST", body: { ...input, proof: prepared.proof } }
        )
      )
    },
    retry: false,
    onSuccess: (result) => {
      void client.invalidateQueries({ queryKey: ["accounting"] })
      onOperation(result.operationId)
      onClose()
    },
  })
  const field = (key: keyof Spec, label: string, disabled = false) => (
    <div className="space-y-1.5" key={key}>
      <Label htmlFor={`accounting-${key}`}>{label}</Label>
      <Input
        id={`accounting-${key}`}
        readOnly={disabled}
        disabled={prepare.isPending || attempted}
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
          <AlertDialogTitle>{actions[action]}</AlertDialogTitle>
          <AlertDialogDescription>
            修改 Slurm
            记账身份与关联。每次操作需校验最新状态、填写原因和确认文本。
          </AlertDialogDescription>
        </AlertDialogHeader>
        {action.startsWith("account_") || action === "user_create" ? (
          <p className="border-warning/30 bg-warning/5 rounded-md border p-3 text-sm">
            Account 描述、组织和 Slurm
            用户身份属于共享记账库的全局信息，可能在其他集群可见。此操作需要平台记账管理权限。
          </p>
        ) : (
          <p className="text-sm text-muted-foreground">
            Association
            仅作用于当前集群。默认关联、最后一个关联及存在未结束作业的关联受保护。
          </p>
        )}
        {!attempted && (
          <form
            className="space-y-3"
            onChange={() => setPrepared(null)}
            onSubmit={form.handleSubmit((v) => {
              confirm.reset()
              prepare.mutate(v)
            })}
          >
            {field(
              "account",
              "Account",
              action === "account_update" || action === "association_remove"
            )}
            {action.startsWith("account_") ? (
              <>
                {field("description", "描述")}
                {field("organization", "组织")}
              </>
            ) : (
              field("user", "Slurm 用户", action === "association_remove")
            )}
            {action === "association_create" || action === "association_remove"
              ? field(
                  "partition",
                  "分区（留空表示通用关联）",
                  action === "association_remove"
                )
              : null}
            {action === "association_remove" &&
              field("associationId", "Association ID", true)}
            {action === "user_create" && (
              <p className="text-xs text-muted-foreground">
                新用户为普通 Slurm 用户，以上 Account 为默认账户，同时建立通用
                Association。此操作不会创建 Linux 或平台登录账户。
              </p>
            )}
            <Button
              type="submit"
              variant="outline"
              disabled={prepare.isPending}
            >
              {prepare.isPending ? "正在校验…" : "校验最新记账状态"}
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
            {prepared.slurmCluster && (
              <p className="mt-1 text-muted-foreground">
                Slurm 集群：
                <span className="font-mono">{prepared.slurmCluster}</span>
              </p>
            )}
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
            onSubmit={confirm.handleSubmit((input) => {
              if (input.confirmation !== prepared.confirmation) {
                confirm.setError("confirmation", {
                  message: "确认文本与目标不一致",
                })
                return
              }
              setAttempted(true)
              onOperation(prepared.operationId)
              execute.mutate(input)
            })}
          >
            <div className="space-y-1.5">
              <Label htmlFor="accounting-reason">操作原因</Label>
              <Input
                id="accounting-reason"
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
              <Label htmlFor="accounting-confirm">
                输入 {prepared.confirmation} 确认
              </Label>
              <Input
                id="accounting-confirm"
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
            <Button
              type="submit"
              variant={
                action === "association_remove" ? "destructive" : "default"
              }
              disabled={attempted}
            >
              {execute.isPending
                ? "正在执行…"
                : attempted
                  ? "已提交，请核实结果"
                  : `确认${actions[action]}`}
            </Button>
          </form>
        )}
        {execute.isError && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(execute.error)} 操作编号：{prepared?.operationId}
            。关闭后可在操作记录中只读核实。
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
