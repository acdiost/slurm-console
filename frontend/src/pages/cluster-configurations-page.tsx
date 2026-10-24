import { useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { ClientResponseError } from "pocketbase"
import {
  Cable,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  LoaderCircle,
  Pencil,
  Plus,
  Power,
  RefreshCw,
  Server,
  ShieldOff,
} from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
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
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { ConnectionForm } from "@/features/clusters/connection-form"
import {
  configurationPageSchema,
  configurationSchema,
  connectionStatus,
  profileSchema,
  type ClusterConfiguration,
  type ConnectionValues,
} from "@/features/clusters/schema"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { z } from "zod"

const EMPTY: ClusterConfiguration[] = []
function errorMessage(error: unknown) {
  if (error instanceof ClientResponseError) {
    if (error.status === 403) return "你没有管理集群配置的权限。"
    return error.response?.error?.message || "操作未完成，请刷新后重试。"
  }
  return "无法连接平台服务，请稍后重试。"
}

export function ClusterConfigurationsPage() {
  const canManage = useAuth(
    (state) =>
      state.user?.access.platformPermissions.includes(
        "platform.clusters.manage"
      ) ?? false
  )
  const [page, setPage] = useState(1)
  const [editor, setEditor] = useState<
    ClusterConfiguration | null | undefined
  >()
  const [disabling, setDisabling] = useState<ClusterConfiguration | null>(null)
  const client = useQueryClient()
  const configs = useQuery({
    queryKey: ["cluster-configurations", page],
    enabled: canManage,
    retry: false,
    queryFn: async ({ signal }) =>
      configurationPageSchema.parse(
        await api(`/cluster-configurations?page=${page}&perPage=20`, { signal })
      ),
  })
  const profiles = useQuery({
    queryKey: ["cluster-connection-profiles"],
    enabled: canManage,
    retry: false,
    queryFn: async ({ signal }) =>
      z
        .array(profileSchema)
        .parse(await api("/cluster-connection-profiles", { signal })),
  })
  const refresh = async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ["cluster-configurations"] }),
      client.invalidateQueries({ queryKey: ["cluster-inventory"] }),
    ])
  }
  const save = useMutation({
    mutationFn: ({ id, values }: { id?: string; values: ConnectionValues }) =>
      api(`/cluster-configurations${id ? `/${id}` : ""}`, {
        method: id ? "PATCH" : "POST",
        body: values,
      }),
    onSuccess: async () => {
      setEditor(undefined)
      await refresh()
      toast.success("集群配置已保存")
    },
  })
  const test = useMutation({
    mutationFn: (config: ClusterConfiguration) =>
      api(`/cluster-configurations/${config.id}/test`, {
        method: "POST",
        body: { revision: config.revision },
      }).then((value) => configurationSchema.parse(value)),
    onSuccess: async (config) => {
      await refresh()
      if (config.lastProbe.status === "connected")
        toast.success(config.lastProbe.message)
      else toast.error(config.lastProbe.message)
    },
    onError: (error) => {
      void refresh()
      toast.error(errorMessage(error))
    },
  })
  const disable = useMutation({
    mutationFn: (config: ClusterConfiguration) =>
      api(`/cluster-configurations/${config.id}/disable`, {
        method: "POST",
        body: { revision: config.revision },
      }),
    onSuccess: async () => {
      setDisabling(null)
      await refresh()
      toast.success("平台连接已停用")
    },
    onError: (error) => {
      void refresh()
      toast.error(errorMessage(error))
    },
  })
  const rows = configs.data?.items ?? EMPTY
  const columns = useMemo<ColumnDef<ClusterConfiguration>[]>(
    () => [
      {
        accessorKey: "name",
        header: "集群",
        cell: ({ row: { original: config } }) => (
          <div className="min-w-36">
            <p className="font-medium">{config.name}</p>
            <p className="mt-1 font-mono text-2xs text-muted-foreground">
              {config.slug}
            </p>
            {config.description && (
              <p
                className="mt-1 max-w-40 truncate text-2xs text-muted-foreground xl:max-w-64"
                title={config.description}
              >
                {config.description}
              </p>
            )}
            <p className="mt-1 font-mono text-2xs text-muted-foreground lg:hidden">
              {config.apiVersion || "未配置 API 版本"}
            </p>
          </div>
        ),
      },
      {
        id: "connection",
        header: "连接与版本",
        cell: ({ row: { original: config } }) => (
          <div className="max-w-72">
            <p
              className="truncate font-mono text-2xs text-muted-foreground"
              title={config.endpoint}
            >
              {config.endpoint || "尚未配置连接"}
            </p>
            <p className="mt-1.5 text-2xs">
              {config.apiVersion || "—"}
              {config.authType &&
                ` · ${config.authType === "local" ? "本地认证" : "JWT / HTTPS"}`}
            </p>
          </div>
        ),
      },
      {
        id: "status",
        header: "检测状态",
        cell: ({ row: { original: config } }) => {
          const state = connectionStatus(config)
          return (
            <div className="min-w-32">
              <Badge
                variant="outline"
                className="status-badge gap-1.5 font-normal"
                data-tone={state.tone}
              >
                <span className="size-1.5 rounded-full bg-current" />
                {state.label}
              </Badge>
              <p className="mt-2 max-w-60 text-2xs leading-5 text-muted-foreground">
                {config.lastProbe.message}
              </p>
              {config.lastProbe.serverVersion && (
                <p className="mt-1 font-mono text-2xs text-muted-foreground">
                  RESTD {config.lastProbe.serverVersion}
                </p>
              )}
              {config.lastProbe.controllers.map((controller) => (
                <p
                  key={`${controller.hostname}-${controller.role}`}
                  className="mt-1 font-mono text-2xs text-muted-foreground"
                >
                  {controller.hostname} · {controller.role} · {controller.state}
                </p>
              ))}
            </div>
          )
        },
      },
      {
        id: "checked",
        header: "上次检测",
        cell: ({ row: { original: config } }) => (
          <div className="min-w-32 text-2xs text-muted-foreground">
            {config.lastProbe.checkedAt ? (
              <>
                <p>
                  {new Date(config.lastProbe.checkedAt).toLocaleString("zh-CN")}
                </p>
                <p className="mt-1 font-mono">
                  {config.lastProbe.durationMs} ms · 超时{" "}
                  {config.timeoutSeconds}s
                </p>
              </>
            ) : (
              "尚未检测"
            )}
          </div>
        ),
      },
    ],
    []
  )
  // TanStack Table manages mutable state outside the React compiler.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    getRowId: (row) => row.id,
  })
  if (!canManage)
    return (
      <div className="mx-auto max-w-4xl p-8">
        <div className="panel flex items-start gap-3 p-6">
          <ShieldOff className="size-5 shrink-0 text-muted-foreground" />
          <div>
            <h1 className="text-lg font-semibold">暂无集群配置权限</h1>
            <p className="mt-2 text-sm text-muted-foreground">
              请联系平台管理员授予集群配置管理权限。
            </p>
          </div>
        </div>
      </div>
    )
  return (
    <div className="mx-auto max-w-7xl space-y-6 p-6 sm:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="font-mono text-micro tracking-widest text-muted-foreground">
            CLUSTER CONNECTIONS
          </p>
          <h1 className="mt-2 text-2xl font-semibold">集群连接配置</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            管理连接方式、API 版本与启停状态，检测 Slurm 控制器连通性。
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            disabled={configs.isFetching || profiles.isFetching}
            onClick={() => {
              void refresh()
              void profiles.refetch()
            }}
          >
            <RefreshCw className={configs.isFetching ? "animate-spin" : ""} />
            刷新
          </Button>
          <Button
            disabled={!profiles.data?.length}
            onClick={() => {
              save.reset()
              setEditor(null)
            }}
          >
            <Plus />
            新建集群
          </Button>
        </div>
      </header>
      <div className="grid gap-3 sm:grid-cols-3">
        {[
          {
            label: "已登记集群",
            value: configs.data?.totalItems ?? "—",
            icon: Server,
          },
          {
            label: "本页启用连接",
            value: rows.filter((config) => config.enabled).length,
            icon: Cable,
          },
          {
            label: "本页最近检测成功",
            value: rows.filter(
              (config) =>
                config.enabled && config.lastProbe.status === "connected"
            ).length,
            icon: CheckCircle2,
          },
        ].map(({ label, value, icon: Icon }) => (
          <div
            key={label}
            className="panel flex items-center justify-between p-5"
          >
            <div>
              <p className="text-2xs text-muted-foreground">{label}</p>
              <p className="mt-2 font-mono text-2xl font-semibold">{value}</p>
            </div>
            <Icon className="size-5 text-muted-foreground" />
          </div>
        ))}
      </div>
      {profiles.data?.length === 0 && (
        <div
          role="status"
          className="rounded-lg border border-dashed p-5 text-sm text-muted-foreground"
        >
          暂未提供可用连接方式，请联系平台管理员完成服务端连接设置。
        </div>
      )}
      {(configs.isError || profiles.isError) && (
        <div role="alert" className="panel p-5 text-sm text-destructive">
          {errorMessage(configs.error || profiles.error)}
        </div>
      )}
      {configs.isPending ? (
        <div aria-label="正在加载集群配置" role="status" className="space-y-3">
          <Skeleton className="h-16" />
          <Skeleton className="h-36" />
        </div>
      ) : (
        <section className="panel overflow-hidden">
          <div className="panel-heading">
            <h2>连接列表</h2>
            <Badge variant="outline">服务端分页</Badge>
          </div>
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id}>
                  {group.headers.map((header) => (
                    <TableHead
                      key={header.id}
                      className={
                        header.id === "connection" || header.id === "checked"
                          ? "hidden lg:table-cell"
                          : undefined
                      }
                    >
                      {flexRender(
                        header.column.columnDef.header,
                        header.getContext()
                      )}
                    </TableHead>
                  ))}
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows.map((row) => (
                <TableRow key={row.id}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className={`py-4 align-top ${cell.column.id === "connection" || cell.column.id === "checked" ? "hidden lg:table-cell" : ""}`}
                    >
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext()
                      )}
                    </TableCell>
                  ))}
                  <TableCell className="py-4 align-top">
                    <div className="flex justify-end gap-1">
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={
                          !row.original.enabled ||
                          !row.original.profileAvailable ||
                          test.isPending
                        }
                        onClick={() => test.mutate(row.original)}
                      >
                        {test.isPending && test.variables?.id === row.id ? (
                          <LoaderCircle className="animate-spin" />
                        ) : (
                          <Cable />
                        )}
                        检测
                      </Button>
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={`编辑 ${row.original.name}`}
                        onClick={() => {
                          save.reset()
                          setEditor(row.original)
                        }}
                      >
                        <Pencil />
                      </Button>
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={`停用 ${row.original.name}`}
                        disabled={!row.original.enabled || disable.isPending}
                        onClick={() => setDisabling(row.original)}
                      >
                        <Power />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!rows.length && !configs.isError && (
            <div className="flex flex-col items-center gap-3 p-12 text-center">
              <Server className="size-7 text-muted-foreground" />
              <p className="text-sm font-medium">还没有登记集群</p>
              <p className="text-xs text-muted-foreground">
                新建集群并选择连接方式后，即可进行连接检测。
              </p>
            </div>
          )}
          <div className="flex items-center justify-between border-t p-4 text-2xs text-muted-foreground">
            <span>
              共 {configs.data?.totalItems ?? 0} 个集群 · 第 {page} 页
            </span>
            <div className="flex gap-2">
              <Button
                size="icon"
                variant="outline"
                aria-label="上一页"
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
              >
                <ChevronLeft />
              </Button>
              <Button
                size="icon"
                variant="outline"
                aria-label="下一页"
                disabled={
                  !configs.data ||
                  page * configs.data.perPage >= configs.data.totalItems
                }
                onClick={() => setPage(page + 1)}
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </section>
      )}
      <p className="text-2xs leading-6 text-muted-foreground">
        检测结果反映上次请求时的状态。检测仅查询 REST
        服务与控制器，作业和节点管理将在后续阶段接入。
      </p>
      <Sheet
        open={editor !== undefined}
        onOpenChange={(open) => {
          if (!open && !save.isPending) setEditor(undefined)
        }}
      >
        <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
          <SheetHeader>
            <SheetTitle>{editor ? "编辑集群连接" : "新建集群"}</SheetTitle>
            <SheetDescription>
              选择连接方式并设置 REST API 参数。
            </SheetDescription>
          </SheetHeader>
          {editor !== undefined && (
            <ConnectionForm
              key={editor?.id ?? "new"}
              config={editor}
              profiles={profiles.data ?? []}
              saving={save.isPending}
              error={save.isError ? errorMessage(save.error) : ""}
              onCancel={() => setEditor(undefined)}
              onSave={(values) => save.mutate({ id: editor?.id, values })}
            />
          )}
        </SheetContent>
      </Sheet>
      <AlertDialog
        open={!!disabling}
        onOpenChange={(open) => {
          if (!open && !disable.isPending) setDisabling(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              停用 {disabling?.name} 的平台连接？
            </AlertDialogTitle>
            <AlertDialogDescription>
              停用后将暂停该连接的检测。集群标识和现有授权会保留，可通过编辑重新启用。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disable.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={disable.isPending}
              onClick={(event) => {
                event.preventDefault()
                if (disabling) disable.mutate(disabling)
              }}
            >
              确认停用
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
