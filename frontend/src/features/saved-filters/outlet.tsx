import { useState } from "react"
import { Navigate, Outlet, useLocation, useSearchParams } from "react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Bookmark, RefreshCw, Save, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { api } from "@/services/api"
import { useAuth, type PlatformUser } from "@/stores/auth"
import { pageNumber } from "@/features/clusters/cluster-picker"
import { clusterPageSchema } from "@/features/overview/schema"
import {
  catalog,
  canEnter,
  savedFiltersSchema,
  savedFilterSchema,
  explicitQuery,
  conditionsFromURL,
  validConditions,
  appliedSearch,
  historyWindow,
  errorMessage,
  type Conditions,
  type SavedFilter,
} from "./schema"
export function SavedFiltersOutlet() {
  const user = useAuth((s) => s.user),
    location = useLocation(),
    module = location.pathname.slice(1),
    d = catalog[module],
    params = new URLSearchParams(location.search)
  if (!user || !d) return <Outlet />
  if (d.scoped && !params.get("cluster"))
    return <ImplicitCluster module={module} />
  const cid = d.scoped ? params.get("cluster") || "" : ""
  if (!canEnter(user, module, cid)) return <Outlet />
  return (
    <FilterScope
      key={`${user.id}:${module}:${cid}`}
      user={user}
      module={module}
      clusterId={cid}
    />
  )
}
function ImplicitCluster({ module }: { module: string }) {
  const [params] = useSearchParams(),
    page = pageNumber(params.get("clusterPage")),
    q = params.get("clusterQ") || ""
  const query = new URLSearchParams({ page: String(page), perPage: "20" })
  if (q) query.set("q", q)
  const list = useQuery({
    queryKey: ["accessible-clusters", page, q],
    queryFn: async ({ signal }) =>
      clusterPageSchema.parse(await api(`/clusters?${query}`, { signal })),
    retry: false,
  })
  if (list.isPending)
    return (
      <p role="status" className="p-6 text-sm text-muted-foreground">
        正在确定筛选方案的集群范围…
      </p>
    )
  const first = list.data?.items[0]
  if (!list.isError && first) {
    const next = new URLSearchParams(params)
    next.set("cluster", first.id)
    return <Navigate to={`/${module}?${next}`} replace />
  }
  return <Outlet />
}
function FilterScope({
  user,
  module,
  clusterId,
}: {
  user: PlatformUser
  module: string
  clusterId: string
}) {
  const [params, setParams] = useSearchParams(),
    client = useQueryClient(),
    [generation, setGeneration] = useState(0)
  const key = ["saved-filters", user.id, module, clusterId]
  const query = useQuery({
    queryKey: key,
    queryFn: async ({ signal }) =>
      savedFiltersSchema.parse(
        await api(
          `/saved-filters?${new URLSearchParams({ module, clusterId })}`,
          { signal }
        )
      ),
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: false,
  })
  const explicit = explicitQuery(params, catalog[module].scoped)
  if (!explicit && (query.isPending || query.isFetching))
    return (
      <p role="status" className="p-6 text-sm text-muted-foreground">
        正在恢复默认筛选方案…
      </p>
    )
  const data = query.isError ? undefined : query.data
  const defaultView = data?.items.find(
    (i) =>
      i.isDefault &&
      i.usable &&
      i.compatible &&
      validConditions(module, i.conditions)
  )
  if (!explicit && defaultView) {
    const next = appliedSearch(
      module,
      params,
      defaultView.conditions,
      "default"
    )
    return (
      <Navigate
        to={`/${module}?${module === "history" ? historyWindow(next) : next}`}
        replace
      />
    )
  }
  if (module === "history" && !params.has("start") && !params.has("end"))
    return <Navigate to={`/${module}?${historyWindow(params)}`} replace />
  const apply = (conditions: Conditions) => {
    const next = appliedSearch(module, params, conditions)
    setGeneration((g) => g + 1)
    setParams(next)
  }
  return (
    <>
      <FilterBar
        key={`${user.id}:${module}:${clusterId}`}
        module={module}
        clusterId={clusterId}
        items={data?.items || []}
        pending={query.isFetching}
        loadError={query.isError ? errorMessage(query.error) : ""}
        current={conditionsFromURL(module, params)}
        onApply={apply}
        onReset={() => apply({})}
        onRefresh={() => void query.refetch()}
        onSaved={() => void client.invalidateQueries({ queryKey: key })}
      />
      <div key={generation}>
        <Outlet />
      </div>
    </>
  )
}
function FilterBar({
  module,
  clusterId,
  items,
  current,
  pending,
  loadError,
  onApply,
  onReset,
  onRefresh,
  onSaved,
}: {
  module: string
  clusterId: string
  items: SavedFilter[]
  current: Conditions
  pending: boolean
  loadError: string
  onApply: (v: Conditions) => void
  onReset: () => void
  onRefresh: () => void
  onSaved: () => void
}) {
  const [selected, setSelected] = useState(""),
    [name, setName] = useState(""),
    [makeDefault, setMakeDefault] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("")
  const item = items.find((i) => i.id === selected),
    valid = validConditions(module, current)
  const mutation = useMutation({
    retry: false,
    mutationFn: async (action: "create" | "update" | "default" | "delete") => {
      if (action !== "create" && !item) throw new Error("Choose a saved filter")
      if (action === "delete") {
        await api(`/saved-filters/${item!.id}`, {
          method: "DELETE",
          body: { revision: item!.revision },
        })
        return { action }
      }
      const existing = action === "default",
        values = {
          name: existing ? item!.name : name.trim(),
          conditions: existing ? item!.conditions : current,
          isDefault: existing ? !item!.isDefault : makeDefault,
        }
      const saved = savedFilterSchema.parse(
        await api(
          action === "create" ? "/saved-filters" : `/saved-filters/${item!.id}`,
          {
            method: action === "create" ? "POST" : "PATCH",
            body:
              action === "create"
                ? { module, clusterId, ...values }
                : { ...values, revision: item!.revision },
          }
        )
      )
      return { action, item: saved }
    },
    onSuccess: ({ action, item: updated }) => {
      setError("")
      setNotice(
        action === "delete"
          ? "已删除方案。"
          : action === "default"
            ? "已更新默认方案。"
            : "已保存当前已应用的列表条件。"
      )
      if (updated) {
        setSelected(updated.id)
        setName(updated.name)
        setMakeDefault(updated.isDefault)
      }
      if (action === "delete") {
        setSelected("")
        setName("")
        setMakeDefault(false)
      }
      onSaved()
    },
    onError: (cause) => {
      setError(errorMessage(cause))
      setNotice("")
    },
  })
  const writing = mutation.isPending,
    disabled = pending || writing || !!loadError
  const choose = (id: string) => {
    setSelected(id)
    const value = items.find((i) => i.id === id)
    setName(value?.name || "")
    setMakeDefault(value?.isDefault || false)
    setNotice("")
    setError("")
  }
  return (
    <section
      className="mx-5 mt-5 space-y-3 rounded-lg border bg-card p-4 sm:mx-8"
      aria-label="保存的筛选方案"
    >
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Bookmark className="size-4" />
          {catalog[module].label}筛选方案
        </h2>
        <span className="text-xs text-muted-foreground">
          仅本人 · {catalog[module].scoped ? "当前集群" : "平台范围"} ·{" "}
          {items.length}/10
        </span>
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto"
          onClick={onRefresh}
          disabled={pending || writing}
        >
          <RefreshCw />
          刷新方案
        </Button>
      </div>
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-48 flex-1 space-y-1.5">
          <Label htmlFor="saved-filter-choice">已保存方案</Label>
          <select
            id="saved-filter-choice"
            className="h-10 w-full rounded-md border bg-background px-3 text-sm"
            value={item?.id || ""}
            disabled={disabled}
            onChange={(e) => choose(e.target.value)}
          >
            <option value="">选择方案</option>
            {items.map((i) => (
              <option key={i.id} value={i.id}>
                {i.name}
                {i.isDefault ? "（默认）" : ""}
                {!i.usable ? "（当前不可应用）" : ""}
              </option>
            ))}
          </select>
        </div>
        <Button
          variant="outline"
          disabled={
            disabled ||
            !item?.usable ||
            !item.compatible ||
            !validConditions(module, item.conditions)
          }
          onClick={() => {
            if (item) {
              onApply(item.conditions)
              setNotice("已应用方案，分页和详情已重置。")
            }
          }}
        >
          应用方案
        </Button>
        <Button
          variant="outline"
          disabled={writing}
          onClick={() => {
            onReset()
            setNotice("已恢复页面默认条件，本次不会自动套用保存的方案。")
          }}
        >
          重置当前条件
        </Button>
      </div>
      <details className="group">
        <summary className="cursor-pointer text-xs text-muted-foreground">
          保存与管理方案
        </summary>
        <div className="mt-3 flex flex-wrap items-end gap-3">
          <div className="min-w-48 flex-1 space-y-1.5">
            <Label htmlFor="saved-filter-name">方案名称</Label>
            <Input
              id="saved-filter-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={80}
              placeholder="最多 40 字"
              autoComplete="off"
            />
          </div>
          <Label className="mb-3 flex items-center gap-2">
            <Checkbox
              aria-label="设为默认筛选方案"
              checked={makeDefault}
              onCheckedChange={(v) => setMakeDefault(v === true)}
            />
            设为默认
          </Label>
          <Button
            disabled={
              disabled ||
              !valid ||
              !name.trim() ||
              Array.from(name.trim()).length > 40 ||
              items.length >= 10
            }
            onClick={() => mutation.mutate("create")}
          >
            <Save />
            另存方案
          </Button>
          <Button
            variant="outline"
            disabled={
              disabled ||
              !item ||
              !valid ||
              !name.trim() ||
              Array.from(name.trim()).length > 40
            }
            onClick={() => mutation.mutate("update")}
          >
            更新所选方案
          </Button>
          <Button
            variant="outline"
            disabled={disabled || !item?.usable}
            onClick={() => mutation.mutate("default")}
          >
            {item?.isDefault ? "取消默认" : "将所选设为默认"}
          </Button>
          <Button
            variant="ghost"
            disabled={disabled || !item}
            onClick={() => mutation.mutate("delete")}
          >
            <Trash2 />
            删除所选方案
          </Button>
        </div>
      </details>
      <p className="text-xs leading-5 text-muted-foreground">
        保存在平台账户中，可跨浏览器使用。明确的地址条件优先；只保存列表条件、排序和每页数量。
        {module === "history" || module === "audits"
          ? " 时间范围按固定起止保存。"
          : ""}
      </p>
      {loadError && (
        <p role="alert" className="text-sm text-destructive">
          {loadError} 未恢复默认方案，当前使用地址中的条件。
        </p>
      )}
      {!valid && (
        <p role="alert" className="text-sm text-destructive">
          当前地址含有无法保存的条件，请先调整筛选。
        </p>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="text-sm">
          {notice}
        </p>
      )}
    </section>
  )
}
