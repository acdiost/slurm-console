import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Gauge, Plus, RefreshCw, Scale } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
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
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  ClusterPicker,
  useAccessibleClusters,
  pageNumber,
} from "@/features/clusters/cluster-picker"
import { PolicyTable } from "@/features/policies/policy-table"
import { QOSEditor } from "@/features/policies/editor"
import {
  qosPageSchema,
  fairsharePageSchema,
  qosDetailSchema,
  policyOperationSchema,
  formatNumber,
  statuses,
  errorMessage,
  type QOS,
  type Fairshare,
} from "@/features/policies/schema"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"

export function PoliciesPage() {
  const [params, setParams] = useSearchParams(),
    selection = useAccessibleClusters(),
    clusterId = selection.id,
    user = useAuth((s) => s.user)
  const [editing, setEditing] = useState<QOS | "create" | null>(null)
  const kind = params.get("tab") === "fairshare" ? "fairshare" : "qos",
    page = pageNumber(params.get("page")),
    q = params.get("q") || "",
    item = params.get("item") || "",
    operation = params.get("operation") || ""
  const permissions = [
    ...(user?.access.platformPermissions ?? []),
    ...(user?.access.clusters.find((c) => c.cluster.id === clusterId)
      ?.permissions ?? []),
  ]
  const has = (...values: string[]) =>
    values.every((p) => permissions.includes(p))
  const global = has("platform.qos.read"),
    canEdit = global && has("platform.qos.manage", "qos.read", "qos.update"),
    canRead = has(
      "associations.read",
      kind === "qos" ? "qos.read" : "fairshare.read"
    )
  const update = (changes: Record<string, string | null>) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      for (const [key, value] of Object.entries(changes)) {
        if (value === null || value === "") next.delete(key)
        else next.set(key, value)
      }
      return next
    })
  const filter = new URLSearchParams({
    page: String(page),
    perPage: "20",
    sort: params.get("sort") || (kind === "qos" ? "name" : "account"),
    order: params.get("order") || "asc",
  })
  if (q) filter.set("q", q)
  if (kind === "fairshare")
    for (const key of ["account", "user", "kind"])
      if (params.get(key)) filter.set(key, params.get(key)!)
  const query = useQuery({
    queryKey: ["policies", clusterId, kind, filter.toString()],
    enabled: !!clusterId && canRead,
    retry: false,
    staleTime: kind === "qos" ? 60000 : 15000,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) => {
      const raw = await api(
        `/clusters/${encodeURIComponent(clusterId)}/${kind}?${filter}`,
        { signal }
      )
      return kind === "qos"
        ? qosPageSchema.parse(raw)
        : fairsharePageSchema.parse(raw)
    },
  })
  const detail = useQuery({
    queryKey: ["policies", clusterId, "qos-detail", item],
    enabled: !!clusterId && !!item && kind === "qos" && canRead,
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      qosDetailSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/qos/${encodeURIComponent(item)}`,
          { signal }
        )
      ),
  })
  const recent = useQuery({
    queryKey: ["policies", clusterId, "recent"],
    enabled: !!clusterId && global && has("qos.read"),
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      z
        .object({ items: z.array(policyOperationSchema) })
        .parse(
          await api(
            `/clusters/${encodeURIComponent(clusterId)}/qos-operations`,
            { signal }
          )
        ),
  })
  const result = useQuery({
    queryKey: ["policies", clusterId, "operation", operation],
    enabled: !!clusterId && !!operation && global && has("qos.read"),
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      policyOperationSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/accounting-operations/${encodeURIComponent(operation)}`,
          { signal }
        )
      ),
  })
  const rows = query.isError || !canRead ? [] : (query.data?.items ?? []),
    qosDetail = detail.isError ? undefined : detail.data?.item,
    fairDetail =
      kind === "fairshare"
        ? (rows as Fairshare[]).find((v) => v.id === item)
        : undefined
  const sort = (key: string) =>
    update({
      sort: key,
      order:
        filter.get("sort") === key && filter.get("order") === "asc"
          ? "desc"
          : "asc",
      page: "1",
    })
  const onOperation = (id: string) =>
    update({ operation: id, cluster: clusterId })
  const refresh = () => {
    if (canRead) void query.refetch()
    if (global) {
      void recent.refetch()
      if (operation) void result.refetch()
    }
    if (item && kind === "qos" && canRead) void detail.refetch()
  }
  return (
    <div className="space-y-6 p-4 md:p-6 lg:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="eyebrow">SCHEDULING POLICY</p>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight">
            QOS 与 Fairshare
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            查看调度服务等级、作业限制和当前公平份额。
          </p>
        </div>
        <Button
          variant="outline"
          onClick={refresh}
          disabled={!canRead || query.isFetching}
        >
          <RefreshCw
            className={query.isFetching ? "size-4 animate-spin" : "size-4"}
          />
          刷新策略数据
        </Button>
      </header>
      <ClusterPicker
        selection={selection}
        label="策略"
        onSwitch={() => setEditing(null)}
      />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Tabs
          value={kind}
          onValueChange={(tab) => {
            setEditing(null)
            update({
              tab,
              page: "1",
              q: null,
              item: null,
              sort: null,
              order: null,
              account: null,
              user: null,
              kind: null,
            })
          }}
        >
          <TabsList>
            <TabsTrigger value="qos">
              <Gauge className="size-4" />
              QOS
            </TabsTrigger>
            <TabsTrigger value="fairshare">
              <Scale className="size-4" />
              Fairshare
            </TabsTrigger>
          </TabsList>
        </Tabs>
        {kind === "qos" && canEdit && canRead && (
          <Button
            disabled={!clusterId || query.isError}
            onClick={() => setEditing("create")}
          >
            <Plus className="size-4" />
            创建 QOS
          </Button>
        )}
      </div>
      <p className="rounded-lg border bg-card p-3 text-sm text-muted-foreground">
        {kind === "qos"
          ? global
            ? "当前可查看此连接共享记账库的全部 QOS。定义及修改可能影响多个集群，写入需要平台 QOS 管理权限。"
            : "当前仅展示所选集群 Association 引用的 QOS；未引用策略需要平台目录读取权限。"
          : "Fairshare 由控制器实时采样。权重、规范化用量与公平份额因子参与集群配置指定的优先级计算；实际调度还取决于其他因子。累计用量使用 Slurm 原始记账单位。"}
      </p>
      <PolicySearch
        key={`${kind}:${q}`}
        q={q}
        onSearch={(value) => update({ q: value, page: "1", item: null })}
      />
      {kind === "fairshare" && (
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <Label htmlFor="fairshare-kind">关联层级</Label>
          <Select
            value={params.get("kind") || "all"}
            onValueChange={(value) =>
              update({
                kind: value === "all" ? null : value,
                page: "1",
                item: null,
              })
            }
          >
            <SelectTrigger id="fairshare-kind" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部层级</SelectItem>
              <SelectItem value="account">Account 层级</SelectItem>
              <SelectItem value="user">用户层级</SelectItem>
            </SelectContent>
          </Select>
          {(params.get("account") || params.get("user")) && (
            <Button
              variant="ghost"
              onClick={() =>
                update({ account: null, user: null, page: "1", item: null })
              }
            >
              清除关联筛选
            </Button>
          )}
        </div>
      )}
      {!canRead ? (
        <p
          role="alert"
          className="rounded-lg border p-4 text-sm text-muted-foreground"
        >
          没有此集群的{kind === "qos" ? "QOS" : "Fairshare"}及关联读取权限。
        </p>
      ) : query.isError ? (
        <p
          role="alert"
          className="rounded-lg border border-destructive/30 p-4 text-sm text-destructive"
        >
          {errorMessage(query.error)}
        </p>
      ) : query.isPending ? (
        <Skeleton className="h-64 rounded-lg" />
      ) : (
        <section className="overflow-hidden rounded-lg border bg-card">
          <div className="flex items-center justify-between gap-3 border-b px-4 py-3">
            <h2 className="font-semibold">
              {kind === "qos" ? "QOS" : "Fairshare"} ·{" "}
              {query.data?.totalItems ?? 0}
            </h2>
            <span className="text-xs text-muted-foreground">
              {query.data?.cached ? "缓存采样" : "实时查询"} ·{" "}
              {query.data?.fetchedAt
                ? new Date(query.data.fetchedAt).toLocaleTimeString("zh-CN", {
                    hour12: false,
                  })
                : "—"}
            </span>
          </div>
          <PolicyTable
            kind={kind}
            items={rows}
            sortKey={filter.get("sort")!}
            order={filter.get("order")!}
            onSort={sort}
            onOpen={(id) => update({ item: id, cluster: clusterId })}
          />
          <div className="flex items-center justify-end gap-3 border-t p-3">
            <Button
              variant="outline"
              size="sm"
              aria-label="上一页策略记录"
              disabled={page <= 1}
              onClick={() => update({ page: String(page - 1), item: null })}
            >
              上一页
            </Button>
            <span className="text-xs text-muted-foreground">第 {page} 页</span>
            <Button
              variant="outline"
              size="sm"
              aria-label="下一页策略记录"
              disabled={page * 20 >= (query.data?.totalItems ?? 0)}
              onClick={() => update({ page: String(page + 1), item: null })}
            >
              下一页
            </Button>
          </div>
        </section>
      )}
      {global && has("qos.read") && (
        <section
          className="space-y-3 rounded-lg border p-4"
          aria-label="最近 QOS 操作"
        >
          <h2 className="font-semibold">本人最近 QOS 操作</h2>
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
                  onClick={() => onOperation(v.operationId)}
                >
                  {v.action === "qos_create" ? "创建" : "编辑"} ·{" "}
                  {v.spec.qos.name} · {statuses[v.status] || v.status}
                </Button>
              ))}
              {recent.data?.items.length === 0 && (
                <p className="text-sm text-muted-foreground">暂无 QOS 操作。</p>
              )}
            </div>
          )}
        </section>
      )}
      {operation && global && (
        <section
          className="space-y-3 rounded-lg border p-4"
          aria-label="QOS 操作结果"
        >
          <div className="flex items-center justify-between">
            <h2 className="font-semibold">QOS 操作结果</h2>
            <Button
              variant="outline"
              size="sm"
              disabled={result.isFetching}
              onClick={() => void result.refetch()}
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
              <div className="space-y-2">
                <Badge variant="outline">
                  {statuses[result.data.status] || result.data.status}
                </Badge>
                <p className="text-sm">{result.data.message}</p>
                <p className="text-xs text-muted-foreground">
                  {result.data.spec.qos.name} · {result.data.reason}
                </p>
              </div>
            )
          )}
        </section>
      )}
      <Sheet
        open={!!item}
        onOpenChange={(open) => {
          if (!open) update({ item: null })
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-lg">
          <SheetHeader>
            <SheetTitle>
              {kind === "qos"
                ? qosDetail?.name || "QOS 详情"
                : "Fairshare 详情"}
            </SheetTitle>
            <SheetDescription>
              {kind === "qos"
                ? "共享记账库的 QOS 定义。时限以分钟计；空值与无限制分别显示。"
                : "所选集群的控制器采样与对应 Association。"}
            </SheetDescription>
          </SheetHeader>
          <div className="space-y-5 px-5 pb-6">
            {kind === "qos" ? (
              detail.isError ? (
                <p role="alert" className="text-sm text-destructive">
                  {errorMessage(detail.error)}
                </p>
              ) : qosDetail ? (
                <>
                  <dl className="space-y-3 text-sm">
                    {Object.entries({
                      ID: qosDetail.id,
                      描述: qosDetail.description,
                      优先级: formatNumber(qosDetail.priority),
                      计费用量因子: formatNumber(qosDetail.usageFactor),
                      每用户运行作业上限: formatNumber(
                        qosDetail.maxJobsPerUser
                      ),
                      每用户提交作业上限: formatNumber(
                        qosDetail.maxSubmitJobsPerUser
                      ),
                      "单作业时限（分钟）": formatNumber(
                        qosDetail.maxWallMinutes
                      ),
                      Flags: qosDetail.flags.join(" / ") || "无",
                      抢占模式: qosDetail.preemptModes.join(" / ") || "未提供",
                    }).map(([label, value]) => (
                      <div key={label} className="grid grid-cols-2 gap-3">
                        <dt className="text-muted-foreground">{label}</dt>
                        <dd className="font-mono break-all">{value}</dd>
                      </div>
                    ))}
                  </dl>
                  {canEdit && (
                    <Button onClick={() => setEditing(qosDetail)}>
                      编辑 QOS
                    </Button>
                  )}
                </>
              ) : (
                <Skeleton className="h-48" />
              )
            ) : fairDetail ? (
              <>
                <dl className="space-y-3 text-sm">
                  {Object.entries({
                    "Association ID": fairDetail.id,
                    集群: fairDetail.cluster,
                    Account: fairDetail.account,
                    用户: fairDetail.user || "Account 层级",
                    父级: fairDetail.parent || "—",
                    分区: fairDetail.partition || "通用关联",
                    权重: formatNumber(fairDetail.shares),
                    规范份额: formatNumber(fairDetail.normalizedShares),
                    累计用量: BigInt(fairDetail.rawUsage).toLocaleString(
                      "zh-CN"
                    ),
                    规范用量: formatNumber(fairDetail.normalizedUsage),
                    有效用量: formatNumber(fairDetail.effectiveUsage),
                    Fairshare: formatNumber(fairDetail.factor),
                    层级因子: formatNumber(fairDetail.level),
                  }).map(([label, value]) => (
                    <div key={label} className="grid grid-cols-2 gap-3">
                      <dt className="text-muted-foreground">{label}</dt>
                      <dd className="font-mono break-all">{value}</dd>
                    </div>
                  ))}
                </dl>
                <Button variant="outline" asChild>
                  <Link
                    to={`/accounts?${new URLSearchParams({ cluster: clusterId, tab: "associations", item: fairDetail.id, account: fairDetail.account })}`}
                  >
                    查看 Association
                  </Link>
                </Button>
              </>
            ) : (
              <p className="text-sm text-muted-foreground">
                该记录不在当前采样页中，请关闭详情后重新查询。
              </p>
            )}
          </div>
        </SheetContent>
      </Sheet>
      {editing && canEdit && (
        <QOSEditor
          key={`${clusterId}:${editing === "create" ? "create" : editing.id}`}
          clusterId={clusterId}
          initial={editing === "create" ? undefined : editing}
          onClose={() => setEditing(null)}
          onOperation={onOperation}
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
function PolicySearch({
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
      <Label htmlFor="policy-search">搜索策略记录</Label>
      <div className="mt-2 flex flex-wrap gap-2">
        <Input
          id="policy-search"
          className="min-w-40 flex-1"
          placeholder="名称、描述、账户或用户"
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
