import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { RefreshCw, Plus, Database, Users, Link2 } from "lucide-react"
import { z } from "zod"
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
  ClusterPicker,
  useAccessibleClusters,
  pageNumber,
} from "@/features/clusters/cluster-picker"
import { AccountingTable } from "@/features/accounting/accounting-table"
import { AccountingManagement } from "@/features/accounting/management"
import {
  actions,
  kinds,
  pageSchema,
  detailSchema,
  operationSchema,
  statuses,
  errorMessage,
  type Action,
  type Kind,
  type Spec,
  type Account,
  type SlurmUser,
  type Association,
} from "@/features/accounting/schema"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"

export function AccountingPage() {
  const [params, setParams] = useSearchParams(),
    selection = useAccessibleClusters(),
    clusterId = selection.id,
    client = useQueryClient(),
    user = useAuth((s) => s.user)
  const [editing, setEditing] = useState<{
    action: Action
    initial?: Partial<Spec>
  } | null>(null)
  const kind: Kind =
    params.get("tab") === "slurm-users"
      ? "slurm-users"
      : params.get("tab") === "associations"
        ? "associations"
        : "accounts"
  const page = pageNumber(params.get("page")),
    q = params.get("q") || "",
    item = params.get("item") || "",
    operation = params.get("operation") || ""
  const update = (changes: Record<string, string | null>) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      for (const [key, value] of Object.entries(changes)) {
        if (value === null || value === "") next.delete(key)
        else next.set(key, value)
      }
      return next
    })
  const permissions = [
    ...(user?.access.platformPermissions ?? []),
    ...(user?.access.clusters.find((c) => c.cluster.id === clusterId)
      ?.permissions ?? []),
  ]
  const has = (...required: string[]) =>
    required.every((p) => permissions.includes(p))
  const global = has("platform.accounting.manage")
  const canCreateAccount = global && has("accounts.read", "accounts.update")
  const canEditAccount = canCreateAccount
  const canCreateUser =
    global &&
    has(
      "slurm_users.read",
      "accounts.read",
      "associations.read",
      "associations.update"
    )
  const canCreateAssociation = has(
    "associations.read",
    "associations.update",
    "accounts.read",
    "slurm_users.read"
  )
  const canRemoveAssociation = has(
    "associations.read",
    "associations.update",
    "jobs.read"
  )
  const filter = new URLSearchParams({
    page: String(page),
    perPage: "20",
    sort: params.get("sort") || (kind === "associations" ? "id" : "name"),
    order: params.get("order") || "asc",
  })
  if (q) filter.set("q", q)
  if (kind === "associations")
    for (const key of ["account", "user", "partition", "kind"])
      if (params.get(key)) filter.set(key, params.get(key)!)
  const query = useQuery({
    queryKey: ["accounting", clusterId, kind, filter.toString()],
    enabled: !!clusterId,
    queryFn: async ({ signal }) =>
      pageSchema(kind).parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/${kind}?${filter}`,
          { signal }
        )
      ),
    retry: false,
    staleTime: 60000,
    refetchOnWindowFocus: false,
  })
  const detail = useQuery({
    queryKey: ["accounting", clusterId, kind, "detail", item],
    enabled: !!clusterId && !!item,
    queryFn: async ({ signal }) =>
      detailSchema(kind).parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/${kind}/${encodeURIComponent(item)}`,
          { signal }
        )
      ),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const recent = useQuery({
    queryKey: ["accounting", clusterId, "recent"],
    enabled: !!clusterId,
    queryFn: async ({ signal }) =>
      z
        .object({ items: z.array(operationSchema) })
        .parse(
          await api(
            `/clusters/${encodeURIComponent(clusterId)}/accounting-operations`,
            { signal }
          )
        ),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const result = useQuery({
    queryKey: ["accounting-operation", clusterId, operation],
    enabled: !!clusterId && !!operation,
    queryFn: async ({ signal }) =>
      operationSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/accounting-operations/${encodeURIComponent(operation)}`,
          { signal }
        )
      ),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const rows = query.isError ? [] : (query.data?.items ?? []),
    value = detail.isError ? undefined : detail.data?.item
  const sort = (key: string) =>
    update({
      sort: key,
      order:
        filter.get("sort") === key && filter.get("order") === "asc"
          ? "desc"
          : "asc",
      page: "1",
    })
  const open = (id: string) => update({ item: id, cluster: clusterId })
  const create =
    kind === "accounts" && canCreateAccount
      ? "account_create"
      : kind === "slurm-users" && canCreateUser
        ? "user_create"
        : kind === "associations" && canCreateAssociation
          ? "association_create"
          : null
  const relation = (account?: string, username?: string) =>
    update({
      tab: "associations",
      item: null,
      page: "1",
      q: null,
      sort: null,
      order: null,
      account: account || null,
      user: username || null,
    })
  return (
    <div className="space-y-6 p-4 md:p-6 lg:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="eyebrow">ACCOUNTING & ACCESS</p>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight">
            账户与 Association
          </h1>
          <p className="mt-2 max-w-3xl text-sm text-muted-foreground">
            查看集群的 Account、Slurm
            记账用户与关联关系，按确认流程管理调度身份。
          </p>
        </div>
        <Button
          variant="outline"
          disabled={query.isFetching}
          onClick={() => {
            void query.refetch()
            void recent.refetch()
          }}
        >
          <RefreshCw
            className={query.isFetching ? "size-4 animate-spin" : "size-4"}
          />
          刷新记账数据
        </Button>
      </header>
      <ClusterPicker
        selection={selection}
        label="记账"
        onSwitch={() => setEditing(null)}
      />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Tabs
          value={kind}
          onValueChange={(v) => {
            setEditing(null)
            update({
              tab: v,
              page: "1",
              item: null,
              q: null,
              account: null,
              user: null,
              partition: null,
              kind: null,
              sort: null,
              order: null,
            })
          }}
        >
          <TabsList>
            <TabsTrigger value="accounts">
              <Database className="size-4" />
              Account
            </TabsTrigger>
            <TabsTrigger value="slurm-users">
              <Users className="size-4" />
              Slurm 用户
            </TabsTrigger>
            <TabsTrigger value="associations">
              <Link2 className="size-4" />
              Association
            </TabsTrigger>
          </TabsList>
        </Tabs>
        {create && (
          <Button
            onClick={() => setEditing({ action: create })}
            disabled={!clusterId || query.isError || query.isPending}
          >
            <Plus className="size-4" />
            {actions[create]}
          </Button>
        )}
      </div>
      <section className="panel p-4">
        <form
          className="flex flex-wrap items-end gap-3"
          key={`${kind}:${q}:${params.get("account")}:${params.get("user")}`}
          onSubmit={(e) => {
            e.preventDefault()
            const form = new FormData(e.currentTarget)
            update({
              q: String(form.get("q") || ""),
              page: "1",
              ...(kind === "associations"
                ? {
                    account: String(form.get("account") || ""),
                    user: String(form.get("user") || ""),
                    partition: String(form.get("partition") || ""),
                  }
                : {}),
            })
          }}
        >
          <div className="min-w-48 flex-1 space-y-1.5">
            <Label htmlFor="accounting-search">搜索{kinds[kind]}</Label>
            <Input
              id="accounting-search"
              name="q"
              maxLength={128}
              defaultValue={q}
              placeholder="名称、描述或关联信息"
            />
          </div>
          {kind === "associations" &&
            [
              ["account", "Account"],
              ["user", "Slurm 用户"],
              ["partition", "分区"],
            ].map(([key, label]) => (
              <div className="w-40 space-y-1.5" key={key}>
                <Label htmlFor={`filter-${key}`}>{label}</Label>
                <Input
                  id={`filter-${key}`}
                  name={key}
                  maxLength={64}
                  defaultValue={params.get(key) || ""}
                />
              </div>
            ))}
          <Button type="submit" variant="secondary">
            筛选
          </Button>
          <Button
            type="button"
            variant="ghost"
            onClick={() =>
              update({
                q: null,
                account: null,
                user: null,
                partition: null,
                kind: null,
                page: "1",
              })
            }
          >
            清除筛选
          </Button>
        </form>
      </section>
      {query.isError ? (
        <section role="alert" className="panel border-destructive/30 p-6">
          <h2 className="font-medium">记账数据暂不可用</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            {errorMessage(query.error)}
          </p>
          <Button
            className="mt-4"
            variant="outline"
            onClick={() => void query.refetch()}
          >
            重新查询
          </Button>
        </section>
      ) : query.isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <section className="panel overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3 text-sm">
            <h2 className="font-medium">
              {kinds[kind]} · {query.data?.totalItems ?? 0}
            </h2>
            <span className="text-xs text-muted-foreground">
              {query.data?.cached ? "最近查询缓存" : "实时查询"} ·{" "}
              {query.data?.fetchedAt
                ? new Date(query.data.fetchedAt).toLocaleTimeString("zh-CN", {
                    hour12: false,
                  })
                : "—"}
            </span>
          </div>
          <AccountingTable
            kind={kind}
            items={rows}
            sortKey={filter.get("sort") || "name"}
            order={filter.get("order") || "asc"}
            onSort={sort}
            onOpen={open}
          />
          <div className="flex items-center justify-end gap-3 border-t px-4 py-3">
            <Button
              variant="outline"
              size="sm"
              disabled={page <= 1}
              aria-label="上一页记账记录"
              onClick={() => update({ page: String(page - 1) })}
            >
              上一页
            </Button>
            <span className="text-xs text-muted-foreground">第 {page} 页</span>
            <Button
              variant="outline"
              size="sm"
              aria-label="下一页记账记录"
              disabled={page * 20 >= (query.data?.totalItems ?? 0)}
              onClick={() => update({ page: String(page + 1) })}
            >
              下一页
            </Button>
          </div>
        </section>
      )}
      <p className="text-xs leading-relaxed text-muted-foreground">
        Account 和 Slurm
        用户名称属于共享记账库；列表只展示关联所选集群的记录。Slurm
        记账身份、Linux 用户及平台登录账户分别管理。
      </p>
      {operation && (
        <section aria-label="记账操作结果" className="panel space-y-3 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="font-medium">记账操作结果</h2>
            <Button
              variant="outline"
              size="sm"
              disabled={result.isFetching}
              onClick={() => void result.refetch()}
            >
              只读核实操作
            </Button>
          </div>
          <p className="font-mono text-xs break-all">{operation}</p>
          {result.isError ? (
            <p role="alert" className="text-sm text-destructive">
              {errorMessage(result.error)}{" "}
              未收到结果时请稍后只读核实，勿重复创建操作。
            </p>
          ) : result.data ? (
            <>
              <Badge
                variant={
                  result.data.status === "confirmed" ? "secondary" : "outline"
                }
              >
                {statuses[result.data.status] || result.data.status}
              </Badge>
              <p className="text-sm">{result.data.message}</p>
              <p className="text-xs text-muted-foreground">
                {actions[result.data.action as Action]} ·{" "}
                {result.data.spec.user || result.data.spec.account} ·{" "}
                {result.data.slurmCluster}
              </p>
            </>
          ) : (
            <p className="text-sm text-muted-foreground">正在查询操作记录…</p>
          )}
        </section>
      )}
      {!!recent.data?.items.length && (
        <section className="panel space-y-3 p-4" aria-label="最近记账操作">
          <h2 className="text-sm font-semibold">本人最近记账操作</h2>
          <div className="flex flex-wrap gap-2">
            {recent.data.items.map((v) => (
              <Button
                key={v.operationId}
                variant="outline"
                size="sm"
                onClick={() => update({ operation: v.operationId })}
              >
                {actions[v.action as Action] || v.action} ·{" "}
                {v.spec.user || v.spec.account} ·{" "}
                {statuses[v.status] || v.status}
              </Button>
            ))}
          </div>
        </section>
      )}
      <Sheet
        open={!!item}
        onOpenChange={(open) => {
          if (!open) update({ item: null })
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-xl">
          <SheetHeader>
            <SheetTitle>{kinds[kind]} 详情</SheetTitle>
            <SheetDescription className="font-mono">{item}</SheetDescription>
          </SheetHeader>
          <div className="space-y-5 p-4">
            {detail.isError ? (
              <p role="alert" className="text-sm text-destructive">
                {errorMessage(detail.error)}
              </p>
            ) : detail.isPending ? (
              <Skeleton className="h-48" />
            ) : value ? (
              <>
                <dl className="divide-y text-sm">
                  {(kind === "accounts"
                    ? [
                        ["名称", (value as Account).name],
                        ["描述", (value as Account).description],
                        ["组织", (value as Account).organization],
                        ["当前集群关联", (value as Account).associationCount],
                        ["当前集群用户", (value as Account).userCount],
                      ]
                    : kind === "slurm-users"
                      ? [
                          ["Slurm 用户", (value as SlurmUser).name],
                          [
                            "全局默认 Account",
                            (value as SlurmUser).defaultAccountScope ===
                            "outside_cluster"
                              ? "其他集群（已隐藏）"
                              : (value as SlurmUser).defaultAccount,
                          ],
                          [
                            "记账管理级别",
                            (value as SlurmUser).administratorLevel.join(" / "),
                          ],
                          [
                            "当前集群关联",
                            (value as SlurmUser).associationCount,
                          ],
                        ]
                      : [
                          ["ID", (value as Association).id],
                          ["Slurm 集群", (value as Association).cluster],
                          ["Account", (value as Association).account],
                          [
                            "Slurm 用户",
                            (value as Association).user || "Account 层级",
                          ],
                          ["分区", (value as Association).partition || "通用"],
                          ["父 Account", (value as Association).parentAccount],
                          [
                            "默认关联",
                            (value as Association).isDefault ? "是" : "否",
                          ],
                          ["注释", (value as Association).comment],
                        ]
                  ).map(([label, text]) => (
                    <div
                      className="grid grid-cols-[8rem_1fr] gap-4 py-3"
                      key={label}
                    >
                      <dt className="text-muted-foreground">{label}</dt>
                      <dd className="break-words">{text || "—"}</dd>
                    </div>
                  ))}
                </dl>
                <div className="flex flex-wrap gap-2">
                  {kind === "associations" && has("tres.read") && (
                    <Button variant="outline" asChild>
                      <Link
                        to={`/tres?${new URLSearchParams({ cluster: clusterId, target: "association", item: (value as Association).id })}`}
                      >
                        查看 TRES 限额
                      </Link>
                    </Button>
                  )}
                  {kind === "accounts" && has("associations.read") && (
                    <Button
                      variant="outline"
                      onClick={() => relation((value as Account).name)}
                    >
                      查看此 Account 的关联
                    </Button>
                  )}
                  {kind === "slurm-users" && has("associations.read") && (
                    <Button
                      variant="outline"
                      onClick={() =>
                        relation(undefined, (value as SlurmUser).name)
                      }
                    >
                      查看此用户的关联
                    </Button>
                  )}
                  {kind === "accounts" &&
                    canEditAccount &&
                    (value as Account).name !== "root" && (
                      <Button
                        onClick={() => {
                          const a = value as Account
                          setEditing({
                            action: "account_update",
                            initial: {
                              account: a.name,
                              description: a.description,
                              organization: a.organization,
                            },
                          })
                        }}
                      >
                        编辑 Account
                      </Button>
                    )}
                  {kind === "associations" &&
                    canRemoveAssociation &&
                    (value as Association).user &&
                    (value as Association).user !== "root" &&
                    !(value as Association).isDefault && (
                      <Button
                        variant="destructive"
                        onClick={() => {
                          const a = value as Association
                          setEditing({
                            action: "association_remove",
                            initial: {
                              account: a.account,
                              user: a.user,
                              partition: a.partition,
                              associationId: a.id,
                            },
                          })
                        }}
                      >
                        校验并移除关联
                      </Button>
                    )}
                </div>
              </>
            ) : null}
          </div>
        </SheetContent>
      </Sheet>
      {editing && (
        <AccountingManagement
          key={`${clusterId}:${editing.action}:${JSON.stringify(editing.initial)}`}
          clusterId={clusterId}
          {...editing}
          onClose={() => setEditing(null)}
          onOperation={(id) => {
            update({ operation: id })
            void client.invalidateQueries({
              queryKey: ["accounting-operation", clusterId, id],
            })
          }}
        />
      )}
      {user?.access.platformPermissions.includes("platform.audit.read") && (
        <Link
          to="/audits"
          className="text-xs text-primary underline-offset-4 hover:underline"
        >
          查看平台操作审计
        </Link>
      )}
    </div>
  )
}
