import { useState } from "react"
import { useForm, useWatch } from "react-hook-form"
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
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { api } from "@/services/api"
import { emptySpec } from "@/features/accounting/schema"
import { confirmationSchema } from "@/features/policies/schema"
import {
  buckets,
  editSchema,
  errorMessage,
  targetName,
  limitText,
  unit,
  tresPreparedSchema,
  tresOperationSchema,
  type Prepared,
  type Target,
  type Resource,
} from "./schema"

export function TRESEditor({
  clusterId,
  target,
  resources,
  initialBucket,
  onClose,
  onOperation,
}: {
  clusterId: string
  target: Target
  resources: Resource[]
  initialBucket: string
  onClose: () => void
  onOperation: (id: string) => void
}) {
  const [prepared, setPrepared] = useState<Prepared | null>(null),
    [attempted, setAttempted] = useState(false),
    client = useQueryClient()
  const first = resources.find((r) => r.editable)?.id || "",
    existing = target.limits[initialBucket]?.find((v) => v.id === first)
  const form = useForm<z.infer<typeof editSchema>>({
    resolver: zodResolver(editSchema),
    defaultValues: {
      resourceId: first,
      bucket: initialBucket,
      mode: "set",
      value: existing?.state === "finite" ? existing.value : "",
    },
  })
  const resourceId = useWatch({ control: form.control, name: "resourceId" }),
    bucket = useWatch({ control: form.control, name: "bucket" }),
    mode = useWatch({ control: form.control, name: "mode" })
  const r = resources.find((v) => v.id === resourceId),
    current = target.limits[bucket]?.find((v) => v.id === resourceId)
  const confirm = useForm<z.infer<typeof confirmationSchema>>({
    resolver: zodResolver(confirmationSchema),
    defaultValues: { confirmation: "", reason: "" },
  })
  const prepare = useMutation({
    retry: false,
    mutationFn: async (v: z.infer<typeof editSchema>) =>
      tresPreparedSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/accounting-controls/prepare`,
          {
            method: "POST",
            body: {
              action:
                target.kind === "qos"
                  ? "qos_tres_update"
                  : "association_tres_update",
              spec: {
                ...emptySpec,
                tres: {
                  kind: target.kind,
                  targetId: target.id,
                  name: target.name,
                  account: target.account,
                  user: target.user,
                  partition: target.partition,
                  ...v,
                  value: v.mode === "clear" ? "" : v.value,
                },
              },
            },
          }
        )
      ),
    onSuccess: setPrepared,
  })
  const execute = useMutation({
    retry: false,
    mutationFn: async (v: z.infer<typeof confirmationSchema>) => {
      if (!prepared || v.confirmation !== prepared.confirmation)
        throw new Error("confirmation")
      return tresOperationSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/accounting-controls/execute`,
          { method: "POST", body: { ...v, proof: prepared.proof } }
        )
      )
    },
    onSuccess: (result) => {
      for (const key of ["tres", "policies", "accounting"])
        void client.invalidateQueries({ queryKey: [key] })
      onOperation(result.operationId)
      onClose()
    },
  })
  const change = (key: "resourceId" | "bucket" | "mode", value: string) => {
    setPrepared(null)
    form.setValue(key, value as "set" | "clear")
    const id = key === "resourceId" ? value : resourceId,
      b = key === "bucket" ? value : bucket,
      entry = target.limits[b]?.find((v) => v.id === id)
    if (key !== "mode")
      form.setValue("value", entry?.state === "finite" ? entry.value : "")
  }
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !execute.isPending) onClose()
      }}
    >
      <AlertDialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-xl">
        <AlertDialogHeader>
          <AlertDialogTitle>配置 TRES 限额</AlertDialogTitle>
          <AlertDialogDescription>
            {targetName(target)} · #{target.id}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <p className="border-warning/30 bg-warning/5 rounded-md border p-3 text-sm">
          {target.kind === "qos"
            ? "共享 QOS 修改可能影响多个集群的作业。"
            : "关联限额适用于当前集群；Account 层级的组限额也约束其下级关联。"}{" "}
          清除表示移除本层设置，后续按 Slurm 继承及 QOS、分区规则生效。
        </p>
        {!attempted && (
          <form
            className="space-y-3"
            onChange={() => setPrepared(null)}
            onSubmit={form.handleSubmit((v) => {
              if (
                target.relative &&
                v.mode === "set" &&
                !buckets[v.bucket]?.minutes &&
                BigInt(v.value) > 100n
              ) {
                form.setError("value", {
                  message: "Relative QOS 使用 0–100 的整数百分比",
                })
                return
              }
              confirm.reset()
              prepare.mutate(v)
            })}
          >
            <div className="space-y-1.5">
              <Label htmlFor="tres-resource">资源</Label>
              <Select
                value={resourceId}
                disabled={prepare.isPending}
                onValueChange={(v) => change("resourceId", v)}
              >
                <SelectTrigger id="tres-resource">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {resources
                    .filter((r) => r.editable)
                    .map((r) => (
                      <SelectItem key={r.id} value={r.id}>
                        {r.key} · {r.unit}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="tres-bucket">限额类型</Label>
              <Select
                value={bucket}
                disabled={prepare.isPending}
                onValueChange={(v) => change("bucket", v)}
              >
                <SelectTrigger id="tres-bucket">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(buckets)
                    .filter(([, b]) => b[target.kind])
                    .map(([id, b]) => (
                      <SelectItem key={id} value={id}>
                        {b.label}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {buckets[bucket]?.description}
              </p>
            </div>
            <p className="text-sm text-muted-foreground">
              当前：{limitText(current)}
              {current?.state === "finite" ? ` ${unit(target, r, bucket)}` : ""}
            </p>
            <div className="space-y-1.5">
              <Label htmlFor="tres-mode">操作方式</Label>
              <Select
                value={mode}
                disabled={prepare.isPending}
                onValueChange={(v) => change("mode", v)}
              >
                <SelectTrigger id="tres-mode">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="set">设置本层数值</SelectItem>
                  <SelectItem value="clear">清除本层设置</SelectItem>
                </SelectContent>
              </Select>
            </div>
            {mode === "set" && (
              <div className="space-y-1.5">
                <Label htmlFor="tres-value">
                  限额（{unit(target, r, bucket)}）
                </Label>
                <Input
                  id="tres-value"
                  inputMode="numeric"
                  disabled={prepare.isPending}
                  {...form.register("value")}
                />
                <p className="text-xs text-muted-foreground">
                  0 按数值零提交。内存使用 MiB，分钟额度使用对应资源单位乘分钟。
                </p>
                {form.formState.errors.value && (
                  <p role="alert" className="text-xs text-destructive">
                    {form.formState.errors.value.message}
                  </p>
                )}
              </div>
            )}
            <Button
              type="submit"
              variant="outline"
              disabled={prepare.isPending || !r}
            >
              {prepare.isPending ? "正在校验…" : "校验最新限额"}
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
              Slurm 集群：{prepared.slurmCluster} ·{" "}
              {prepared.spec.tres.mode === "clear"
                ? "清除本层设置"
                : `${prepared.spec.tres.value} ${unit(target, r, prepared.spec.tres.bucket)}`}
            </p>
          </div>
        )}
        {prepared?.allowed && (
          <form
            className="space-y-3"
            onSubmit={confirm.handleSubmit((v) => {
              if (v.confirmation !== prepared.confirmation) {
                confirm.setError("confirmation", {
                  message: "确认文本与目标不一致",
                })
                return
              }
              setAttempted(true)
              onOperation(prepared.operationId)
              execute.mutate(v)
            })}
          >
            <div className="space-y-1.5">
              <Label htmlFor="tres-reason">操作原因</Label>
              <Input
                id="tres-reason"
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
              <Label htmlFor="tres-confirm">
                输入 {prepared.confirmation} 确认
              </Label>
              <Input
                id="tres-confirm"
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
                  : "确认配置限额"}
            </Button>
          </form>
        )}
        {execute.isError && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(execute.error)} 操作编号：{prepared?.operationId}
            ；请关闭后只读核实。
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
