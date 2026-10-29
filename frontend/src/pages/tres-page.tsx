import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { RefreshCw, SlidersHorizontal, Database } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet"
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select"
import {
  Table,
  TableHeader,
  TableHead,
  TableRow,
  TableBody,
  TableCell,
} from "@/components/ui/table"
import {
  ClusterPicker,
  useAccessibleClusters,
  pageNumber,
} from "@/features/clusters/cluster-picker"
import { TRESTargetTable } from "@/features/tres/target-table"
import { TRESEditor } from "@/features/tres/editor"
import {
  buckets,
  pageSchema,
  detailSchema,
  tresOperationSchema,
  errorMessage,
  statuses,
  targetName,
  limitText,
  unit,
  protectedTarget,
} from "@/features/tres/schema"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"

export function TRESPage() {
  const [params, setParams] = useSearchParams(),
    selection = useAccessibleClusters(),
    clusterId = selection.id,
    user = useAuth((s) => s.user),
    [editing, setEditing] = useState(false),
    [catalog, setCatalog] = useState(false)
  const kind = params.get("target") === "qos" ? "qos" : "association",
    item = params.get("item") || "",
    operation = params.get("operation") || "",
    page = pageNumber(params.get("page")),
    q = params.get("q") || ""
  const permissions = [
      ...(user?.access.platformPermissions ?? []),
      ...(user?.access.clusters.find((c) => c.cluster.id === clusterId)
        ?.permissions ?? []),
    ],
    has = (...ps: string[]) => ps.every((p) => permissions.includes(p))
  const canRead = has(
      "tres.read",
      "associations.read",
      ...(kind === "qos" ? ["qos.read"] : [])
    ),
    canEdit = has(
      "tres.update",
      kind === "qos" ? "qos.update" : "associations.update",
      ...(kind === "qos" ? ["platform.qos.read", "platform.qos.manage"] : [])
    )
  const update = (changes: Record<string, string | null>) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      for (const [k, v] of Object.entries(changes)) {
        if (v === null || v === "") next.delete(k)
        else next.set(k, v)
      }
      return next
    })
  const filter = new URLSearchParams({
    target: kind,
    page: String(page),
    perPage: "20",
    sort: params.get("sort") || "id",
    order: params.get("order") || "asc",
  })
  if (q) filter.set("q", q)
  for (const k of ["account", "user"])
    if (params.get(k)) filter.set(k, params.get(k)!)
  const list = useQuery({
    queryKey: ["tres", clusterId, kind, filter.toString()],
    enabled: !!clusterId && canRead,
    retry: false,
    staleTime: 60000,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      pageSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/tres-limits?${filter}`,
          { signal }
        )
      ),
  })
  const detail = useQuery({
    queryKey: ["tres", clusterId, kind, "detail", item],
    enabled: !!clusterId && !!item && canRead,
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      detailSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/tres-limits/${kind}/${encodeURIComponent(item)}`,
          { signal }
        )
      ),
  })
  const recent = useQuery({
    queryKey: ["tres", clusterId, "recent"],
    enabled: !!clusterId && has("tres.read"),
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      z
        .object({ items: z.array(tresOperationSchema) })
        .parse(
          await api(
            `/clusters/${encodeURIComponent(clusterId)}/tres-operations`,
            { signal }
          )
        ),
  })
  const result = useQuery({
    queryKey: ["tres", clusterId, "operation", operation],
    enabled: !!clusterId && !!operation && has("tres.read"),
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      tresOperationSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/accounting-operations/${encodeURIComponent(operation)}`,
          { signal }
        )
      ),
  })
  const rows = list.isError || !canRead ? [] : (list.data?.items ?? []),
    target = detail.isError ? undefined : detail.data?.item,
    resources = detail.data?.resources ?? list.data?.resources ?? []
  const bucket =
      params.get("bucket") && buckets[params.get("bucket")!]?.[kind]
        ? params.get("bucket")!
        : "group_total",
    entries = target?.limits[bucket] ?? []
  const openOperation = (id: string) =>
    update({ operation: id, cluster: clusterId })
  const refresh = () => {
    if (canRead) {
      void list.refetch()
      if (item) void detail.refetch()
    }
    void recent.refetch()
    if (operation) void result.refetch()
  }
  return (
    <div className="space-y-6 p-4 md:p-6 lg:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="eyebrow">RESOURCE QUOTAS</p>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight">
            TRES 限额
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            配置 CPU、内存、GPU 等已注册资源的本层限额与分钟额度。
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={() => setCatalog(true)}
            disabled={!list.data?.resources.length}
          >
            <Database className="size-4" />
            资源目录
          </Button>
          <Button
            variant="outline"
            onClick={refresh}
            disabled={!canRead || list.isFetching}
          >
            <RefreshCw
              className={list.isFetching ? "size-4 animate-spin" : "size-4"}
            />
            刷新限额
          </Button>
        </div>
      </header>
      <ClusterPicker
        selection={selection}
        label="限额"
        onSwitch={() => setEditing(false)}
      />
      <Tabs
        value={kind}
        onValueChange={(target) => {
          setEditing(false)
          update({
            target,
            page: "1",
            item: null,
            q: null,
            bucket: null,
            account: null,
            user: null,
            sort: null,
            order: null,
          })
        }}
      >
        <TabsList>
          <TabsTrigger value="association">Association 限额</TabsTrigger>
          <TabsTrigger value="qos">QOS 限额</TabsTrigger>
        </TabsList>
      </Tabs>
      <p className="rounded-lg border bg-card p-3 text-sm text-muted-foreground">
        {kind === "qos"
          ? has("platform.qos.read")
            ? "当前可见共享记账库的全部 QOS。全局限额修改需平台 QOS 管理权限，可能影响多个集群。"
            : "仅展示所选集群 Association 引用的 QOS。完整目录及全局写入需要平台授权。"
          : "关联按实际 Slurm 集群隔离，Account 组限额也约束下级关联。"}{" "}
        显示本层配置；实际可用额度还取决于父级、QOS、分区及站点
        AccountingStorageEnforce 配置。
      </p>
      <TRESSearch
        key={`${kind}:${q}`}
        q={q}
        onSearch={(q) => update({ q, page: "1", item: null })}
      />
      {(params.get("account") || params.get("user")) && (
        <Button
          variant="ghost"
          onClick={() => update({ account: null, user: null, page: "1" })}
        >
          清除关联筛选
        </Button>
      )}
      {!canRead ? (
        <p role="alert" className="text-sm text-muted-foreground">
          没有此集群的 TRES 及对象读取权限。
        </p>
      ) : list.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(list.error)}
        </p>
      ) : list.isPending ? (
        <Skeleton className="h-64 rounded-lg" />
      ) : (
        <section className="overflow-hidden rounded-lg border bg-card">
          <div className="flex items-center justify-between border-b px-4 py-3">
            <h2 className="font-semibold">
              {kind === "qos" ? "QOS" : "Association"} ·{" "}
              {list.data?.totalItems ?? 0}
            </h2>
            <p className="text-xs text-muted-foreground">
              {list.data?.cached ? "缓存采样" : "实时查询"} ·{" "}
              {list.data?.fetchedAt
                ? new Date(list.data.fetchedAt).toLocaleTimeString("zh-CN", {
                    hour12: false,
                  })
                : "—"}
            </p>
          </div>
          <TRESTargetTable
            items={rows}
            onOpen={(item) => update({ item, cluster: clusterId })}
            onSort={(sort) =>
              update({
                sort,
                order:
                  filter.get("sort") === sort && filter.get("order") === "asc"
                    ? "desc"
                    : "asc",
                page: "1",
                item: null,
              })
            }
          />
          <div className="flex items-center justify-end gap-3 border-t p-3">
            <Button
              variant="outline"
              size="sm"
              aria-label="上一页限额对象"
              disabled={page <= 1}
              onClick={() => update({ page: String(page - 1), item: null })}
            >
              上一页
            </Button>
            <span className="text-xs text-muted-foreground">第 {page} 页</span>
            <Button
              variant="outline"
              size="sm"
              aria-label="下一页限额对象"
              disabled={page * 20 >= (list.data?.totalItems ?? 0)}
              onClick={() => update({ page: String(page + 1), item: null })}
            >
              下一页
            </Button>
          </div>
        </section>
      )}
      <section
        className="space-y-3 rounded-lg border p-4"
        aria-label="最近 TRES 操作"
      >
        <h2 className="font-semibold">本人最近 TRES 操作</h2>
        {recent.isError ? (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(recent.error)}
          </p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {recent.data?.items.map((v) => (
              <Button
                key={v.operationId}
                variant="outline"
                size="sm"
                onClick={() => openOperation(v.operationId)}
              >
                {v.spec.tres.name || v.spec.tres.account} ·{" "}
                {buckets[v.spec.tres.bucket]?.label} ·{" "}
                {statuses[v.status] || v.status}
              </Button>
            ))}
            {recent.data?.items.length === 0 && (
              <p className="text-sm text-muted-foreground">暂无 TRES 操作。</p>
            )}
          </div>
        )}
      </section>
      {operation && (
        <section
          className="space-y-3 rounded-lg border p-4"
          aria-label="TRES 操作结果"
        >
          <div className="flex items-center justify-between">
            <h2 className="font-semibold">TRES 操作结果</h2>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void result.refetch()}
              disabled={result.isFetching}
            >
              只读核实结果
            </Button>
          </div>
          <p className="font-mono text-xs break-all">{operation}</p>
          {result.isError ? (
            <p role="alert" className="text-sm text-destructive">
              {errorMessage(result.error)}
            </p>
          ) : (
            result.data && (
              <>
                <Badge variant="outline">
                  {statuses[result.data.status] || result.data.status}
                </Badge>
                <p className="text-sm">{result.data.message}</p>
                <p className="text-xs text-muted-foreground">
                  {result.data.spec.tres.mode === "clear"
                    ? "清除本层配置"
                    : result.data.spec.tres.value}{" "}
                  · {result.data.reason}
                </p>
              </>
            )
          )}
        </section>
      )}
      <Sheet
        open={!!item}
        onOpenChange={(open) => {
          if (!open) {
            setEditing(false)
            update({ item: null })
          }
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-2xl">
          <SheetHeader>
            <SheetTitle>
              {target ? targetName(target) : "TRES 限额详情"}
            </SheetTitle>
            <SheetDescription>
              {target
                ? `Slurm 集群 ${target.cluster} · #${target.id}`
                : "读取所选对象的最新本层设置。"}
            </SheetDescription>
          </SheetHeader>
          <div className="space-y-5 px-5 pb-6">
            {detail.isError ? (
              <p role="alert" className="text-sm text-destructive">
                {errorMessage(detail.error)}
              </p>
            ) : !target ? (
              <Skeleton className="h-48" />
            ) : (
              <>
                {target.relative && (
                  <p className="rounded-md border p-3 text-sm">
                    Relative QOS：容量限额以 0–100
                    的整数百分比配置；时间额度的设置由管理员维护。
                  </p>
                )}
                <div className="space-y-1.5">
                  <Label htmlFor="tres-detail-bucket">查看限额类型</Label>
                  <Select
                    value={bucket}
                    onValueChange={(bucket) => update({ bucket })}
                  >
                    <SelectTrigger id="tres-detail-bucket">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {Object.entries(buckets)
                        .filter(([, v]) => v[kind])
                        .map(([key, v]) => (
                          <SelectItem key={key} value={key}>
                            {v.label}
                          </SelectItem>
                        ))}
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">
                    {buckets[bucket].description}
                  </p>
                </div>
                <Table aria-label="本层资源限额">
                  <TableHeader>
                    <TableRow>
                      <TableHead>资源</TableHead>
                      <TableHead>本层数值</TableHead>
                      <TableHead>单位</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {entries.map((v) => {
                      const r = resources.find((r) => r.id === v.id)
                      return (
                        <TableRow key={v.id}>
                          <TableCell className="font-mono">
                            {r?.key || `#${v.id}`}
                          </TableCell>
                          <TableCell className="font-mono">
                            {limitText(v)}
                          </TableCell>
                          <TableCell>{unit(target, r, bucket)}</TableCell>
                        </TableRow>
                      )
                    })}
                    {entries.length === 0 && (
                      <TableRow>
                        <TableCell
                          colSpan={3}
                          className="h-24 text-center text-muted-foreground"
                        >
                          本层未设置此类限额，按 Slurm 规则继承。
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
                <p className="text-xs text-muted-foreground">
                  清除只移除此对象、此类型、此资源的本层设置。其他资源和高级策略会保留并在写后核实。
                </p>
                {protectedTarget(target) ? (
                  <p className="text-sm text-muted-foreground">
                    系统 root 层级与服务关联受保护，支持查询。
                  </p>
                ) : (
                  canEdit && (
                    <Button
                      onClick={() => setEditing(true)}
                      disabled={!resources.some((r) => r.editable)}
                    >
                      <SlidersHorizontal className="size-4" />
                      配置 TRES 限额
                    </Button>
                  )
                )}
                <Button variant="outline" asChild>
                  <Link
                    to={
                      target.kind === "qos"
                        ? `/policies?${new URLSearchParams({ cluster: clusterId, item: target.id === item ? target.name : item })}`
                        : `/accounts?${new URLSearchParams({ cluster: clusterId, tab: "associations", item: target.id })}`
                    }
                  >
                    查看{target.kind === "qos" ? " QOS" : " Association"}
                  </Link>
                </Button>
              </>
            )}
          </div>
        </SheetContent>
      </Sheet>
      <Sheet open={catalog} onOpenChange={setCatalog}>
        <SheetContent className="overflow-y-auto sm:max-w-xl">
          <SheetHeader>
            <SheetTitle>TRES 资源目录</SheetTitle>
            <SheetDescription>
              目录由共享 SlurmDBD
              注册。显示已登记资源，具体跟踪与限额执行由站点管理员配置。
            </SheetDescription>
          </SheetHeader>
          <div className="px-5 pb-6">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>资源</TableHead>
                  <TableHead>基础单位</TableHead>
                  <TableHead>管理</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.data?.resources.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell>{r.id}</TableCell>
                    <TableCell className="font-mono">{r.key}</TableCell>
                    <TableCell>{r.unit}</TableCell>
                    <TableCell>{r.editable ? "可配置" : "查询"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </SheetContent>
      </Sheet>
      {editing && target && canEdit && !protectedTarget(target) && (
        <TRESEditor
          key={`${clusterId}:${kind}:${target.id}:${bucket}`}
          clusterId={clusterId}
          target={target}
          resources={resources}
          initialBucket={bucket}
          onClose={() => setEditing(false)}
          onOperation={openOperation}
        />
      )}
      {has("platform.audit.read") && (
        <Button variant="link" className="px-0" asChild>
          <Link to="/audits">查看平台操作审计</Link>
        </Button>
      )}
    </div>
  )
}
const searchSchema = z.object({
  q: z.string().trim().max(128, "最多 128 个字符"),
})
function TRESSearch({
  q,
  onSearch,
}: {
  q: string
  onSearch: (q: string) => void
}) {
  const form = useForm<z.infer<typeof searchSchema>>({
    resolver: zodResolver(searchSchema),
    defaultValues: { q },
  })
  return (
    <form
      className="rounded-lg border bg-card p-4"
      onSubmit={form.handleSubmit((v) => onSearch(v.q))}
    >
      <Label htmlFor="tres-search">搜索限额对象</Label>
      <div className="mt-2 flex flex-wrap gap-2">
        <Input
          id="tres-search"
          className="min-w-40 flex-1"
          placeholder="名称、账户、用户名或 ID"
          {...form.register("q")}
        />
        <Button type="submit" variant="outline">
          筛选
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={() => {
            form.reset({ q: "" })
            onSearch("")
          }}
        >
          清除筛选
        </Button>
      </div>
      {form.formState.errors.q && (
        <p role="alert" className="mt-2 text-xs text-destructive">
          {form.formState.errors.q.message}
        </p>
      )}
    </form>
  )
}
