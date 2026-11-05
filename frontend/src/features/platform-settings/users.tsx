import { useState } from "react"
import { useForm, Controller, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { z } from "zod"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select"
import { api } from "@/services/api"
import { refreshAccess } from "@/stores/auth"
import {
  usersSchema,
  platformUserSchema,
  catalogSchema,
  userRolesSchema,
  type PlatformUser,
} from "./schema"
import { Confirmation, errorMessage } from "./common"
const sourceLabels = {
  local: "平台密码",
  pam: "PAM",
  ldap: "LDAP",
  freeipa: "FreeIPA",
}
export function UserSettings({
  actor,
  canUsers,
  canRoles,
}: {
  actor: string
  canUsers: boolean
  canRoles: boolean
}) {
  const client = useQueryClient(),
    [page, setPage] = useState(1),
    [search, setSearch] = useState(""),
    [query, setQuery] = useState(""),
    [selected, setSelected] = useState<PlatformUser>(),
    [creating, setCreating] = useState(false)
  const users = useQuery({
    queryKey: ["platform-users", actor, page, query],
    queryFn: async ({ signal }) =>
      usersSchema.parse(
        await api(
          `/platform-users?page=${page}&perPage=20&q=${encodeURIComponent(query)}`,
          { signal }
        )
      ),
    enabled: canUsers,
    retry: false,
    refetchOnWindowFocus: false,
  })
  const catalog = useQuery({
    queryKey: ["platform-role-catalog", actor],
    queryFn: async ({ signal }) =>
      catalogSchema.parse(await api("/platform-role-catalog", { signal })),
    enabled: canRoles,
    retry: false,
    refetchOnWindowFocus: false,
  })
  const saved = async (u: PlatformUser) => {
    setSelected(u)
    void client.invalidateQueries({ queryKey: ["platform-users"] })
    void client.invalidateQueries({ queryKey: ["platform-user-roles"] })
    if (u.id === actor) await refreshAccess()
  }
  return (
    <div className="space-y-6">
      {canUsers && (
        <section className="panel space-y-5 p-5 sm:p-6">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-lg font-semibold">平台用户</h2>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">
                平台账户与 Linux / LDAP / Slurm
                身份分别管理。创建账户不自动授予角色。
              </p>
            </div>
            <Button onClick={() => setCreating((v) => !v)}>
              {creating ? "收起创建表单" : "创建平台用户"}
            </Button>
          </div>
          {creating && (
            <CreateUser
              onCreated={(u) => {
                setCreating(false)
                void saved(u)
              }}
            />
          )}
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              setPage(1)
              setQuery(search.trim())
            }}
          >
            <Input
              aria-label="搜索平台用户"
              placeholder="邮箱、显示名或系统用户名"
              value={search}
              maxLength={100}
              onChange={(e) => setSearch(e.target.value)}
            />
            <Button type="submit" variant="outline">
              查询用户
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={users.isFetching}
              onClick={() => void users.refetch()}
            >
              刷新用户
            </Button>
          </form>
          {users.isPending ? (
            <p role="status">正在读取用户…</p>
          ) : users.isError ? (
            <p role="alert">用户列表不可用或管理权限已变化。</p>
          ) : (
            <>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>用户 / 邮箱</TableHead>
                      <TableHead>认证来源</TableHead>
                      <TableHead>状态</TableHead>
                      <TableHead>操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {users.data?.items.map((u) => (
                      <TableRow key={u.id}>
                        <TableCell>
                          <p className="font-medium">{u.name || u.email}</p>
                          <p className="mt-1 text-xs text-muted-foreground">
                            {u.email}
                            {u.username ? ` · ${u.username}` : ""}
                          </p>
                        </TableCell>
                        <TableCell>{sourceLabels[u.authSource]}</TableCell>
                        <TableCell>
                          <Badge variant={u.active ? "outline" : "secondary"}>
                            {u.active ? "活动" : "已停用"}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => setSelected(u)}
                          >
                            管理 {u.name || u.email}
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                {users.data?.items.length === 0 && (
                  <p className="p-4 text-sm text-muted-foreground">
                    没有匹配的用户。
                  </p>
                )}
              </div>
              <div className="flex items-center justify-between gap-3 text-sm">
                <span>
                  共 {users.data?.totalItems} 位 · 第 {page} 页
                </span>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    disabled={page === 1 || users.isFetching}
                    onClick={() => {
                      setSelected(undefined)
                      setPage((p) => p - 1)
                    }}
                  >
                    上一页用户
                  </Button>
                  <Button
                    variant="outline"
                    disabled={
                      users.isFetching ||
                      page * 20 >= (users.data?.totalItems || 0)
                    }
                    onClick={() => {
                      setSelected(undefined)
                      setPage((p) => p + 1)
                    }}
                  >
                    下一页用户
                  </Button>
                </div>
              </div>
            </>
          )}
        </section>
      )}
      {canUsers && selected && !users.isError && (
        <div className="space-y-6" key={`${selected.id}:${selected.revision}`}>
          <UserEditor
            actor={actor}
            user={selected}
            onSaved={saved}
            onClose={() => setSelected(undefined)}
          />
          {canRoles && !catalog.isError && catalog.data && (
            <UserRoles
              actor={actor}
              user={selected}
              catalog={catalog.data}
              onSaved={saved}
            />
          )}
        </div>
      )}
      {canRoles && (
        <section className="panel space-y-4 p-5 sm:p-6">
          <h2 className="text-lg font-semibold">角色与权限目录</h2>
          <p className="text-sm leading-6 text-muted-foreground">
            角色定义在服务器维护；本页查看权限，并为用户授予或撤销已有角色。集群角色只适用于所选集群。
          </p>
          {catalog.isPending ? (
            <p role="status">正在读取角色目录…</p>
          ) : catalog.isError ? (
            <p role="alert">角色目录不可用或授权管理权限已变化。</p>
          ) : (
            catalog.data?.roles.map((r) => (
              <details className="rounded-md border p-4" key={r.code}>
                <summary className="cursor-pointer text-sm font-medium">
                  {r.name} · {r.scope === "platform" ? "平台范围" : "指定集群"}{" "}
                  · {r.permissions.length} 项权限
                </summary>
                <dl className="mt-4 grid gap-3 sm:grid-cols-2">
                  {r.permissions.map((p) => (
                    <div key={p.code}>
                      <dt className="text-sm">{p.name}</dt>
                      <dd className="mt-1 font-mono text-xs break-all text-muted-foreground">
                        {p.code}
                      </dd>
                    </div>
                  ))}
                </dl>
              </details>
            ))
          )}
        </section>
      )}
    </div>
  )
}
const createSchema = z
  .object({
    email: z.email(),
    name: z.string().trim().min(1).max(100),
    authSource: z.enum(["local", "pam", "ldap", "freeipa"]),
    username: z.string().trim().max(64),
    password: z.string().max(255),
  })
  .superRefine((v, c) => {
    if (
      v.authSource === "local" &&
      (v.password.length < 12 ||
        new TextEncoder().encode(v.password).length > 71)
    )
      c.addIssue({
        code: "custom",
        path: ["password"],
        message: "密码至少 12 个字符且不超过 71 字节。",
      })
    if (v.authSource !== "local" && !v.username)
      c.addIssue({
        code: "custom",
        path: ["username"],
        message: "请输入系统或目录用户名。",
      })
  })
function CreateUser({ onCreated }: { onCreated: (u: PlatformUser) => void }) {
  const [error, setError] = useState("")
  const form = useForm<z.infer<typeof createSchema>>({
    resolver: zodResolver(createSchema),
    defaultValues: {
      email: "",
      name: "",
      authSource: "local",
      username: "",
      password: "",
    },
  })
  const source = useWatch({ control: form.control, name: "authSource" })
  const save = form.handleSubmit(async (v) => {
    setError("")
    try {
      const result = platformUserSchema.parse(
        await api("/platform-users", {
          method: "POST",
          body: {
            ...v,
            username: v.authSource === "local" ? "" : v.username,
            password: v.authSource === "local" ? v.password : "",
          },
        })
      )
      toast.success("平台用户已创建，尚无角色")
      onCreated(result)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      form.resetField("password")
    }
  })
  return (
    <form
      onSubmit={save}
      noValidate
      className="space-y-4 rounded-lg border bg-muted/20 p-4"
    >
      <h3 className="font-semibold">新建平台账户</h3>
      <fieldset
        className="grid gap-4 sm:grid-cols-2"
        disabled={form.formState.isSubmitting}
      >
        {(
          [
            ["email", "平台邮箱"],
            ["name", "显示名称"],
          ] as const
        ).map(([key, label]) => (
          <div className="space-y-2" key={key}>
            <Label htmlFor={`create-${key}`}>{label}</Label>
            <Input id={`create-${key}`} {...form.register(key)} />
          </div>
        ))}
        <div className="space-y-2">
          <Label htmlFor="create-source">认证来源</Label>
          <Controller
            control={form.control}
            name="authSource"
            render={({ field }) => (
              <Select
                value={field.value}
                onValueChange={(v) => {
                  field.onChange(v)
                  form.setValue("password", "")
                  form.setValue("username", "")
                }}
              >
                <SelectTrigger id="create-source">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="local">平台密码</SelectItem>
                  <SelectItem value="pam">PAM 系统账户</SelectItem>
                  <SelectItem value="ldap">LDAP 目录账户</SelectItem>
                  <SelectItem value="freeipa">FreeIPA 目录账户</SelectItem>
                </SelectContent>
              </Select>
            )}
          />
        </div>
        {source === "local" ? (
          <div className="space-y-2">
            <Label htmlFor="create-password">初始平台密码</Label>
            <Input
              id="create-password"
              type="password"
              autoComplete="new-password"
              {...form.register("password")}
            />
            <p className="text-xs text-muted-foreground">
              至少 12 个字符，不超过 71 字节。
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            <Label htmlFor="create-username">系统 / 目录用户名</Label>
            <Input
              id="create-username"
              autoComplete="off"
              {...form.register("username")}
            />
            <p className="text-xs leading-6 text-muted-foreground">
              只关联已存在且符合准入条件的身份，不接收其密码；PAM 不允许 root。
            </p>
          </div>
        )}
      </fieldset>
      {Object.values(form.formState.errors).map((e, i) => (
        <p role="alert" key={i} className="text-sm text-destructive">
          {e.message || "请检查用户输入。"}
        </p>
      ))}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button disabled={form.formState.isSubmitting} type="submit">
        {form.formState.isSubmitting ? "正在创建…" : "保存新用户"}
      </Button>
    </form>
  )
}
function UserEditor({
  actor,
  user,
  onSaved,
  onClose,
}: {
  actor: string
  user: PlatformUser
  onSaved: (u: PlatformUser) => Promise<void>
  onClose: () => void
}) {
  const [name, setName] = useState(user.name),
    [active, setActive] = useState(user.active),
    [review, setReview] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("")
  const save = async () => {
    setBusy(true)
    setError("")
    try {
      const saved = platformUserSchema.parse(
        await api(`/platform-users/${user.id}`, {
          method: "PATCH",
          body: {
            name: name.trim(),
            active,
            revision: user.revision,
            confirmation: "UPDATE USER",
          },
        })
      )
      setReview(false)
      await onSaved(saved)
      toast.success("平台用户已更新")
    } catch (e) {
      setReview(false)
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <section className="panel space-y-5 p-5 sm:p-6">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">
            管理用户：{user.name || user.email}
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            {user.email} · {sourceLabels[user.authSource]} · 修订{" "}
            {user.revision}
          </p>
        </div>
        <Button variant="ghost" disabled={busy} onClick={onClose}>
          关闭用户管理
        </Button>
      </div>
      <div className="max-w-xl space-y-4">
        <div className="space-y-2">
          <Label htmlFor="edit-user-name">用户显示名称</Label>
          <Input
            id="edit-user-name"
            value={name}
            maxLength={100}
            onChange={(e) => setName(e.target.value)}
            disabled={busy}
          />
        </div>
        <label className="flex items-center gap-3 text-sm">
          <Checkbox
            checked={active}
            onCheckedChange={(v) => setActive(v === true)}
            disabled={busy || user.id === actor}
          />
          允许此用户登录平台
          {user.id === actor ? "（当前账户不能自行停用）" : ""}
        </label>
        <p className="text-sm leading-6 text-muted-foreground">
          停用会撤销既有平台会话；再次启用后需重新登录。外部账户还需通过 PAM /
          LDAP 准入检查。最后一个活动 Super Admin 不能被停用。
        </p>
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button
        disabled={
          busy ||
          name.trim().length === 0 ||
          (name === user.name && active === user.active)
        }
        onClick={() => setReview(true)}
      >
        核对用户修改
      </Button>
      {review && (
        <Confirmation
          title="确认用户修改"
          phrase="UPDATE USER"
          busy={busy}
          onCancel={() => setReview(false)}
          onConfirm={() => void save()}
        >
          <p>
            {user.email}
            <br />
            显示名称：{user.name} → {name.trim()}
            <br />
            允许登录：{user.active ? "是" : "否"} → {active ? "是" : "否"}
          </p>
          <p>认证来源、系统身份及 Slurm 账户保持原样。</p>
        </Confirmation>
      )}
    </section>
  )
}
function UserRoles({
  actor,
  user,
  catalog,
  onSaved,
}: {
  actor: string
  user: PlatformUser
  catalog: z.infer<typeof catalogSchema>
  onSaved: (u: PlatformUser) => Promise<void>
}) {
  const [roleCode, setRole] = useState(""),
    [clusterId, setCluster] = useState(""),
    [review, setReview] = useState<{
      roleCode: string
      clusterId: string
      remove: boolean
    }>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("")
  const grants = useQuery({
    queryKey: ["platform-user-roles", actor, user.id, user.revision],
    queryFn: async ({ signal }) =>
      userRolesSchema.parse(
        await api(`/platform-users/${user.id}/roles`, { signal })
      ),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const role = catalog.roles.find((r) => r.code === roleCode)
  const target = review
    ? catalog.roles.find((r) => r.code === review.roleCode)
    : undefined
  const apply = async () => {
    if (!review || !grants.data) return
    setBusy(true)
    setError("")
    try {
      const saved = platformUserSchema.parse(
        await api(
          `/platform-users/${user.id}/roles${review.remove ? "/remove" : ""}`,
          {
            method: "POST",
            body: {
              roleCode: review.roleCode,
              clusterId: review.clusterId,
              revision: grants.data.user.revision,
              confirmation: "CHANGE ACCESS",
            },
          }
        )
      )
      setReview(undefined)
      await onSaved(saved)
      toast.success("角色授权已更新")
    } catch (e) {
      setReview(undefined)
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <section className="panel space-y-5 p-5 sm:p-6">
      <div>
        <h2 className="text-lg font-semibold">{user.email} 的角色授权</h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          平台角色适用于整个平台；集群角色仅在指定集群生效。授权变更不自动修改
          Slurm 原生权限。
        </p>
      </div>
      {grants.isPending ? (
        <p role="status">正在读取用户授权…</p>
      ) : grants.isError ? (
        <p role="alert">用户授权不可用或权限已变化，请刷新用户列表。</p>
      ) : (
        <>
          <div className="space-y-3">
            {grants.data?.roles.length === 0 && (
              <p className="text-sm text-muted-foreground">此用户尚无角色。</p>
            )}
            {grants.data?.roles.map((g) => (
              <div
                key={`${g.roleCode}:${g.clusterId}`}
                className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-4"
              >
                <div>
                  <p className="text-sm font-medium">{g.roleName}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {g.scope === "platform"
                      ? "整个平台"
                      : `${g.clusterName} · ${g.clusterId}`}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onClick={() =>
                    setReview({
                      roleCode: g.roleCode,
                      clusterId: g.clusterId,
                      remove: true,
                    })
                  }
                >
                  撤销 {g.roleName}
                </Button>
              </div>
            ))}
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="grant-role">授予角色</Label>
              <Select
                value={roleCode}
                onValueChange={(v) => {
                  setRole(v)
                  setCluster("")
                }}
                disabled={busy}
              >
                <SelectTrigger id="grant-role">
                  <SelectValue placeholder="选择角色" />
                </SelectTrigger>
                <SelectContent>
                  {catalog.roles.map((r) => (
                    <SelectItem key={r.code} value={r.code}>
                      {r.name} · {r.scope === "platform" ? "平台" : "集群"}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {role?.scope === "cluster" && (
              <div className="space-y-2">
                <Label htmlFor="grant-cluster">授权集群</Label>
                <Select
                  value={clusterId}
                  onValueChange={setCluster}
                  disabled={busy}
                >
                  <SelectTrigger id="grant-cluster">
                    <SelectValue placeholder="选择集群" />
                  </SelectTrigger>
                  <SelectContent>
                    {catalog.clusters.map((c) => (
                      <SelectItem value={c.id} key={c.id}>
                        {c.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
          </div>
          <Button
            variant="outline"
            disabled={busy || !role || (role.scope === "cluster" && !clusterId)}
            onClick={() =>
              setReview({
                roleCode,
                clusterId: role?.scope === "platform" ? "" : clusterId,
                remove: false,
              })
            }
          >
            核对授予角色
          </Button>
        </>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {review && (
        <Confirmation
          title={review.remove ? "确认撤销角色" : "确认授予角色"}
          phrase="CHANGE ACCESS"
          busy={busy}
          onCancel={() => setReview(undefined)}
          onConfirm={() => void apply()}
        >
          <p>
            用户：{user.email}
            <br />
            角色：{target?.name || review.roleCode}
            <br />
            范围：
            {review.clusterId
              ? `${catalog.clusters.find((c) => c.id === review.clusterId)?.name || ""} · ${review.clusterId}`
              : "整个平台"}
          </p>
          {review.roleCode === "super_admin" && (
            <p>
              Super Admin 可访问所有集群、管理平台配置和用户授权。最后一个活动
              Super Admin 不能被撤权。
            </p>
          )}
          <p>变更立即生效，操作与范围将记录在审计中。</p>
        </Confirmation>
      )}
    </section>
  )
}
