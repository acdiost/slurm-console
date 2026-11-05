import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useSearchParams } from "react-router"
import {
  Network,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  Users,
} from "lucide-react"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { refreshAccess } from "@/stores/auth"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet"
import { catalogSchema } from "@/features/platform-settings/schema"
import { pageNumber } from "@/features/clusters/cluster-picker"
import {
  infoSchema,
  userPageSchema,
  groupPageSchema,
  mappingsSchema,
  operationSchema,
  operationsSchema,
  draft,
  actionNames,
  type Draft,
  type Mapping,
  type Operation,
} from "@/features/freeipa/schema"
const selectClass = "h-9 rounded-md border bg-card px-3 text-xs"
export function FreeIPAPage() {
  const user = useAuth((s) => s.user)
  if (!user?.access.platformPermissions.includes("platform.directory.read"))
    return (
      <div role="alert" className="p-8">
        当前没有 FreeIPA 目录读取权限。
      </div>
    )
  return (
    <Directory
      key={user.id}
      actor={user.id}
      manage={user.access.platformPermissions.includes(
        "platform.directory.manage"
      )}
      roles={user.access.platformPermissions.includes("platform.roles.manage")}
    />
  )
}
function Directory({
  actor,
  manage,
  roles,
}: {
  actor: string
  manage: boolean
  roles: boolean
}) {
  const [params, setParams] = useSearchParams(),
    client = useQueryClient()
  const tab =
      params.get("tab") === "groups"
        ? "groups"
        : params.get("tab") === "mappings" && roles
          ? "mappings"
          : "users",
    page = pageNumber(params.get("page")),
    q = params.get("q") || ""
  const [search, setSearch] = useState(q),
    [editing, setEditing] = useState<Draft | null>(null),
    [latest, setLatest] = useState<Operation | null>(null)
  const info = useQuery({
    queryKey: ["freeipa-info", actor],
    queryFn: async ({ signal }) =>
      infoSchema.parse(await api("/directory", { signal })),
    retry: false,
  })
  const users = useQuery({
    queryKey: ["freeipa-users", actor, page, q],
    enabled: !!info.data?.configured && tab === "users",
    queryFn: async ({ signal }) =>
      userPageSchema.parse(
        await api(
          `/directory/users?${new URLSearchParams({ page: String(page), perPage: "20", q })}`,
          { signal }
        )
      ),
    retry: false,
  })
  const groups = useQuery({
    queryKey: ["freeipa-groups", actor, page, q],
    enabled: !!info.data?.configured && tab === "groups",
    queryFn: async ({ signal }) =>
      groupPageSchema.parse(
        await api(
          `/directory/groups?${new URLSearchParams({ page: String(page), perPage: "20", q })}`,
          { signal }
        )
      ),
    retry: false,
  })
  const mappings = useQuery({
    queryKey: ["freeipa-mappings", actor],
    enabled: !!info.data?.configured && roles && tab === "mappings",
    queryFn: async ({ signal }) =>
      mappingsSchema.parse(await api("/directory/role-mappings", { signal })),
    retry: false,
  })
  const operations = useQuery({
    queryKey: ["freeipa-operations", actor],
    enabled: !!info.data?.configured && manage,
    queryFn: async ({ signal }) =>
      operationsSchema.parse(await api("/directory/operations", { signal })),
    retry: false,
  })
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ["freeipa-users"] })
    void client.invalidateQueries({ queryKey: ["freeipa-groups"] })
    void client.invalidateQueries({ queryKey: ["freeipa-operations"] })
    void client.invalidateQueries({ queryKey: ["freeipa-mappings"] })
  }
  const active = tab === "groups" ? groups : users,
    data = !active.isError ? active.data : undefined
  const changeTab = (v: string) => {
    setParams({ tab: v })
    setSearch("")
    setEditing(null)
  }
  return (
    <div className="mx-auto max-w-screen-2xl space-y-6 p-4 sm:p-8">
      <header className="page-hero flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="section-eyebrow">IDENTITY DIRECTORY</p>
          <h1 className="mt-2 text-2xl font-semibold sm:text-3xl">
            FreeIPA 目录
          </h1>
          <p className="mt-3 text-xs leading-6 text-muted-foreground">
            统一查询和管理目录身份，通过明确的组映射授予平台角色。
          </p>
        </div>
        <Network className="hidden size-9 text-primary sm:block" />
      </header>
      {info.isPending && <p role="status">正在读取目录配置…</p>}
      {info.isError && (
        <p role="alert" className="panel p-5">
          目录配置不可用或访问权限已变化。
        </p>
      )}
      {info.data && !info.data.configured && (
        <div className="panel space-y-2 p-6">
          <h2 className="font-semibold">尚未接入 FreeIPA</h2>
          <p className="text-xs text-muted-foreground">
            请管理员配置服务端连接及受信任 CA，再刷新此页面。
          </p>
          <Button variant="outline" onClick={() => void info.refetch()}>
            刷新配置
          </Button>
        </div>
      )}
      {!info.isError && info.data?.configured && (
        <>
          <div className="flex flex-wrap items-center gap-3 text-xs">
            <Badge variant="outline">{info.data.domain}</Badge>
            <span className="font-mono text-muted-foreground">
              {info.data.realm}
            </span>
            <span className="text-muted-foreground">
              配置已加载 · 连通性以查询结果为准
            </span>
          </div>
          <nav aria-label="FreeIPA 目录分类" className="flex flex-wrap gap-2">
            {[
              ["users", "目录用户"],
              ["groups", "目录组"],
              ...(roles ? [["mappings", "组角色映射"]] : []),
            ].map(([v, label]) => (
              <Button
                key={v}
                variant={tab === v ? "default" : "outline"}
                aria-current={tab === v ? "page" : undefined}
                onClick={() => changeTab(v)}
              >
                {v === "users" ? (
                  <Users />
                ) : v === "groups" ? (
                  <Network />
                ) : (
                  <ShieldCheck />
                )}
                {label}
              </Button>
            ))}
          </nav>
          {tab !== "mappings" ? (
            <section className="panel">
              <div className="panel-heading">
                <form
                  className="flex min-w-0 flex-wrap gap-2"
                  onSubmit={(e) => {
                    e.preventDefault()
                    setParams({ tab, q: search.trim(), page: "1" })
                  }}
                >
                  <Input
                    aria-label="搜索 FreeIPA 目录"
                    placeholder="用户名、姓名或组名称"
                    maxLength={100}
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className="w-60 max-w-full"
                  />
                  <Button type="submit" variant="outline">
                    <Search />
                    查询目录
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    aria-label="刷新目录"
                    onClick={refresh}
                    disabled={active.isFetching}
                  >
                    <RefreshCw />
                  </Button>
                </form>
                {manage && (
                  <Button
                    onClick={() =>
                      setEditing(
                        draft(tab === "users" ? "user.create" : "group.create")
                      )
                    }
                  >
                    <Plus />
                    {tab === "users" ? "创建目录用户" : "创建目录组"}
                  </Button>
                )}
              </div>
              {active.isPending ? (
                <p role="status" className="p-5">
                  正在读取目录…
                </p>
              ) : active.isError ? (
                <p role="alert" className="p-5 text-destructive">
                  目录查询未完成。请检查连接和权限，或缩小筛选范围。
                </p>
              ) : (
                <>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        {(tab === "users"
                          ? [
                              "用户 / 姓名",
                              "UID / GID",
                              "状态",
                              "直接所属组",
                              "操作",
                            ]
                          : ["组 / 描述", "GID", "直接用户成员", "操作"]
                        ).map((v) => (
                          <TableHead key={v}>{v}</TableHead>
                        ))}
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {tab === "users" &&
                        !users.isError &&
                        users.data?.items.map((u) => (
                          <TableRow key={u.username}>
                            <TableCell>
                              <p className="font-mono font-medium">
                                {u.username}
                              </p>
                              <p className="mt-1 text-2xs text-muted-foreground">
                                {u.firstName} {u.lastName}
                                {u.email ? ` · ${u.email}` : ""}
                              </p>
                            </TableCell>
                            <TableCell className="font-mono text-xs">
                              {u.uid || "—"} / {u.gid || "—"}
                            </TableCell>
                            <TableCell>
                              <Badge
                                variant="outline"
                                className="status-badge"
                                data-tone={
                                  !u.enabled
                                    ? "neutral"
                                    : u.passwordExpired || u.accountExpired
                                      ? "warning"
                                      : "success"
                                }
                              >
                                {!u.enabled
                                  ? "已停用"
                                  : u.accountExpired
                                    ? "账户已过期"
                                    : u.passwordExpired
                                      ? "密码已过期"
                                      : "已启用"}
                              </Badge>
                            </TableCell>
                            <TableCell className="max-w-64 text-xs whitespace-normal">
                              {u.groups.join("、") || "—"}
                            </TableCell>
                            <TableCell>
                              {manage && (
                                <Button
                                  variant="outline"
                                  size="sm"
                                  disabled={u.username === "admin"}
                                  onClick={() =>
                                    setEditing(
                                      draft(
                                        u.enabled
                                          ? "user.disable"
                                          : "user.enable",
                                        u.username,
                                        u.version
                                      )
                                    )
                                  }
                                >
                                  {u.enabled ? "停用" : "启用"}
                                </Button>
                              )}
                            </TableCell>
                          </TableRow>
                        ))}
                      {tab === "groups" &&
                        !groups.isError &&
                        groups.data?.items.map((g) => (
                          <TableRow key={g.name}>
                            <TableCell>
                              <p className="font-mono font-medium">{g.name}</p>
                              <p className="mt-1 text-2xs text-muted-foreground">
                                {g.description || "—"}
                              </p>
                            </TableCell>
                            <TableCell className="font-mono text-xs">
                              {g.gid || "非 POSIX"}
                            </TableCell>
                            <TableCell className="max-w-80 text-xs whitespace-normal">
                              {g.members.join("、") || "暂无直接用户成员"}
                            </TableCell>
                            <TableCell>
                              {manage && (
                                <div className="flex gap-2">
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    onClick={() =>
                                      setEditing(
                                        draft(
                                          "group.add-member",
                                          g.name,
                                          g.version
                                        )
                                      )
                                    }
                                  >
                                    添加成员
                                  </Button>
                                  <Button
                                    size="sm"
                                    variant="ghost"
                                    onClick={() =>
                                      setEditing(
                                        draft(
                                          "group.remove-member",
                                          g.name,
                                          g.version
                                        )
                                      )
                                    }
                                  >
                                    移除成员
                                  </Button>
                                </div>
                              )}
                            </TableCell>
                          </TableRow>
                        ))}
                    </TableBody>
                  </Table>
                  {!data?.items.length && (
                    <p className="p-8 text-center text-xs text-muted-foreground">
                      没有符合条件的目录条目。
                    </p>
                  )}
                  <div className="flex flex-wrap items-center justify-between gap-2 border-t px-5 py-3 text-xs text-muted-foreground">
                    <span>
                      共 {data?.totalItems ?? 0} 条 · 第 {page} 页
                    </span>
                    <div className="flex gap-2">
                      <Button
                        variant="outline"
                        disabled={page <= 1 || active.isFetching}
                        onClick={() =>
                          setParams({ tab, q, page: String(page - 1) })
                        }
                      >
                        上一页
                      </Button>
                      <Button
                        variant="outline"
                        disabled={
                          page * 20 >= (data?.totalItems ?? 0) ||
                          active.isFetching
                        }
                        onClick={() =>
                          setParams({ tab, q, page: String(page + 1) })
                        }
                      >
                        下一页
                      </Button>
                    </div>
                  </div>
                </>
              )}
            </section>
          ) : (
            <MappingPanel
              actor={actor}
              mappings={mappings.isError ? undefined : mappings.data?.items}
              failed={mappings.isError}
              pending={mappings.isPending}
              refresh={refresh}
            />
          )}
          <p className="text-2xs leading-6 text-muted-foreground">
            目录身份、平台账户与 Slurm
            账户独立管理。新建目录用户尚未设置密码，需在 FreeIPA
            中完成凭据配置，再由平台管理员显式关联。停用目录用户会影响后续认证，已有系统进程和
            Slurm 作业不会被终止。
          </p>
          {manage && (
            <section className="panel space-y-3 p-5">
              <div className="flex items-center justify-between gap-2">
                <h2 className="text-sm font-semibold">我最近的目录操作</h2>
                <Button
                  variant="ghost"
                  onClick={() => void operations.refetch()}
                >
                  刷新操作
                </Button>
              </div>
              {latest && (
                <p
                  role="status"
                  className="rounded-md bg-muted/40 p-3 text-xs leading-6"
                >
                  {latest.message}
                  <span className="block font-mono">{latest.operationId}</span>
                </p>
              )}
              {operations.isError ? (
                <p role="alert">操作记录暂不可用。</p>
              ) : !operations.isPending && !operations.data?.items.length ? (
                <p className="text-xs text-muted-foreground">暂无操作记录。</p>
              ) : (
                operations.data?.items.map((o) => (
                  <div
                    key={o.operationId}
                    className="flex flex-wrap items-center justify-between gap-2 border-t pt-3 text-xs"
                  >
                    <div>
                      <span className="font-mono">{o.target}</span> ·{" "}
                      {actionNames[o.action.replace("directory.", "")] ||
                        o.action}
                      <p className="mt-1 font-mono text-2xs text-muted-foreground">
                        {o.operationId}
                      </p>
                    </div>
                    <Badge variant="outline">
                      {{
                        succeeded: "已完成",
                        failed: "已拒绝",
                        processing: "处理中",
                        unknown: "结果待核实",
                      }[o.status] || o.status}
                    </Badge>
                  </div>
                ))
              )}
            </section>
          )}
        </>
      )}
      <Sheet
        open={!!editing}
        onOpenChange={(open) => {
          if (!open) setEditing(null)
        }}
      >
        <SheetContent className="w-full overflow-y-auto sm:max-w-lg">
          <SheetHeader>
            <SheetTitle>
              {editing ? actionNames[editing.action] : "目录操作"}
            </SheetTitle>
            <SheetDescription>
              核对目录目标与影响范围，输入完整目标名称后执行。请求不会自动重试。
            </SheetDescription>
          </SheetHeader>
          {editing && (
            <ChangeForm
              key={editing.operationId}
              initial={editing}
              onSaved={(o) => {
                setLatest(o)
                setEditing(null)
                refresh()
                void refreshAccess().catch(() => {})
              }}
            />
          )}
        </SheetContent>
      </Sheet>
    </div>
  )
}
function ChangeForm({
  initial,
  onSaved,
}: {
  initial: Draft
  onSaved: (o: Operation) => void
}) {
  const [v, set] = useState(initial),
    [pending, setPending] = useState(false),
    [error, setError] = useState(""),
    [uncertain, setUncertain] = useState(false)
  const field = (key: keyof Draft, label: string, maxLength = 100) => (
    <div className="space-y-2">
      <Label htmlFor={`ipa-${key}`}>{label}</Label>
      <Input
        id={`ipa-${key}`}
        value={v[key]}
        maxLength={maxLength}
        required
        onChange={(e) => set({ ...v, [key]: e.target.value })}
      />
    </div>
  )
  const submit = async () => {
    setPending(true)
    setError("")
    try {
      const o = operationSchema.parse(
        await api("/directory/changes", { method: "POST", body: v })
      )
      onSaved(o)
    } catch {
      setUncertain(true)
      setError(
        "请求未确认。请保留操作编号查询状态，勿创建新操作重试。操作也可能被输入校验或权限检查拒绝。"
      )
    } finally {
      setPending(false)
    }
  }
  const check = async () => {
    setPending(true)
    try {
      const o = operationSchema.parse(
        await api(`/directory/operations/${encodeURIComponent(v.operationId)}`)
      )
      onSaved(o)
    } catch {
      setError(
        "暂未查到可确认的操作记录。可关闭面板、核对目录与最近操作后重新发起。"
      )
    } finally {
      setPending(false)
    }
  }
  return (
    <form
      className="space-y-5 p-4"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <fieldset disabled={pending || uncertain} className="space-y-4">
        {initial.action.endsWith("create") ? (
          field(
            "target",
            initial.action === "user.create" ? "目录用户名" : "目录组名称",
            63
          )
        ) : (
          <p className="rounded-lg bg-muted/40 p-3 font-mono text-sm">
            {v.target}
          </p>
        )}
        {v.action === "user.create" && (
          <>
            {field("firstName", "名")}
            {field("lastName", "姓")}
            <div className="space-y-2">
              <Label htmlFor="ipa-email">邮箱（可选）</Label>
              <Input
                id="ipa-email"
                type="email"
                maxLength={255}
                value={v.email}
                onChange={(e) => set({ ...v, email: e.target.value })}
              />
            </div>
            <p className="text-xs text-muted-foreground">
              不设置密码、不自动创建平台账户或授予角色。
            </p>
          </>
        )}
        {v.action === "group.create" && (
          <div className="space-y-2">
            <Label htmlFor="ipa-description">组描述（可选）</Label>
            <Input
              id="ipa-description"
              maxLength={500}
              value={v.description}
              onChange={(e) => set({ ...v, description: e.target.value })}
            />
          </div>
        )}
        {v.action.includes("member") && field("member", "成员用户名", 63)}
        {field("reason", "操作原因", 220)}
        {field("confirmation", `输入 ${v.target || "完整目标名称"} 确认`, 63)}
        <Button
          type="submit"
          disabled={
            v.confirmation !== v.target ||
            !v.target ||
            v.reason.trim().length < 2
          }
        >
          {pending ? "正在提交…" : "确认执行"}
        </Button>
      </fieldset>
      <p className="text-2xs text-muted-foreground">
        操作编号{" "}
        <span className="block font-mono break-all">{v.operationId}</span>
      </p>
      {error && (
        <p role="alert" className="text-xs leading-6 text-destructive">
          {error}
        </p>
      )}
      {uncertain && (
        <Button
          type="button"
          variant="outline"
          onClick={() => void check()}
          disabled={pending}
        >
          查询此操作结果
        </Button>
      )}
    </form>
  )
}
function MappingPanel({
  actor,
  mappings,
  failed,
  pending,
  refresh,
}: {
  actor: string
  mappings: Mapping[] | undefined
  failed: boolean
  pending: boolean
  refresh: () => void
}) {
  const catalog = useQuery({
    queryKey: ["platform-role-catalog", actor],
    queryFn: async ({ signal }) =>
      catalogSchema.parse(await api("/platform-role-catalog", { signal })),
    retry: false,
  })
  const [group, setGroup] = useState(""),
    [roleCode, setRole] = useState(""),
    [clusterId, setCluster] = useState(""),
    [confirmation, setConfirmation] = useState(""),
    [error, setError] = useState(""),
    [writing, setWriting] = useState(false),
    [removing, setRemoving] = useState<Mapping | null>(null)
  const role = catalog.data?.roles.find((r) => r.code === roleCode)
  const submit = async () => {
    setWriting(true)
    setError("")
    try {
      if (removing)
        await api(`/directory/role-mappings/${removing.id}`, {
          method: "DELETE",
          body: { updatedAt: removing.updatedAt, confirmation },
        })
      else
        await api("/directory/role-mappings", {
          method: "POST",
          body: { group, roleCode, clusterId, confirmation },
        })
      setGroup("")
      setConfirmation("")
      setRemoving(null)
      refresh()
      await refreshAccess()
    } catch {
      setError("映射未确认，请刷新核对角色范围、组是否存在及授权变化。")
    } finally {
      setWriting(false)
    }
  }
  return (
    <section className="panel space-y-5 p-5">
      <div>
        <h2 className="text-lg font-semibold">组角色映射</h2>
        <p className="mt-2 text-xs leading-6 text-muted-foreground">
          只对显式关联的 FreeIPA
          平台账户生效，按直接成员实时核对用户与组身份。目录故障时拒绝授权；移除映射不会删除手工授权。
        </p>
      </div>
      {failed || catalog.isError ? (
        <p role="alert">映射或角色目录不可用。</p>
      ) : pending ? (
        <p role="status">正在读取映射…</p>
      ) : (
        <>
          <div className="space-y-3">
            {!mappings?.length && (
              <p className="text-xs text-muted-foreground">
                尚未配置组角色映射。
              </p>
            )}
            {mappings?.map((m) => (
              <div
                className="flex flex-wrap items-center gap-3 rounded-lg border p-3 text-xs"
                key={m.id}
              >
                <span className="font-mono">{m.group}</span>
                <span>→ {m.roleName}</span>
                <span className="text-muted-foreground">
                  {m.clusterId
                    ? catalog.data?.clusters.find((c) => c.id === m.clusterId)
                        ?.name || m.clusterId
                    : "平台范围"}
                </span>
                <Button
                  className="ml-auto"
                  variant="ghost"
                  onClick={() => {
                    setRemoving(m)
                    setConfirmation("")
                  }}
                >
                  移除映射
                </Button>
              </div>
            ))}
          </div>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              void submit()
            }}
            className="space-y-4 border-t pt-5"
          >
            <h3 className="text-sm font-medium">
              {removing
                ? `移除 ${removing.group} → ${removing.roleName}`
                : "添加映射"}
            </h3>
            <fieldset disabled={writing} className="space-y-4">
              {!removing && (
                <div className="grid gap-4 sm:grid-cols-3">
                  <div className="space-y-2">
                    <Label htmlFor="ipa-map-group">目录组名称</Label>
                    <Input
                      id="ipa-map-group"
                      value={group}
                      maxLength={63}
                      required
                      onChange={(e) => setGroup(e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="ipa-map-role">平台角色</Label>
                    <select
                      id="ipa-map-role"
                      className={`${selectClass} w-full`}
                      value={roleCode}
                      required
                      onChange={(e) => {
                        setRole(e.target.value)
                        setCluster("")
                      }}
                    >
                      <option value="">选择角色</option>
                      {catalog.data?.roles.map((r) => (
                        <option key={r.code} value={r.code}>
                          {r.name} · {r.scope === "cluster" ? "集群" : "平台"}
                        </option>
                      ))}
                    </select>
                  </div>
                  {role?.scope === "cluster" && (
                    <div className="space-y-2">
                      <Label htmlFor="ipa-map-cluster">授权集群</Label>
                      <select
                        id="ipa-map-cluster"
                        className={`${selectClass} w-full`}
                        required
                        value={clusterId}
                        onChange={(e) => setCluster(e.target.value)}
                      >
                        <option value="">选择集群</option>
                        {catalog.data?.clusters.map((c) => (
                          <option key={c.id} value={c.id}>
                            {c.name}
                          </option>
                        ))}
                      </select>
                    </div>
                  )}
                </div>
              )}
              <div className="space-y-2">
                <Label htmlFor="ipa-map-confirm">
                  输入 CHANGE ACCESS 确认授权变更
                </Label>
                <Input
                  id="ipa-map-confirm"
                  value={confirmation}
                  onChange={(e) => setConfirmation(e.target.value)}
                />
              </div>
              <div className="flex gap-2">
                <Button
                  disabled={
                    confirmation !== "CHANGE ACCESS" ||
                    (!removing &&
                      (!group ||
                        !roleCode ||
                        (role?.scope === "cluster" && !clusterId)))
                  }
                  type="submit"
                >
                  {writing
                    ? "正在保存…"
                    : removing
                      ? "确认移除映射"
                      : "添加组角色映射"}
                </Button>
                {removing && (
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => {
                      setRemoving(null)
                      setConfirmation("")
                    }}
                  >
                    取消移除
                  </Button>
                )}
              </div>
            </fieldset>
            {error && (
              <p role="alert" className="text-xs text-destructive">
                {error}
              </p>
            )}
          </form>
        </>
      )}
    </section>
  )
}
