import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import {
  ChevronLeft,
  ChevronRight,
  Database,
  RefreshCw,
  Server,
  Settings2,
  ShieldOff,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import {
  clusterPageSchema,
  overviewSchema,
  timestamp,
} from "@/features/overview/schema"
import {
  AttentionPanel,
  ControllerPanel,
  JobsPanel,
  NodeStatesPanel,
  PartitionsPanel,
  ResourceCards,
} from "@/features/overview/panels"

export function overviewError(error: unknown) {
  if (error instanceof ClientResponseError) {
    if (error.status === 403) return "当前账户没有访问此集群总览的权限。"
    if (error.status === 404) return "当前集群不存在或已被移除。"
    return error.response?.error?.message || "暂时无法读取总览，请稍后刷新。"
  }
  return "暂时无法读取总览，请检查网络后重试。"
}
export function OverviewPage() {
  const [params, setParams] = useSearchParams()
  const [automatic, setAutomatic] = useState(true)
  const pageValue = Number(params.get("page") ?? 1)
  const page =
    Number.isInteger(pageValue) && pageValue >= 1 && pageValue <= 1000000
      ? pageValue
      : 1
  const canManage = useAuth(
    (state) =>
      state.user?.access.platformPermissions.includes(
        "platform.clusters.manage"
      ) ?? false
  )
  const client = useQueryClient()
  const inventory = useQuery({
    queryKey: ["accessible-clusters", page],
    queryFn: async ({ signal }) =>
      clusterPageSchema.parse(
        await api(`/clusters?page=${page}&perPage=20`, { signal })
      ),
    retry: false,
  })
  const clusterId = params.get("cluster") || inventory.data?.items[0]?.id || ""
  const overview = useQuery({
    queryKey: ["cluster-overview", clusterId],
    enabled: !!clusterId,
    queryFn: async ({ signal }) =>
      overviewSchema.parse(
        await api(`/clusters/${encodeURIComponent(clusterId)}/overview`, {
          signal,
        })
      ),
    staleTime: 5000,
    retry: false,
    refetchOnWindowFocus: automatic,
    refetchInterval: (query) =>
      automatic &&
      !(
        query.state.error instanceof ClientResponseError &&
        [403, 404].includes(query.state.error.status)
      )
        ? 10000
        : false,
  })
  // A failed permission check must immediately hide previously cached metrics.
  const data = overview.isError ? undefined : overview.data
  const clusters = inventory.data?.items ?? []
  const extraCluster =
    data && !clusters.some((cluster) => cluster.id === data.cluster.id)
      ? [data.cluster]
      : []
  const refresh = () => {
    void inventory.refetch()
    if (clusterId) void overview.refetch()
    void client.invalidateQueries({ queryKey: ["cluster-inventory"] })
  }
  const movePage = (next: number) => setParams({ page: String(next) })
  const loading =
    (inventory.isPending && !clusterId) || (!!clusterId && overview.isPending)
  const emptyPage = !!inventory.data?.totalItems && !clusters.length
  return (
    <div className="mx-auto max-w-7xl space-y-5 p-6 sm:p-8">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="section-eyebrow">CLUSTER OVERVIEW</p>
          <h1 className="mt-2 text-2xl font-semibold">集群总览</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {data
              ? `${data.cluster.name} · ${data.cluster.description || data.cluster.slug}`
              : "查看集群资源、调度状态与当前需关注项。"}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={automatic ? "automatic" : "paused"}
            onValueChange={(value) => setAutomatic(value === "automatic")}
          >
            <SelectTrigger aria-label="自动刷新" className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="automatic">每 10 秒刷新</SelectItem>
              <SelectItem value="paused">暂停刷新</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            disabled={overview.isFetching || inventory.isFetching}
            onClick={refresh}
          >
            <RefreshCw
              className={
                overview.isFetching || inventory.isFetching
                  ? "animate-spin motion-reduce:animate-none"
                  : ""
              }
            />
            刷新总览
          </Button>
        </div>
      </header>
      <section
        aria-label="集群选择"
        className="panel flex flex-wrap items-center gap-3 px-4 py-3"
      >
        <Database className="size-4 text-muted-foreground" />
        <Select
          value={clusterId}
          disabled={!clusters.length && !extraCluster.length}
          onValueChange={(value) =>
            setParams({ page: String(page), cluster: value })
          }
        >
          <SelectTrigger aria-label="选择集群" className="w-full sm:w-64">
            <SelectValue placeholder="选择集群" />
          </SelectTrigger>
          <SelectContent>
            {[...extraCluster, ...clusters].map((cluster) => (
              <SelectItem key={cluster.id} value={cluster.id}>
                {cluster.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="text-2xs text-muted-foreground">
          {inventory.isError
            ? "集群列表暂不可用"
            : inventory.data
              ? `${inventory.data.totalItems} 个可见集群 · 第 ${page} 页`
              : "正在读取集群列表…"}
        </span>
        <div className="flex items-center gap-1">
          <Button
            size="icon"
            variant="ghost"
            aria-label="上一组集群"
            disabled={page <= 1 || inventory.isFetching}
            onClick={() => movePage(page - 1)}
          >
            <ChevronLeft />
          </Button>
          <Button
            size="icon"
            variant="ghost"
            aria-label="下一组集群"
            disabled={
              !inventory.data ||
              page * inventory.data.perPage >= inventory.data.totalItems ||
              inventory.isFetching
            }
            onClick={() => movePage(page + 1)}
          >
            <ChevronRight />
          </Button>
        </div>
        {canManage && (
          <Button asChild size="sm" variant="ghost" className="sm:ml-auto">
            <Link to="/clusters">
              <Settings2 />
              集群配置
            </Link>
          </Button>
        )}
      </section>
      {inventory.isError && (
        <div role="alert" className="panel p-5 text-xs text-destructive">
          无法读取集群列表，请刷新重试。
        </div>
      )}
      {loading && (
        <div role="status" aria-label="正在读取集群总览" className="space-y-4">
          <Skeleton className="h-24" />
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {[1, 2, 3, 4].map((key) => (
              <Skeleton key={key} className="h-56" />
            ))}
          </div>
          <Skeleton className="h-64" />
        </div>
      )}
      {overview.isError && clusterId && (
        <div role="alert" className="panel flex items-start gap-3 p-6">
          <ShieldOff className="mt-1 size-5 shrink-0 text-[var(--amber-11)]" />
          <div>
            <h2 className="text-sm font-semibold">暂时无法展示此集群</h2>
            <p className="mt-2 text-xs leading-6 text-muted-foreground">
              {overviewError(overview.error)}
            </p>
            <p className="mt-2 text-2xs text-muted-foreground">
              可以切换其他集群，或刷新后重试。
            </p>
          </div>
        </div>
      )}
      {!inventory.isPending && !inventory.isError && !clusterId && (
        <div className="panel flex flex-col items-center gap-3 border-dashed px-6 py-14 text-center">
          <Server className="size-7 text-muted-foreground" />
          <h2 className="text-base font-semibold">
            {emptyPage ? "本页暂无集群" : "暂无可访问的集群"}
          </h2>
          <p className="max-w-md text-xs leading-6 text-muted-foreground">
            {emptyPage
              ? "集群列表可能已变化，请返回第一页重新选择。"
              : canManage
                ? "先登记并启用集群连接，再查看真实资源与调度状态。"
                : "请联系管理员授予集群读取权限。"}
          </p>
          {emptyPage ? (
            <Button
              variant="outline"
              className="mt-2"
              onClick={() => movePage(1)}
            >
              返回第一页
            </Button>
          ) : (
            <Button asChild variant="outline" className="mt-2">
              <Link to={canManage ? "/clusters" : "/access"}>
                {canManage ? "配置集群连接" : "查看我的权限"}
              </Link>
            </Button>
          )}
        </div>
      )}
      {data && (
        <>
          <div className="flex flex-wrap items-center justify-between gap-2 text-2xs text-muted-foreground">
            <span className="font-mono">{data.cluster.slug}</span>
            <span className="flex items-center gap-2">
              <Badge variant="outline" className="font-normal">
                只读概况
              </Badge>
              总览生成于 {timestamp(data.generatedAt)}
              {overview.isFetching && " · 正在更新…"}
            </span>
          </div>
          <ControllerPanel section={data.health} />
          <ResourceCards section={data.nodes} />
          <div className="grid items-start gap-5 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
            <div className="min-w-0 space-y-5">
              <JobsPanel section={data.jobs} />
              <PartitionsPanel section={data.partitions} />
            </div>
            <div className="min-w-0 space-y-5">
              <NodeStatesPanel section={data.nodes} />
              <AttentionPanel overview={data} />
            </div>
          </div>
          <footer className="text-2xs leading-6 text-muted-foreground">
            各项数据按来源时间更新，可能使用短时缓存。空闲量按节点状态与调度分配估算，不代表任意作业均可立即启动；GPU
            调度分配量不等于实际利用率。历史记账和管理操作审计将在后续接入。
          </footer>
        </>
      )}
    </div>
  )
}
