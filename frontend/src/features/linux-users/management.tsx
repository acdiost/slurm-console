import { useEffect, useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Plus, LockKeyhole, UnlockKeyhole } from "lucide-react"
import {
  AlertDialog as Dialog,
  AlertDialogContent as DialogContent,
  AlertDialogHeader as DialogHeader,
  AlertDialogTitle as DialogTitle,
  AlertDialogDescription as DialogDescription,
  AlertDialogCancel,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { api } from "@/services/api"
import { timestamp } from "@/features/overview/schema"
import { linuxUserSchema, errorMessage } from "./schema"
const actionLabels: Record<string, string> = {
  create: "创建",
  lock: "锁定",
  unlock: "解锁",
}
const singleLine = (value: string) =>
  Array.from(value).every((char) => {
    const code = char.codePointAt(0)!
    return code >= 32 && (code < 127 || code > 159)
  })
const capabilitySchema = z.object({
  enabled: z.boolean(),
  actions: z.array(z.string()),
  minUid: z.number(),
  maxUid: z.number(),
  message: z.string(),
  recentOperations: z
    .array(
      z.object({
        operationId: z.string(),
        name: z.string(),
        action: z.string(),
        status: z.string(),
      })
    )
    .default([]),
})
const specSchema = z.object({
  name: z
    .string()
    .regex(
      /^[a-z_][a-z0-9_-]{0,31}$/,
      "使用小写字母、数字、下划线或短横线，不能以数字开头"
    ),
  uid: z.number().int().min(1000).max(60000),
  comment: z
    .string()
    .max(100)
    .refine((v) => !v.includes(":") && singleLine(v), "描述不能包含冒号或换行"),
  shell: z.enum(["/bin/bash", "/sbin/nologin"]),
})
type Spec = z.infer<typeof specSchema>
const preparedSchema = z.object({
  allowed: z.boolean(),
  message: z.string(),
  proof: z.string(),
  operationId: z.string(),
  expiresAt: z.string(),
  action: z.string(),
  spec: z.object({
    name: z.string(),
    uid: z.number(),
    comment: z.string(),
    shell: z.string(),
  }),
  account: linuxUserSchema.nullable(),
  expiry: z.number().nullable(),
  minUid: z.number(),
  maxUid: z.number(),
})
const operationSchema = z.object({
  operationId: z.string(),
  name: z.string(),
  action: z.string(),
  status: z.string(),
  message: z.string(),
  createdAt: z.string(),
  updatedAt: z.string(),
})
const confirmationSchema = z.object({
  confirmation: z.string(),
  reason: z
    .string()
    .trim()
    .min(2, "至少填写 2 个字的操作原因")
    .max(300)
    .refine((v) => singleLine(v), "原因必须为单行"),
})
export function OperationPanel({
  clusterId,
  operationId,
}: {
  clusterId: string
  operationId: string
}) {
  const query = useQuery({
    queryKey: ["linux-user-operation", clusterId, operationId],
    enabled: !!operationId,
    queryFn: async ({ signal }) =>
      operationSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/linux-user-operations/${encodeURIComponent(operationId)}`,
          { signal }
        )
      ),
    retry: false,
    refetchOnWindowFocus: false,
  })
  if (!operationId) return null
  const data = query.isError ? undefined : query.data
  return (
    <section className="panel space-y-3 p-4" aria-label="本地账户操作结果">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-semibold">本地账户操作结果</h2>
        <Button
          variant="outline"
          size="sm"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          查询操作结果
        </Button>
      </div>
      <p className="font-mono text-2xs break-all">{operationId}</p>
      {query.isPending && <p role="status">正在查询操作记录…</p>}
      {query.isError && (
        <p role="alert" className="text-xs text-destructive">
          尚未读到此操作结果，请稍后查询并核实权限；不要重复提交写请求。
        </p>
      )}
      {data && (
        <>
          <p className="text-sm">
            {data.name} · {actionLabels[data.action] || data.action} ·{" "}
            {data.status === "confirmed"
              ? "已核实"
              : data.status === "failed"
                ? "未执行/失败"
                : "结果待核实"}
          </p>
          <p className="text-xs leading-6 text-muted-foreground">
            {data.message}
          </p>
        </>
      )}
    </section>
  )
}
export function LinuxUserManagement({
  clusterId,
  target,
  operationId,
  onOperation,
}: {
  clusterId: string
  target?: string
  operationId: string
  onOperation: (id: string) => void
}) {
  const cap = useQuery({
    queryKey: ["linux-user-capabilities", clusterId],
    queryFn: async ({ signal }) =>
      capabilitySchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/linux-user-controls`,
          { signal }
        )
      ),
    retry: false,
    staleTime: 0,
  })
  const [action, setAction] = useState("")
  const data = cap.isError ? undefined : cap.data
  return (
    <>
      <section className="panel space-y-3 p-4" aria-label="本地账户管理">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">本地账户管理</h2>
            <p className="mt-1 text-2xs leading-5 text-muted-foreground">
              {data?.message ||
                (cap.isError
                  ? "主机写入能力暂不可用；用户查询仍可使用。"
                  : "正在检查本地账户管理能力…")}
            </p>
            {data?.enabled && (
              <p className="mt-1 text-2xs text-muted-foreground">
                主机允许的 UID 范围：{data.minUid}–{data.maxUid}
              </p>
            )}
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              variant="ghost"
              disabled={cap.isFetching}
              onClick={() => void cap.refetch()}
            >
              刷新管理能力
            </Button>
            {data?.enabled && data.actions.includes("create") && !target && (
              <Button size="sm" onClick={() => setAction("create")}>
                <Plus />
                创建本地账户
              </Button>
            )}
            {data?.enabled && target && data.actions.includes("lock") && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => setAction("lock")}
              >
                <LockKeyhole />
                锁定 {target}
              </Button>
            )}
            {data?.enabled && target && data.actions.includes("unlock") && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => setAction("unlock")}
              >
                <UnlockKeyhole />
                解锁 {target}
              </Button>
            )}
          </div>
        </div>
        {!!data?.recentOperations.length && (
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-2xs text-muted-foreground">我最近的操作</span>
            {data.recentOperations.map((op) => (
              <Button
                key={op.operationId}
                size="sm"
                variant="ghost"
                onClick={() => onOperation(op.operationId)}
              >
                {op.name} · {actionLabels[op.action] || op.action} ·{" "}
                {op.status === "confirmed"
                  ? "已核实"
                  : op.status === "failed"
                    ? "失败"
                    : "待核实"}
              </Button>
            ))}
          </div>
        )}
        {data?.enabled && !target && (
          <p className="text-2xs text-muted-foreground">
            打开用户详情可查看该账户的锁定和解锁入口。
          </p>
        )}
      </section>
      {action && (
        <AccountDialog
          key={clusterId + action + target}
          clusterId={clusterId}
          action={action}
          name={target || ""}
          minimum={data?.minUid || 10000}
          onClose={() => setAction("")}
          onOperation={onOperation}
        />
      )}
      <OperationPanel clusterId={clusterId} operationId={operationId} />
    </>
  )
}
export function AccountDialog({
  clusterId,
  action,
  name,
  minimum,
  onClose,
  onOperation,
}: {
  clusterId: string
  action: string
  name: string
  minimum: number
  onClose: () => void
  onOperation: (id: string) => void
}) {
  const client = useQueryClient(),
    [prepared, setPrepared] = useState<z.infer<typeof preparedSchema> | null>(
      null
    ),
    [submitted, setSubmitted] = useState(false),
    [now, setNow] = useState(() => Date.now())
  const title =
    action === "create"
      ? "创建本地账户"
      : action === "lock"
        ? "锁定本地账户"
        : "解锁本地账户"
  const form = useForm<Spec>({
    resolver: zodResolver(specSchema),
    defaultValues: { name, uid: minimum, comment: "", shell: "/sbin/nologin" },
  })
  const shell = useWatch({ control: form.control, name: "shell" })
  const confirm = useForm<z.infer<typeof confirmationSchema>>({
    resolver: zodResolver(confirmationSchema),
    defaultValues: { reason: "", confirmation: "" },
  })
  const typed = useWatch({ control: confirm.control, name: "confirmation" })
  const prepare = useMutation({
    retry: false,
    mutationFn: async (spec: Partial<Spec>) =>
      preparedSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/linux-user-controls/prepare`,
          { method: "POST", body: { action, spec } }
        )
      ),
    onSuccess: (value) => {
      setPrepared(value)
      confirm.reset({ reason: "", confirmation: "" })
      setNow(Date.now())
    },
  })
  const execute = useMutation({
    retry: false,
    mutationFn: async (values: z.infer<typeof confirmationSchema>) => {
      if (!prepared?.allowed) throw Error("missing confirmation")
      setSubmitted(true)
      onOperation(prepared.operationId)
      return operationSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/linux-user-controls/execute`,
          { method: "POST", body: { ...values, proof: prepared.proof } }
        )
      )
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["linux-users", clusterId] })
      void client.invalidateQueries({
        queryKey: ["linux-user-capabilities", clusterId],
      })
      void client.invalidateQueries({
        queryKey: ["linux-user-detail", clusterId],
      })
      void client.invalidateQueries({
        queryKey: ["linux-user-operation", clusterId],
      })
    },
  })
  useEffect(() => {
    if (!prepared) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [prepared])
  const expired = prepared && now >= Date.parse(prepared.expiresAt)
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !execute.isPending) onClose()
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            操作作用于身份主机的本地账户，可能影响使用同一身份的其他系统。目录账户及受保护账户不允许修改。
          </DialogDescription>
        </DialogHeader>
        {submitted ? (
          <div className="space-y-3">
            <p className="text-sm">
              {execute.isPending
                ? "正在执行，请勿重复提交…"
                : execute.isError
                  ? "请求结果不确定，请查询原操作结果；不要再次发送。"
                  : execute.data?.status === "confirmed"
                    ? "主机已核实结果，审计记录已保存。"
                    : execute.data?.status === "failed"
                      ? "请求未执行或失败，请查看原操作记录。"
                      : "请求结果待核实，请查询原操作记录。"}
            </p>
            {prepared && (
              <OperationPanel
                clusterId={clusterId}
                operationId={prepared.operationId}
              />
            )}
            <Button
              variant="outline"
              disabled={execute.isPending}
              onClick={onClose}
            >
              关闭并保留操作记录
            </Button>
          </div>
        ) : (
          <>
            {!prepared &&
              (action === "create" ? (
                <form
                  className="space-y-4"
                  onSubmit={form.handleSubmit((v) => prepare.mutate(v))}
                  noValidate
                >
                  <div className="space-y-2">
                    <Label htmlFor="local-name">本地用户名</Label>
                    <Input
                      id="local-name"
                      autoComplete="off"
                      {...form.register("name")}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="local-uid">UID</Label>
                    <Input
                      id="local-uid"
                      type="number"
                      {...form.register("uid", { valueAsNumber: true })}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="local-comment">账户描述</Label>
                    <Input id="local-comment" {...form.register("comment")} />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="local-shell">登录 Shell</Label>
                    <Select
                      value={shell}
                      onValueChange={(v) =>
                        form.setValue("shell", v as Spec["shell"])
                      }
                    >
                      <SelectTrigger id="local-shell" className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="/sbin/nologin">
                          /sbin/nologin
                        </SelectItem>
                        <SelectItem value="/bin/bash">/bin/bash</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <p className="text-xs leading-6 text-muted-foreground">
                    自动创建同名主组及 /home/用户名，主目录权限
                    0700。初始账户到期锁定且没有可用密码，凭据由系统管理员配置；解锁不会设置密码。
                  </p>
                  {Object.entries(form.formState.errors).map(([key, error]) => (
                    <p
                      role="alert"
                      key={key}
                      className="text-xs text-destructive"
                    >
                      {error.message}
                    </p>
                  ))}
                  <Button type="submit" disabled={prepare.isPending}>
                    校验创建条件
                  </Button>
                </form>
              ) : (
                <div className="space-y-3">
                  <p className="font-mono">{name}</p>
                  <Button
                    disabled={prepare.isPending}
                    onClick={() => prepare.mutate({ name })}
                  >
                    校验最新账户状态
                  </Button>
                </div>
              ))}
            {prepare.isError && (
              <p role="alert" className="text-xs text-destructive">
                {errorMessage(prepare.error)}
              </p>
            )}
            {prepared && (
              <div className="space-y-4">
                <p className="text-sm leading-6">{prepared.message}</p>
                {action === "create" && (
                  <p className="rounded-md border p-3 font-mono text-xs leading-6 break-all">
                    {prepared.spec.name} · UID {prepared.spec.uid}
                    <br />
                    /home/{prepared.spec.name} · {prepared.spec.shell}
                    <br />
                    {prepared.spec.comment || "无账户描述"}
                  </p>
                )}
                {prepared.account && (
                  <p className="font-mono text-xs">
                    {prepared.account.name} · UID {prepared.account.uid} ·{" "}
                    {prepared.account.home}
                  </p>
                )}
                {prepared.allowed ? (
                  <form
                    className="space-y-4"
                    onSubmit={confirm.handleSubmit((v) => execute.mutate(v))}
                    noValidate
                  >
                    <p className="text-2xs text-muted-foreground">
                      确认有效至 {timestamp(prepared.expiresAt)}
                      。到期锁定不终止现有会话或 Slurm
                      作业；解锁仅恢复平台保存的原到期设置，不能保证登录一定成功。
                    </p>
                    <div className="space-y-2">
                      <Label htmlFor="local-reason">操作原因</Label>
                      <Input
                        id="local-reason"
                        {...confirm.register("reason")}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="local-confirm">
                        输入 {prepared.spec.name} 以确认
                      </Label>
                      <Input
                        id="local-confirm"
                        autoComplete="off"
                        {...confirm.register("confirmation")}
                      />
                    </div>
                    {Object.entries(confirm.formState.errors).map(
                      ([key, error]) => (
                        <p
                          role="alert"
                          key={key}
                          className="text-xs text-destructive"
                        >
                          {error.message}
                        </p>
                      )
                    )}
                    {expired && (
                      <p role="alert" className="text-xs text-destructive">
                        确认已过期，请重新校验。
                      </p>
                    )}
                    <div className="flex gap-2">
                      <Button
                        type="submit"
                        disabled={
                          typed !== prepared.spec.name ||
                          !!expired ||
                          execute.isPending
                        }
                      >
                        确认{title}
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        onClick={() => setPrepared(null)}
                      >
                        重新校验
                      </Button>
                    </div>
                  </form>
                ) : (
                  <Button variant="outline" onClick={() => setPrepared(null)}>
                    返回修改
                  </Button>
                )}
              </div>
            )}
          </>
        )}
        <AlertDialogCancel disabled={execute.isPending}>关闭</AlertDialogCancel>
      </DialogContent>
    </Dialog>
  )
}
