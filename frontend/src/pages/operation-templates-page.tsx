import { useState } from "react"
import { useSearchParams } from "react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { BookOpenCheck, Plus, RefreshCw } from "lucide-react"
import { ClientResponseError } from "pocketbase"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@/components/ui/alert-dialog"
import { useAuth } from "@/stores/auth"
import { api } from "@/services/api"
import {
  ClusterPicker,
  useAccessibleClusters,
} from "@/features/clusters/cluster-picker"
import { BulkControlDialog } from "@/features/bulk/control-dialog"
import { BulkOperations } from "@/features/bulk/operations-panel"
import { label, type BulkAction } from "@/features/bulk/schema"
import {
  actions,
  targets,
  templatesSchema,
  templateSchema,
  preparationSchema,
  type OperationTemplate,
} from "@/features/operation-templates/schema"
import { date } from "@/features/capacity/schema"
function message(e: unknown) {
  return e instanceof ClientResponseError
    ? e.status === 403 || e.status === 404
      ? "模板或当前操作权限不可用，请检查本人、集群与动作授权。"
      : e.response?.error?.message || "模板请求未完成，请刷新后重试。"
    : "模板请求未完成，请刷新后重试。"
}
export function OperationTemplatesPage() {
  const user = useAuth((s) => s.user),
    selection = useAccessibleClusters(),
    id = selection.id
  const permissions = [
    ...(user?.access.platformPermissions || []),
    ...(user?.access.clusters.find((c) => c.cluster.id === id)?.permissions ||
      []),
  ]
  const available = actions.filter(
    (a) =>
      permissions.includes(a) && permissions.includes(`${a.split(".")[0]}.read`)
  )
  return (
    <div className="space-y-6 p-5 sm:p-8">
      <header>
        <p className="eyebrow">OPERATIONS TEMPLATES</p>
        <h1 className="mt-2 flex items-center gap-2 text-2xl font-semibold">
          <BookOpenCheck />
          运维操作模板
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          保存本人在此集群的常用动作、原因和核查清单；每次重新选择明确目标并确认。
        </p>
      </header>
      <ClusterPicker selection={selection} label="运维模板" />
      {!id ? (
        <p role="status">
          {selection.inventory.isPending ? "正在读取集群…" : "暂无可用集群。"}
        </p>
      ) : !available.length ? (
        <p role="alert" className="panel p-5 text-destructive">
          需要作业取消或节点 Drain/Resume 权限，以及对应的资源读取权限。
        </p>
      ) : (
        <Workspace
          key={`${user?.id}:${id}`}
          clusterId={id}
          available={available}
        />
      )}
    </div>
  )
}
function Workspace({
  clusterId,
  available,
}: {
  clusterId: string
  available: BulkAction[]
}) {
  const client = useQueryClient(),
    user = useAuth((s) => s.user),
    [params, setParams] = useSearchParams()
  const [editing, setEditing] = useState<
      OperationTemplate | null | undefined
    >(),
    [deleting, setDeleting] = useState<OperationTemplate>(),
    [selected, setSelected] = useState<OperationTemplate>(),
    [targetText, setTargetText] = useState(""),
    [checked, setChecked] = useState<number[]>([]),
    [open, setOpen] = useState(false)
  const base = `/clusters/${clusterId}/operation-templates`,
    key = ["operation-templates", user?.id, clusterId]
  const list = useQuery({
    queryKey: key,
    queryFn: async ({ signal }) =>
      templatesSchema.parse(await api(base, { signal })),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const remove = useMutation({
    retry: false,
    mutationFn: async (v: OperationTemplate) =>
      api(`${base}/${v.id}`, {
        method: "DELETE",
        body: { revision: v.revision },
      }),
    onSuccess: () => {
      setDeleting(undefined)
      setSelected(undefined)
      void client.invalidateQueries({ queryKey: key })
    },
  })
  const values = selected ? targets(selected.action, targetText) : null
  const current =
    selected &&
    list.data?.items.some(
      (v) => v.id === selected.id && v.revision === selected.revision
    ) &&
    available.includes(selected.action)
  const ids = (params.get("operations") || "").split(",").filter(Boolean),
    resource = params.get("resource") === "job" ? "job" : "node"
  if (list.isError)
    return (
      <p role="alert" className="panel p-5 text-destructive">
        {message(list.error)}
      </p>
    )
  return (
    <>
      <section className="panel space-y-4 p-5" aria-label="本人的运维模板">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="font-semibold">
            本集群的本人模板（{list.data?.items.length ?? "—"} / 20）
          </h2>
          <div className="flex gap-2">
            <Button
              variant="outline"
              disabled={list.isFetching}
              onClick={() => void list.refetch()}
            >
              <RefreshCw />
              刷新模板
            </Button>
            <Button onClick={() => setEditing(null)}>
              <Plus />
              新建模板
            </Button>
          </div>
        </div>
        <p className="text-sm leading-6 text-muted-foreground">
          模板保存不执行 Slurm
          操作。准备确认时重新读取目标状态，最终操作继续使用现有逐项权限、身份、显式确认和审计。
        </p>
        {list.isPending ? (
          <p role="status">正在读取模板…</p>
        ) : !list.data?.items.length ? (
          <p role="status" className="py-4 text-muted-foreground">
            还没有本人模板，可以新建常用的取消、Drain 或 Resume 模板。
          </p>
        ) : (
          <div className="grid gap-4 xl:grid-cols-2">
            {list.data.items
              .filter((v) => available.includes(v.action))
              .map((v) => (
                <article key={v.id} className="space-y-3 rounded-lg border p-4">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="font-semibold break-all">{v.name}</h3>
                    <Badge variant="outline">{label(v.action)}</Badge>
                  </div>
                  <p className="text-sm break-all whitespace-pre-wrap text-muted-foreground">
                    {v.description || "未填写说明"}
                  </p>
                  <p className="text-xs">
                    核查 {v.checklist.length} 项 · v{v.revision} ·{" "}
                    {date(v.updatedAt)}
                  </p>
                  <div className="flex flex-wrap gap-2">
                    <Button
                      variant="outline"
                      onClick={() => {
                        setSelected(v)
                        setTargetText("")
                        setChecked([])
                      }}
                    >
                      使用模板 {v.name}
                    </Button>
                    <Button variant="ghost" onClick={() => setEditing(v)}>
                      编辑 {v.name}
                    </Button>
                    <Button
                      variant="ghost"
                      onClick={() => {
                        remove.reset()
                        setDeleting(v)
                      }}
                    >
                      删除 {v.name}
                    </Button>
                  </div>
                </article>
              ))}
          </div>
        )}
      </section>
      {editing !== undefined &&
        (!editing || available.includes(editing.action)) && (
          <Editor
            key={`${editing?.id || "new"}:${editing?.revision || 0}`}
            value={editing}
            available={available}
            base={base}
            onClose={() => setEditing(undefined)}
            onSaved={() => {
              setEditing(undefined)
              void client.invalidateQueries({ queryKey: key })
            }}
          />
        )}
      {selected && available.includes(selected.action) && (
        <section className="panel space-y-4 p-5" aria-label="模板操作草稿">
          <div className="flex items-center justify-between">
            <h2 className="font-semibold">
              使用模板：{selected.name} · v{selected.revision}
            </h2>
            <Button variant="ghost" onClick={() => setSelected(undefined)}>
              关闭草稿
            </Button>
          </div>
          <p className="text-sm">
            动作：{label(selected.action)}。
            {selected.action === "jobs.cancel"
              ? "取消会中断作业并可能丢失未保存结果。"
              : selected.action === "nodes.resume"
                ? "恢复后可能立即调度排队作业。"
                : "Drain 阻止新分配，已有作业继续运行。"}
          </p>
          <p className="text-sm break-all">
            默认原因：{selected.defaultReason || "在最终确认中填写"}
          </p>
          <Label className="grid gap-2">
            本次目标
            <Input
              aria-label="本次模板目标"
              value={targetText}
              onChange={(e) => {
                setTargetText(e.target.value)
                setChecked([])
              }}
              maxLength={2600}
              placeholder={
                selected.action === "jobs.cancel"
                  ? "1–10 个明确 Job ID，用空格或逗号分隔"
                  : "1–10 个明确节点名，不接受范围或通配符"
              }
            />
          </Label>
          <p className="text-xs text-muted-foreground">
            每次重新选择目标，不保存或自动复用节点/作业 ID。明确目标最多 10
            个，不能重复。
          </p>
          {targetText && !values && (
            <p role="alert" className="text-sm text-destructive">
              请输入 1–10 个合法且不重复的明确目标，不接受表达式。
            </p>
          )}
          {selected.checklist.length > 0 && (
            <fieldset className="space-y-3">
              <legend className="mb-3 text-sm font-medium">本次核查清单</legend>
              {selected.checklist.map((c, i) => (
                <Label
                  key={i}
                  className="flex items-start gap-3 text-sm leading-6"
                >
                  <Checkbox
                    checked={checked.includes(i)}
                    onCheckedChange={(v) =>
                      setChecked((previous) =>
                        v === true
                          ? [...previous, i]
                          : previous.filter((n) => n !== i)
                      )
                    }
                  />
                  {c}
                </Label>
              ))}
            </fieldset>
          )}
          {!current && (
            <p role="alert" className="text-sm text-destructive">
              模板已变化或当前动作权限不可用，请刷新并重新选择。
            </p>
          )}
          <Button
            disabled={
              !current ||
              !values ||
              checked.length !== selected.checklist.length ||
              list.isFetching
            }
            onClick={() => setOpen(true)}
          >
            校验目标并准备确认
          </Button>
          <p className="text-xs text-muted-foreground">
            勾选清单用于本次人工核查；准备和查看结果不会自动执行或重试控制。
          </p>
        </section>
      )}
      {ids.length > 0 && (
        <BulkOperations
          clusterId={clusterId}
          ids={ids}
          resource={resource}
          automatic={false}
          batchId={params.get("batch")}
        />
      )}
      {open && current && selected && values && (
        <BulkControlDialog
          key={`${selected.id}:${selected.revision}:${values.join(",")}`}
          clusterId={clusterId}
          action={selected.action}
          targets={values}
          initialReason={selected.defaultReason}
          templateLabel={`${selected.name} · v${selected.revision}`}
          prepareKey={`${selected.id}:${selected.revision}`}
          prepare={async (signal) => {
            const v = preparationSchema.parse(
              await api(`${base}/${selected.id}/prepare`, {
                method: "POST",
                body: { revision: selected.revision, targets: values },
                signal,
              })
            )
            if (
              v.template.id !== selected.id ||
              v.template.revision !== selected.revision ||
              v.template.action !== selected.action ||
              v.prepared.action !== selected.action ||
              v.prepared.items.length !== values.length ||
              v.prepared.items.some((item, i) => item.target !== values[i])
            )
              throw new Error("Template preparation changed")
            return v.prepared
          }}
          onClose={() => {
            setOpen(false)
            setTargetText("")
            setChecked([])
          }}
          onStart={(operations, batch) =>
            setParams((p) => {
              const n = new URLSearchParams(p)
              n.set("operations", operations.join(","))
              n.set("batch", batch)
              n.set(
                "resource",
                selected.action === "jobs.cancel" ? "job" : "node"
              )
              return n
            })
          }
          onResult={() =>
            void client.invalidateQueries({
              queryKey: ["bulk-operations", clusterId],
            })
          }
        />
      )}
      {deleting && (
        <AlertDialog
          open
          onOpenChange={(v) => {
            if (!v && !remove.isPending) setDeleting(undefined)
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>删除模板 {deleting.name}？</AlertDialogTitle>
              <AlertDialogDescription>
                删除本人保存的模板不会改变 Slurm
                状态或已准备的操作，审计记录仍保留。
              </AlertDialogDescription>
            </AlertDialogHeader>
            {remove.isError && (
              <p role="alert" className="text-sm text-destructive">
                {message(remove.error)}
              </p>
            )}
            <AlertDialogFooter>
              <AlertDialogCancel disabled={remove.isPending}>
                返回
              </AlertDialogCancel>
              <Button
                variant="destructive"
                disabled={remove.isPending}
                onClick={() => remove.mutate(deleting)}
              >
                确认删除模板
              </Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  )
}
function Editor({
  value,
  available,
  base,
  onClose,
  onSaved,
}: {
  value: OperationTemplate | null
  available: BulkAction[]
  base: string
  onClose: () => void
  onSaved: () => void
}) {
  const [error, setError] = useState("")
  const save = useMutation({
    retry: false,
    mutationFn: async (body: Record<string, unknown>) =>
      templateSchema.parse(
        await api(value ? `${base}/${value.id}` : base, {
          method: value ? "PATCH" : "POST",
          body,
        })
      ),
    onSuccess: onSaved,
  })
  return (
    <form
      className="panel space-y-4 p-5"
      aria-label="运维模板编辑"
      onSubmit={(e) => {
        e.preventDefault()
        const f = new FormData(e.currentTarget),
          checklist = String(f.get("checklist") || "")
            .split("\n")
            .map((s) => s.trim())
            .filter(Boolean)
        if (
          checklist.length > 8 ||
          checklist.some((v) => Array.from(v).length > 160)
        ) {
          setError("清单最多 8 项，每项最多 160 字。")
          return
        }
        setError("")
        save.mutate({
          name: String(f.get("name") || "").trim(),
          action: String(f.get("action")),
          description: String(f.get("description") || ""),
          defaultReason: String(f.get("reason") || "").trim(),
          checklist,
          revision: value?.revision || 0,
        })
      }}
    >
      <h2 className="font-semibold">
        {value ? `编辑模板：${value.name}` : "新建本人运维模板"}
      </h2>
      <div className="grid gap-4 sm:grid-cols-2">
        <Label className="grid gap-2">
          模板名称
          <Input
            name="name"
            required
            maxLength={80}
            defaultValue={value?.name || ""}
          />
        </Label>
        <Label className="grid gap-2">
          模板动作
          <select
            name="action"
            className="h-10 rounded-md border bg-background px-3 text-sm"
            defaultValue={value?.action || available[0]}
          >
            {available.map((a) => (
              <option key={a} value={a}>
                {label(a)}
              </option>
            ))}
          </select>
        </Label>
      </div>
      <Label className="grid gap-2">
        说明
        <textarea
          name="description"
          className="min-h-20 rounded-md border bg-background p-3 text-sm"
          maxLength={1000}
          defaultValue={value?.description || ""}
        />
      </Label>
      <Label className="grid gap-2">
        默认操作原因（可选）
        <Input
          name="reason"
          maxLength={440}
          defaultValue={value?.defaultReason || ""}
          placeholder="最终确认时仍可编辑；最多 220 字"
        />
      </Label>
      <Label className="grid gap-2">
        核查清单（每行一项，最多 8 项）
        <textarea
          name="checklist"
          className="min-h-28 rounded-md border bg-background p-3 text-sm"
          maxLength={4000}
          defaultValue={value?.checklist.join("\n") || ""}
          placeholder="例如：已通知相关作业用户&#10;已确认维护窗口和回退方案"
        />
      </Label>
      <p className="text-xs text-muted-foreground">
        模板不保存目标、可执行脚本、连接凭据或确认令牌；不支持定时或后台自动执行。
      </p>
      {(error || save.isError) && (
        <p role="alert" className="text-sm text-destructive">
          {error || message(save.error)}
        </p>
      )}
      <div className="flex gap-2">
        <Button disabled={save.isPending}>保存模板</Button>
        <Button
          type="button"
          variant="outline"
          disabled={save.isPending}
          onClick={onClose}
        >
          取消编辑
        </Button>
      </div>
    </form>
  )
}
