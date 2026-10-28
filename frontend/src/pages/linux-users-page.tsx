import { useMemo, useState } from "react"
import { useSearchParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { ClientResponseError } from "pocketbase"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import {
  ArrowDown,
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  RefreshCw,
  Search,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  ClusterPicker,
  pageNumber,
  useAccessibleClusters,
} from "@/features/clusters/cluster-picker"
import { LinuxUserManagement } from "@/features/linux-users/management"
import { LinuxUserDetail } from "@/features/linux-users/user-detail"
import {
  errorMessage,
  kinds,
  linuxUserPageSchema,
  sources,
  type LinuxUser,
} from "@/features/linux-users/schema"
import { timestamp } from "@/features/overview/schema"
import { api } from "@/services/api"
const EMPTY: LinuxUser[] = []
const filterSchema = z.object({
  q: z.string().trim().max(128, "最多 128 个字符"),
  source: z.string(),
  kind: z.string(),
})
type Filters = z.infer<typeof filterSchema>
function FiltersForm({
  initial,
  onApply,
}: {
  initial: Filters
  onApply: (values: Filters) => void
}) {
  const {
    register,
    control,
    setValue,
    reset,
    handleSubmit,
    formState: { errors },
  } = useForm<Filters>({
    resolver: zodResolver(filterSchema),
    defaultValues: initial,
  })
  const source = useWatch({ control, name: "source" }),
    kind = useWatch({ control, name: "kind" })
  return (
    <form
      className="panel flex flex-wrap items-end gap-3 p-4"
      noValidate
      onSubmit={handleSubmit(onApply)}
    >
      <div className="min-w-48 flex-1 space-y-2">
        <Label htmlFor="linux-users-q">搜索枚举结果</Label>
        <Input
          id="linux-users-q"
          placeholder="用户名、描述或主目录"
          {...register("q")}
          aria-invalid={!!errors.q}
        />
        {errors.q && (
          <p role="alert" className="text-xs text-destructive">
            {errors.q.message}
          </p>
        )}
      </div>
      <div className="w-44 space-y-2">
        <Label htmlFor="linux-users-source">身份来源</Label>
        <Select
          value={source || "all"}
          onValueChange={(v) => setValue("source", v === "all" ? "" : v)}
        >
          <SelectTrigger id="linux-users-source" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部来源</SelectItem>
            {Object.entries(sources).map(([v, label]) => (
              <SelectItem key={v} value={v}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="w-36 space-y-2">
        <Label htmlFor="linux-users-kind">UID 范围</Label>
        <Select
          value={kind || "all"}
          onValueChange={(v) => setValue("kind", v === "all" ? "" : v)}
        >
          <SelectTrigger id="linux-users-kind" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部范围</SelectItem>
            {Object.entries(kinds).map(([v, label]) => (
              <SelectItem key={v} value={v}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <Button
        variant="ghost"
        type="button"
        onClick={() => {
          const empty = { q: "", source: "", kind: "" }
          reset(empty)
          onApply(empty)
        }}
      >
        清除筛选
      </Button>
      <Button type="submit">
        <Search />
        查询用户
      </Button>
    </form>
  )
}
const lookupSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "请输入用户名")
    .max(255, "用户名过长")
    .regex(/^[\p{L}\p{N}_][\p{L}\p{N}_.@+\\$-]{0,254}$/u, "请输入合法用户名")
    .refine(
      (value) => !/^[0-9]+$/.test(value),
      "请输入用户名，不能使用纯数字 UID"
    )
    .refine(
      (value) => new TextEncoder().encode(value).byteLength <= 255,
      "用户名最多 255 字节"
    ),
})
function ExactLookup({ onLookup }: { onLookup: (name: string) => void }) {
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<z.infer<typeof lookupSchema>>({
    resolver: zodResolver(lookupSchema),
    defaultValues: { name: "" },
  })
  return (
    <form
      className="panel flex flex-wrap items-end gap-3 p-4"
      noValidate
      onSubmit={handleSubmit((v) => onLookup(v.name))}
    >
      <div className="w-full space-y-2 sm:w-72">
        <Label htmlFor="linux-user-lookup">精确查询用户名</Label>
        <Input
          id="linux-user-lookup"
          placeholder="例如 consoleadmin 或 user@domain"
          autoComplete="off"
          {...register("name")}
          aria-invalid={!!errors.name}
        />
        {errors.name && (
          <p role="alert" className="text-xs text-destructive">
            {errors.name.message}
          </p>
        )}
      </div>
      <Button type="submit" variant="outline">
        查询用户详情
      </Button>
      <p className="text-2xs leading-5 text-muted-foreground">
        目录服务可能关闭枚举；列表中未出现的用户仍可按完整用户名查询。
      </p>
    </form>
  )
}
export function LinuxUsersPage() {
  const [params, setParams] = useSearchParams(),
    selection = useAccessibleClusters(),
    clusterId = selection.id,
    [automatic, setAutomatic] = useState(true)
  const page = pageNumber(params.get("page")),
    perPage = [20, 50, 100].includes(Number(params.get("perPage")))
      ? Number(params.get("perPage"))
      : 20
  const filters = {
      q: params.get("q") || "",
      source: params.get("source") || "",
      kind: params.get("kind") || "",
    },
    sort = params.get("sort") || "name",
    order = params.get("order") || "asc",
    name = params.get("username") || ""
  const list = new URLSearchParams({
    page: String(page),
    perPage: String(perPage),
    sort,
    order,
  })
  for (const [k, v] of Object.entries(filters)) if (v) list.set(k, v)
  const query = useQuery({
    queryKey: ["linux-users", clusterId, list.toString()],
    enabled: !!clusterId,
    queryFn: async ({ signal }) =>
      linuxUserPageSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/linux-users?${list}`,
          { signal }
        )
      ),
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: automatic,
    refetchInterval: (q) =>
      automatic &&
      !(
        q.state.error instanceof ClientResponseError &&
        [403, 404].includes(q.state.error.status)
      )
        ? 30000
        : false,
  })
  const data = query.isError ? undefined : query.data,
    rows = data?.items ?? EMPTY
  function update(changes: Record<string, string>) {
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      for (const [k, v] of Object.entries(changes)) {
        if (v) next.set(k, v)
        else next.delete(k)
      }
      return next
    })
  }
  const columns = useMemo<ColumnDef<LinuxUser>[]>(
    () => [
      {
        accessorKey: "name",
        header: "用户名",
        cell: ({ row }) => (
          <span className="font-mono font-medium break-all">
            {row.original.name}
          </span>
        ),
      },
      {
        accessorKey: "uid",
        header: "UID",
        cell: ({ row }) => (
          <span className="font-mono">{row.original.uid}</span>
        ),
      },
      {
        accessorKey: "gid",
        header: "主组 GID",
        cell: ({ row }) => (
          <span className="font-mono">{row.original.gid}</span>
        ),
      },
      {
        accessorKey: "source",
        header: "身份来源",
        cell: ({ row }) => (
          <Badge variant="outline">{sources[row.original.source]}</Badge>
        ),
      },
      {
        accessorKey: "kind",
        header: "UID 范围",
        cell: ({ row }) => (
          <Badge variant="outline">{kinds[row.original.kind]}</Badge>
        ),
      },
      {
        accessorKey: "comment",
        header: "描述",
        cell: ({ row }) => (
          <span
            className="block max-w-56 truncate"
            title={row.original.comment}
          >
            {row.original.comment || "—"}
          </span>
        ),
      },
    ],
    []
  )
  // TanStack Table exposes mutable methods that must not be compiler memoized.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualSorting: true,
    getRowId: (row) => row.name,
  })
  return (
    <div className="mx-auto w-full max-w-[1600px] space-y-5 p-5 lg:p-8">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="section-eyebrow">LINUX IDENTITIES</p>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight">
            Linux 系统用户
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {selection.current?.name || "选择集群以查询 Linux 身份"}
          </p>
        </div>
        <div className="flex gap-2">
          <Select
            value={automatic ? "30" : "off"}
            onValueChange={(v) => setAutomatic(v === "30")}
          >
            <SelectTrigger aria-label="Linux 用户自动刷新" className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="30">每 30 秒刷新</SelectItem>
              <SelectItem value="off">暂停自动刷新</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            disabled={query.isFetching || selection.inventory.isFetching}
            onClick={() => {
              void selection.inventory.refetch()
              if (clusterId) void query.refetch()
            }}
          >
            <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
            刷新用户
          </Button>
        </div>
      </header>
      <ClusterPicker selection={selection} label="Linux 用户" />
      {selection.inventory.isError && (
        <p role="alert" className="panel p-4 text-sm text-destructive">
          集群候选列表读取失败，请刷新后重试。
        </p>
      )}
      {!clusterId &&
        !selection.inventory.isPending &&
        !selection.inventory.isError && (
          <p className="panel p-6 text-sm text-muted-foreground">
            暂无可访问集群，请联系管理员检查登记与授权。
          </p>
        )}
      {clusterId && (
        <>
          <LinuxUserManagement
            key={clusterId}
            clusterId={clusterId}
            operationId={params.get("operation") || ""}
            onOperation={(operation) => update({ operation })}
          />
          <ExactLookup
            key={clusterId}
            onLookup={(username) => update({ username })}
          />
          <FiltersForm
            key={JSON.stringify(filters)}
            initial={filters}
            onApply={(v) => update({ ...v, page: "1" })}
          />
          {query.isPending && (
            <p
              role="status"
              className="panel p-6 text-sm text-muted-foreground"
            >
              正在读取 Linux 用户…
            </p>
          )}
          {query.isError && (
            <p role="alert" className="panel p-5 text-sm text-destructive">
              {errorMessage(query.error)}
            </p>
          )}
          {data && (
            <section
              className="panel overflow-hidden"
              aria-label="Linux 用户列表"
            >
              <div className="flex flex-wrap items-center justify-between gap-3 border-b p-4">
                <div>
                  <h2 className="text-sm font-semibold">
                    当前枚举结果 · {data.totalItems}
                  </h2>
                  <p className="mt-1 text-2xs text-muted-foreground">
                    {data.sourceLabel} · {timestamp(data.fetchedAt)} · UID_MIN{" "}
                    {data.uidMin ?? "未知"}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <Select
                    value={sort}
                    onValueChange={(v) => update({ sort: v, page: "1" })}
                  >
                    <SelectTrigger aria-label="Linux 用户排序" className="w-32">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="name">按用户名</SelectItem>
                      <SelectItem value="uid">按 UID</SelectItem>
                      <SelectItem value="gid">按 GID</SelectItem>
                    </SelectContent>
                  </Select>
                  <Button
                    size="icon"
                    variant="outline"
                    aria-label="切换 Linux 用户排序方向"
                    onClick={() =>
                      update({
                        order: order === "asc" ? "desc" : "asc",
                        page: "1",
                      })
                    }
                  >
                    {order === "asc" ? <ArrowUp /> : <ArrowDown />}
                  </Button>
                </div>
              </div>
              <Table>
                <TableHeader>
                  {table.getHeaderGroups().map((group) => (
                    <TableRow key={group.id}>
                      {group.headers.map((header) => (
                        <TableHead key={header.id}>
                          {flexRender(
                            header.column.columnDef.header,
                            header.getContext()
                          )}
                        </TableHead>
                      ))}
                      <TableHead>操作</TableHead>
                    </TableRow>
                  ))}
                </TableHeader>
                <TableBody>
                  {table.getRowModel().rows.map((row) => (
                    <TableRow key={row.id}>
                      {row.getVisibleCells().map((cell) => (
                        <TableCell key={cell.id}>
                          {flexRender(
                            cell.column.columnDef.cell,
                            cell.getContext()
                          )}
                        </TableCell>
                      ))}
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label={`查看 Linux 用户 ${row.original.name}`}
                          onClick={() =>
                            update({ username: row.original.name })
                          }
                        >
                          详情
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {!rows.length && (
                <div className="space-y-2 py-12 text-center">
                  <h3 className="text-sm font-medium">
                    本页没有匹配的枚举记录
                  </h3>
                  <p className="text-xs text-muted-foreground">
                    可调整筛选，或使用精确用户名查询目录账户。
                  </p>
                  {page > 1 && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => update({ page: "1" })}
                    >
                      返回第一页
                    </Button>
                  )}
                </div>
              )}
              <div className="flex flex-wrap items-center justify-between gap-3 border-t p-4">
                <p className="text-2xs text-muted-foreground">
                  共 {data.totalItems} 条匹配枚举记录 · 第 {page} 页
                </p>
                <div className="flex gap-2">
                  <Select
                    value={String(perPage)}
                    onValueChange={(v) => update({ perPage: v, page: "1" })}
                  >
                    <SelectTrigger
                      aria-label="每页 Linux 用户数"
                      className="w-36"
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {[20, 50, 100].map((n) => (
                        <SelectItem value={String(n)} key={n}>
                          {n} 条 / 页
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button
                    size="icon"
                    variant="outline"
                    aria-label="上一页 Linux 用户"
                    disabled={page <= 1 || query.isFetching}
                    onClick={() => update({ page: String(page - 1) })}
                  >
                    <ChevronLeft />
                  </Button>
                  <Button
                    size="icon"
                    variant="outline"
                    aria-label="下一页 Linux 用户"
                    disabled={
                      page * perPage >= data.totalItems || query.isFetching
                    }
                    onClick={() => update({ page: String(page + 1) })}
                  >
                    <ChevronRight />
                  </Button>
                </div>
              </div>
            </section>
          )}
          <LinuxUserDetail
            key={clusterId + ":" + name}
            clusterId={clusterId}
            name={name}
            operationId={params.get("operation") || ""}
            onOperation={(operation) => update({ operation })}
            onClose={() => update({ username: "" })}
          />
        </>
      )}
      <p className="rounded-md border p-4 text-2xs leading-6 text-muted-foreground">
        列表展示 NSS 本次可枚举的身份；目录未开启枚举时不会包含全部账户。UID
        范围依据来源主机的 UID_MIN 分类，不能据此判断登录资格。Linux
        身份、平台账户及 Slurm Association 分别管理。
      </p>
    </div>
  )
}
