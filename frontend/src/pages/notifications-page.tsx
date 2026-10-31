import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Bell, Plus, RefreshCw, CheckCheck, Settings2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import {
  ClusterPicker,
  useAccessibleClusters,
  pageNumber,
} from "@/features/clusters/cluster-picker"
import {
  kinds,
  states,
  allows,
  ruleListSchema,
  notificationPageSchema,
  errorMessage,
  dateTime,
  thresholdLabel,
  type Kind,
  type Rule,
} from "@/features/notifications/schema"
const selectStyle = "h-10 rounded-md border bg-background px-3 text-sm"
export function NotificationsPage() {
  const user = useAuth((s) => s.user),
    [params, setParams] = useSearchParams(),
    client = useQueryClient(),
    selection = useAccessibleClusters(),
    [editing, setEditing] = useState<Rule | "new" | null>(null),
    [error, setError] = useState("")
  const page = pageNumber(params.get("page")),
    perPage = [20, 50, 100].includes(Number(params.get("perPage")))
      ? Number(params.get("perPage"))
      : 20,
    read = params.get("read") || "all",
    phase = params.get("phase") || "",
    cid = params.get("clusterId") || ""
  const listParams = new URLSearchParams({
    page: String(page),
    perPage: String(perPage),
    read,
  })
  if (phase) listParams.set("phase", phase)
  if (cid) listParams.set("clusterId", cid)
  const list = useQuery({
    queryKey: ["notifications", user?.id, "page", listParams.toString()],
    queryFn: async ({ signal }) =>
      notificationPageSchema.parse(
        await api(`/notifications?${listParams}`, { signal })
      ),
    refetchInterval: 30000,
    retry: false,
  })
  const rules = useQuery({
    queryKey: ["notification-rules", user?.id],
    queryFn: async ({ signal }) =>
      ruleListSchema.parse(await api("/notification-rules", { signal })),
    refetchInterval: 30000,
    retry: false,
  })
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ["notifications", user?.id] })
    void client.invalidateQueries({
      queryKey: ["notification-rules", user?.id],
    })
  }
  const change = (key: string, value: string) =>
    setParams((previous) => {
      const n = new URLSearchParams(previous)
      if (value) n.set(key, value)
      else n.delete(key)
      n.set("page", "1")
      return n
    })
  const mutation = useMutation({
    retry: false,
    mutationFn: async (action: {
      type: "read" | "read-all" | "check" | "delete"
      id?: string
      read?: boolean
      revision?: number
    }) => {
      const { type, id } = action
      if (type === "read")
        return api(`/notifications/${id}/read`, {
          method: "POST",
          body: { read: action.read },
        })
      if (type === "read-all")
        return api("/notifications/read-all", { method: "POST", body: {} })
      if (type === "check")
        return api(`/notification-rules/${id}/check`, {
          method: "POST",
          body: {},
        })
      return api(`/notification-rules/${id}`, {
        method: "DELETE",
        body: { revision: action.revision },
      })
    },
    onSuccess: () => {
      setError("")
      refresh()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const busy = mutation.isPending
  return (
    <div className="space-y-6 p-5 sm:p-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="eyebrow">ALERTS & NOTIFICATIONS</p>
          <h1 className="mt-2 flex items-center gap-2 text-2xl font-semibold">
            <Bell className="size-6" />
            告警通知
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            关注自己的集群规则，接收触发和恢复通知。每 30
            秒检查，持续异常只通知一次。
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={refresh}
            disabled={list.isFetching || rules.isFetching}
          >
            <RefreshCw />
            刷新
          </Button>
          <Button
            onClick={() => setEditing("new")}
            disabled={
              rules.isPending ||
              rules.isError ||
              (rules.data?.items.length ?? 0) >= 20
            }
          >
            <Plus />
            新建规则
          </Button>
        </div>
      </header>
      {(error || list.isError || rules.isError) && (
        <p role="alert" className="panel p-4 text-sm text-destructive">
          {error || errorMessage(list.error || rules.error)}
        </p>
      )}
      <section className="panel space-y-4 p-5" aria-label="站内通知">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="font-semibold">
            站内通知{" "}
            <span className="ml-2 text-sm font-normal text-muted-foreground">
              {list.isError
                ? "暂不可用"
                : `${list.data?.unreadCount ?? 0} 条未读`}
            </span>
          </h2>
          <Button
            size="sm"
            variant="outline"
            onClick={() => mutation.mutate({ type: "read-all" })}
            disabled={busy || list.isError || !list.data?.unreadCount}
          >
            <CheckCheck />
            全部标为已读
          </Button>
        </div>
        <div className="flex flex-wrap gap-3">
          <Label className="flex items-center gap-2">
            读取状态
            <select
              aria-label="通知读取状态"
              className={selectStyle}
              value={read}
              onChange={(e) => change("read", e.target.value)}
            >
              <option value="all">全部通知</option>
              <option value="unread">仅未读</option>
              <option value="read">仅已读</option>
            </select>
          </Label>
          <Label className="flex items-center gap-2">
            事件
            <select
              aria-label="通知事件"
              className={selectStyle}
              value={phase}
              onChange={(e) => change("phase", e.target.value)}
            >
              <option value="">全部事件</option>
              <option value="firing">告警触发</option>
              <option value="recovered">告警恢复</option>
            </select>
          </Label>
          <Label className="flex items-center gap-2">
            集群
            <select
              aria-label="通知集群"
              className={selectStyle}
              value={cid}
              onChange={(e) => change("clusterId", e.target.value)}
            >
              <option value="">全部集群</option>
              {cid && !selection.options.some((c) => c.id === cid) && (
                <option value={cid}>{cid}</option>
              )}
              {selection.options.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </Label>
        </div>
        {list.isPending ? (
          <p role="status" className="py-6 text-sm text-muted-foreground">
            正在读取通知…
          </p>
        ) : !list.isError && list.data?.items.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            当前没有匹配的通知。创建规则后，将在异常触发或恢复时通知你。
          </p>
        ) : (
          !list.isError && (
            <div className="space-y-3">
              {list.data?.items.map((n) => (
                <article
                  key={n.id}
                  aria-label={`${n.ruleName} ${n.phase === "firing" ? "触发" : "恢复"}`}
                  className={`space-y-2 rounded-lg border p-4 ${!n.read ? "border-primary/30 bg-primary/5" : ""}`}
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge
                      variant={
                        n.phase === "firing" && n.severity === "critical"
                          ? "destructive"
                          : "outline"
                      }
                    >
                      {n.phase === "recovered"
                        ? "已恢复"
                        : n.severity === "critical"
                          ? "严重告警"
                          : "警告"}
                    </Badge>
                    <h3 className="font-medium">{n.ruleName}</h3>
                    {!n.read && (
                      <span className="text-xs text-primary">未读</span>
                    )}
                    <time className="ml-auto text-xs text-muted-foreground">
                      {dateTime(n.createdAt)}
                    </time>
                  </div>
                  <p className="text-sm">{n.message}</p>
                  {n.samples.length > 0 && (
                    <p className="font-mono text-xs break-all text-muted-foreground">
                      {n.samples.join(" · ")}
                      {n.affectedCount > 5 ? ` · 等 ${n.affectedCount} 项` : ""}
                    </p>
                  )}
                  <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                    <span>
                      {selection.options.find((c) => c.id === n.clusterId)
                        ?.name || n.clusterId}{" "}
                      · {kinds[n.kind]}
                    </span>
                    <Link
                      to={n.href}
                      className="text-primary underline underline-offset-4"
                    >
                      查看当前资源
                    </Link>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="ml-auto"
                      disabled={busy}
                      onClick={() =>
                        mutation.mutate({
                          type: "read",
                          id: n.id,
                          read: !n.read,
                        })
                      }
                    >
                      {n.read ? "标为未读" : "标为已读"}
                    </Button>
                  </div>
                </article>
              ))}
            </div>
          )
        )}
        <div className="flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
          <span>
            共 {list.isError ? "—" : (list.data?.totalItems ?? 0)} 条 · 第{" "}
            {page} 页
          </span>
          <select
            aria-label="每页通知数"
            className={`${selectStyle} ml-auto`}
            value={perPage}
            onChange={(e) => change("perPage", e.target.value)}
          >
            {[20, 50, 100].map((n) => (
              <option key={n} value={n}>
                {n} 条 / 页
              </option>
            ))}
          </select>
          <Button
            size="sm"
            variant="outline"
            aria-label="上一页通知"
            disabled={page <= 1 || list.isError || list.isFetching}
            onClick={() =>
              setParams((p) => {
                const n = new URLSearchParams(p)
                n.set("page", String(page - 1))
                return n
              })
            }
          >
            上一页
          </Button>
          <Button
            size="sm"
            variant="outline"
            aria-label="下一页通知"
            disabled={
              list.isError ||
              list.isFetching ||
              page * perPage >= (list.data?.totalItems ?? 0)
            }
            onClick={() =>
              setParams((p) => {
                const n = new URLSearchParams(p)
                n.set("page", String(page + 1))
                return n
              })
            }
          >
            下一页
          </Button>
        </div>
      </section>
      <section className="panel space-y-4 p-5" aria-label="我的告警规则">
        <div className="flex items-center justify-between">
          <h2 className="flex items-center gap-2 font-semibold">
            <Settings2 className="size-4" />
            我的告警规则
          </h2>
          <span className="text-xs text-muted-foreground">
            {rules.isError ? "—" : (rules.data?.items.length ?? 0)}/20 ·
            仅本人可见
          </span>
        </div>
        <p className="text-xs leading-6 text-muted-foreground">
          来源不可用、权限撤销和过期 GPU
          数据不会被当成恢复。规则修改或停用会结束本轮评估，历史通知保留。通知保留最多
          30 天、每人最近 200 条。
        </p>
        {rules.isPending ? (
          <p role="status">正在读取规则…</p>
        ) : !rules.isError && rules.data?.items.length === 0 ? (
          <p className="py-5 text-sm text-muted-foreground">
            尚未创建告警规则。
          </p>
        ) : (
          !rules.isError && (
            <div className="grid gap-3 xl:grid-cols-2">
              {rules.data?.items.map((r) => (
                <article
                  key={r.id}
                  aria-label={`规则 ${r.name}`}
                  className="space-y-3 rounded-lg border p-4"
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="font-medium">{r.name}</h3>
                    <Badge
                      className="ml-auto"
                      variant={r.state === "firing" ? "destructive" : "outline"}
                    >
                      {states[r.state] || "未知"}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {selection.options.find((c) => c.id === r.clusterId)
                      ?.name || r.clusterId}{" "}
                    · {kinds[r.kind]} ·{" "}
                    {r.kind !== "cluster_health"
                      ? `${thresholdLabel(r.kind)} ${r.threshold} · `
                      : ""}
                    持续 {r.holdMinutes} 分钟
                  </p>
                  <p className="text-sm">{r.summary}</p>
                  {r.incidentActive && r.state === "unknown" && (
                    <p className="text-xs text-amber-700">
                      原告警尚未确认恢复。
                    </p>
                  )}
                  <p className="text-xs text-muted-foreground">
                    最近检查：{dateTime(r.lastChecked)}
                    {r.enabled && r.usable && r.lastChecked && r.stale
                      ? " · 采样延迟，当前状态待核实"
                      : ""}
                  </p>
                  <div className="flex flex-wrap gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={busy || !r.enabled || !r.usable}
                      onClick={() =>
                        mutation.mutate({ type: "check", id: r.id })
                      }
                    >
                      立即检查
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={busy || !r.usable}
                      onClick={() => setEditing(r)}
                    >
                      编辑规则
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={busy}
                      onClick={() =>
                        mutation.mutate({
                          type: "delete",
                          id: r.id,
                          revision: r.revision,
                        })
                      }
                    >
                      删除规则
                    </Button>
                  </div>
                </article>
              ))}
            </div>
          )
        )}
      </section>
      <Sheet
        open={editing !== null}
        onOpenChange={(v) => {
          if (!v) setEditing(null)
        }}
      >
        <SheetContent className="gap-5 overflow-y-auto p-6 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl">
          <SheetHeader className="p-0">
            <SheetTitle>
              {editing === "new" ? "新建告警规则" : "编辑告警规则"}
            </SheetTitle>
            <SheetDescription>
              只向当前平台账户发送站内通知，不执行资源控制。保存后开始一轮新的评估。
            </SheetDescription>
          </SheetHeader>
          {editing && (
            <RuleForm
              key={typeof editing === "string" ? "new" : editing.id}
              initial={editing === "new" ? undefined : editing}
              selection={selection}
              onSaved={() => {
                setEditing(null)
                refresh()
              }}
            />
          )}
        </SheetContent>
      </Sheet>
    </div>
  )
}
function RuleForm({
  initial,
  selection,
  onSaved,
}: {
  initial?: Rule
  selection: ReturnType<typeof useAccessibleClusters>
  onSaved: () => void
}) {
  const user = useAuth((s) => s.user),
    [name, setName] = useState(initial?.name || ""),
    [kind, setKind] = useState<Kind>(initial?.kind || "node_unavailable"),
    [threshold, setThreshold] = useState(String(initial?.threshold || 1)),
    [hold, setHold] = useState(String(initial?.holdMinutes ?? 2)),
    [severity, setSeverity] = useState(initial?.severity || "warning"),
    [enabled, setEnabled] = useState(initial?.enabled ?? true)
  const cid = initial?.clusterId || selection.id,
    permitted = allows(user, kind, cid)
  const mutation = useMutation({
    retry: false,
    mutationFn: () =>
      api(
        initial ? `/notification-rules/${initial.id}` : "/notification-rules",
        {
          method: initial ? "PATCH" : "POST",
          body: {
            clusterId: cid,
            name: name.trim(),
            kind,
            severity,
            enabled,
            threshold: kind === "cluster_health" ? 1 : Number(threshold),
            holdMinutes: Number(hold),
            ...(initial ? { revision: initial.revision } : {}),
          },
        }
      ),
    onSuccess: onSaved,
  })
  const choose = (next: Kind) => {
    setKind(next)
    setThreshold(
      next === "gpu_temperature" ? "85" : next === "job_pending" ? "60" : "1"
    )
  }
  const value = Number(threshold),
    minutes = Number(hold),
    valid =
      name.trim().length > 0 &&
      Array.from(name.trim()).length <= 40 &&
      Number.isInteger(value) &&
      value >= 1 &&
      value <= 10000 &&
      Number.isInteger(minutes) &&
      minutes >= 0 &&
      minutes <= 60 &&
      (kind !== "gpu_temperature" || (value >= 30 && value <= 120))
  return (
    <>
      {!initial && <ClusterPicker selection={selection} label="告警规则" />}
      <form
        className="space-y-5"
        onSubmit={(e) => {
          e.preventDefault()
          if (valid && permitted) mutation.mutate()
        }}
      >
        {initial ? (
          <p className="text-sm text-muted-foreground">
            集群：{selection.options.find((c) => c.id === cid)?.name || cid}
            （范围不可变）
          </p>
        ) : null}
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2 sm:col-span-2">
            <Label htmlFor="alert-name">规则名称</Label>
            <Input
              id="alert-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={80}
              placeholder="例如：离线节点巡检"
              required
            />
          </div>
          <Label className="grid gap-2">
            规则类型
            <select
              aria-label="规则类型"
              className={selectStyle}
              value={kind}
              onChange={(e) => choose(e.target.value as Kind)}
            >
              {Object.entries(kinds).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </Label>
          <Label className="grid gap-2">
            告警等级
            <select
              aria-label="告警等级"
              className={selectStyle}
              value={severity}
              onChange={(e) =>
                setSeverity(e.target.value as "warning" | "critical")
              }
            >
              <option value="warning">警告</option>
              <option value="critical">严重</option>
            </select>
          </Label>
          {kind !== "cluster_health" && (
            <div className="space-y-2">
              <Label htmlFor="alert-threshold">{thresholdLabel(kind)}</Label>
              <Input
                id="alert-threshold"
                type="number"
                min={kind === "gpu_temperature" ? 30 : 1}
                max={kind === "gpu_temperature" ? 120 : 10000}
                step="1"
                value={threshold}
                onChange={(e) => setThreshold(e.target.value)}
                required
              />
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="alert-hold">持续分钟数</Label>
            <Input
              id="alert-hold"
              type="number"
              min="0"
              max="60"
              step="1"
              value={hold}
              onChange={(e) => setHold(e.target.value)}
              required
            />
            <p className="text-xs text-muted-foreground">
              0 表示首次有效异常采样即触发。
            </p>
          </div>
        </div>
        {kind === "job_pending" && (
          <p className="text-xs leading-6 text-muted-foreground">
            按当前 PENDING 状态和提交时间判断，重新入队的作业沿用其提交时间。
          </p>
        )}
        <Label className="flex items-center gap-2">
          <Checkbox
            aria-label="启用告警规则"
            checked={enabled}
            onCheckedChange={(v) => setEnabled(v === true)}
          />
          启用规则
        </Label>
        {!permitted && (
          <p role="alert" className="text-sm text-destructive">
            没有所选集群对应来源的读取权限。
          </p>
        )}
        {mutation.isError && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(mutation.error)}
          </p>
        )}
        <div className="flex justify-end">
          <Button
            type="submit"
            disabled={!permitted || !valid || mutation.isPending}
          >
            {mutation.isPending ? "正在保存…" : "保存规则"}
          </Button>
        </div>
      </form>
    </>
  )
}
