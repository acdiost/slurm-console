import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import {
  ArrowDown,
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  RefreshCw,
  Search,
  Settings2,
  Server,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/services/api"
import { pageNumber } from "@/features/clusters/cluster-picker"
import { timestamp } from "@/features/overview/schema"
import { FleetCard } from "@/features/fleet/fleet-card"
import { errorMessage, fleetPageSchema } from "@/features/fleet/schema"
const filterSchema = z.object({
  q: z.string().trim().max(128, "最多 128 个字符"),
  configuration: z.string(),
})
type Filters = z.infer<typeof filterSchema>
function FleetFilters({
  initial,
  onApply,
}: {
  initial: Filters
  onApply: (v: Filters) => void
}) {
  const {
      register,
      control,
      setValue,
      handleSubmit,
      reset,
      formState: { errors },
    } = useForm<Filters>({
      resolver: zodResolver(filterSchema),
      defaultValues: initial,
    }),
    configuration = useWatch({ control, name: "configuration" })
  return (
    <form
      noValidate
      onSubmit={handleSubmit(onApply)}
      className="panel flex flex-wrap items-end gap-4 p-4"
    >
      <div className="min-w-56 flex-1 space-y-2">
        <Label htmlFor="fleet-search">搜索集群</Label>
        <Input
          id="fleet-search"
          placeholder="名称、标识或描述"
          autoComplete="off"
          {...register("q")}
          aria-invalid={!!errors.q}
        />
        {errors.q && (
          <p role="alert" className="text-2xs text-destructive">
            {errors.q.message}
          </p>
        )}
      </div>
      <div className="w-full space-y-2 sm:w-48">
        <Label htmlFor="fleet-configuration">配置状态</Label>
        <Select
          value={configuration || "all"}
          onValueChange={(v) => setValue("configuration", v === "all" ? "" : v)}
        >
          <SelectTrigger id="fleet-configuration" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部配置</SelectItem>
            <SelectItem value="enabled">已启用</SelectItem>
            <SelectItem value="disabled">已停用</SelectItem>
            <SelectItem value="unconfigured">未配置</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="flex gap-2">
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => {
            const empty = { q: "", configuration: "" }
            reset(empty)
            onApply(empty)
          }}
        >
          清除筛选
        </Button>
        <Button type="submit" size="sm">
          <Search />
          查询集群
        </Button>
      </div>
    </form>
  )
}
export function FleetPage() {
  const [params, setParams] = useSearchParams(),
    [automatic, setAutomatic] = useState(true)
  const page = pageNumber(params.get("page")),
    perPage = [6, 12, 24].includes(Number(params.get("perPage")))
      ? Number(params.get("perPage"))
      : 12
  const filters = {
      q: params.get("q") || "",
      configuration: params.get("configuration") || "",
    },
    sort = params.get("sort") || "name",
    order = params.get("order") || "asc"
  const list = new URLSearchParams({
    page: String(page),
    perPage: String(perPage),
    sort,
    order,
  })
  for (const [k, v] of Object.entries(filters)) if (v) list.set(k, v)
  const query = useQuery({
    queryKey: ["cluster-fleet", list.toString()],
    queryFn: async ({ signal }) =>
      fleetPageSchema.parse(await api(`/cluster-fleet?${list}`, { signal })),
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
  const data = query.isError ? undefined : query.data
  const update = (changes: Record<string, string | null>) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      for (const [k, v] of Object.entries(changes)) {
        if (v === null || v === "") next.delete(k)
        else next.set(k, v)
      }
      return next
    })
  return (
    <div className="mx-auto max-w-screen-2xl space-y-6 p-6 sm:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="section-eyebrow">CLUSTER FLEET</p>
          <h1 className="mt-2 text-2xl font-semibold">集群工作台</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            在授权范围内查看各集群，进入对应资源与调度工作空间。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Select
            value={automatic ? "30" : "off"}
            onValueChange={(v) => setAutomatic(v === "30")}
          >
            <SelectTrigger aria-label="工作台自动刷新" className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="30">每 30 秒刷新</SelectItem>
              <SelectItem value="off">暂停自动刷新</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
            刷新工作台
          </Button>
          {data?.canManage && (
            <Button asChild variant="outline">
              <Link to="/clusters">
                <Settings2 />
                集群配置
              </Link>
            </Button>
          )}
        </div>
      </header>
      <FleetFilters
        key={JSON.stringify(filters)}
        initial={filters}
        onApply={(v) => update({ ...v, page: "1" })}
      />
      {query.isPending && (
        <div
          role="status"
          aria-label="正在读取集群工作台"
          className="grid gap-4 md:grid-cols-2 xl:grid-cols-3"
        >
          {[1, 2, 3].map((k) => (
            <Skeleton className="h-96" key={k} />
          ))}
        </div>
      )}
      {query.isError && (
        <div role="alert" className="panel p-5 text-sm text-destructive">
          {errorMessage(query.error)}
        </div>
      )}
      {data && (
        <>
          <section
            className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4"
            aria-label="本页集群统计"
          >
            {[
              [
                "登记集群",
                String(data.totalItems),
                "符合筛选条件的可见登记总数",
              ],
              [
                "本页控制器连通",
                String(data.pageSummary.connected),
                "连通不代表所有节点可调度",
              ],
              [
                "本页连接/数据待处理",
                String(data.pageSummary.needsAttention),
                `${data.pageSummary.partial} 个集群的资源数据不完整`,
              ],
              [
                "本页停用/未配置",
                String(
                  data.pageSummary.disabled + data.pageSummary.unconfigured
                ),
                "按配置状态区分，不请求上游",
              ],
            ].map(([label, value, description]) => (
              <article key={label} className="panel p-4">
                <h2 className="text-2xs text-muted-foreground">{label}</h2>
                <p className="mt-2 font-mono text-2xl font-semibold">{value}</p>
                <p className="mt-2 text-2xs leading-5 text-muted-foreground">
                  {description}
                </p>
              </article>
            ))}
          </section>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-2xs text-muted-foreground">
              本次查询 {timestamp(data.generatedAt)} · 各来源采样时间单独标注
            </p>
            <div className="flex items-center gap-2">
              <Select
                value={sort}
                onValueChange={(v) => update({ sort: v, page: "1" })}
              >
                <SelectTrigger aria-label="集群排序" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="name">按集群名称</SelectItem>
                  <SelectItem value="slug">按集群标识</SelectItem>
                </SelectContent>
              </Select>
              <Button
                size="icon"
                variant="outline"
                aria-label="切换集群排序方向"
                onClick={() =>
                  update({ order: order === "asc" ? "desc" : "asc", page: "1" })
                }
              >
                {order === "asc" ? <ArrowUp /> : <ArrowDown />}
              </Button>
            </div>
          </div>
          {data.items.length ? (
            <section
              className="grid gap-5 md:grid-cols-2 xl:grid-cols-3"
              aria-label="集群卡片"
            >
              {data.items.map((item) => (
                <FleetCard key={item.cluster.id} item={item} />
              ))}
            </section>
          ) : (
            <div className="panel space-y-3 border-dashed px-5 py-14 text-center">
              <Server className="mx-auto size-7 text-muted-foreground" />
              <h2 className="text-sm font-medium">
                {data.totalItems > 0
                  ? "本页没有集群"
                  : "没有符合条件的可访问集群"}
              </h2>
              <p className="text-xs text-muted-foreground">
                调整筛选或请管理员检查集群读取授权与登记配置。
              </p>
              {page > 1 && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => update({ page: "1" })}
                >
                  返回第一页
                </Button>
              )}
            </div>
          )}
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-2xs text-muted-foreground">
              共 {data.totalItems} 个登记集群 · 第 {page} 页
            </p>
            <div className="flex items-center gap-2">
              <Select
                value={String(perPage)}
                onValueChange={(v) => update({ perPage: v, page: "1" })}
              >
                <SelectTrigger aria-label="每页集群数" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {[6, 12, 24].map((n) => (
                    <SelectItem value={String(n)} key={n}>
                      {n} 个 / 页
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                size="icon"
                variant="outline"
                aria-label="上一页集群"
                disabled={page <= 1 || query.isFetching}
                onClick={() => update({ page: String(page - 1) })}
              >
                <ChevronLeft />
              </Button>
              <Button
                size="icon"
                variant="outline"
                aria-label="下一页集群"
                disabled={page * perPage >= data.totalItems || query.isFetching}
                onClick={() => update({ page: String(page + 1) })}
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </>
      )}
      <p className="rounded-md border p-4 text-2xs leading-6 text-muted-foreground">
        工作台仅查询数据。状态与资源分别采样；统计按登记连接计数。同一 Slurm
        实例可能被多次登记，资源仅逐集群展示，不跨集群相加。无法确认或无权读取的数据保留为未知。
      </p>
    </div>
  )
}
