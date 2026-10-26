import { useQuery } from "@tanstack/react-query"
import { ClientResponseError } from "pocketbase"
import { Link } from "react-router"
import {
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  RefreshCw,
} from "lucide-react"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from "@/components/ui/table"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import {
  nodePageSchema,
  errorMessage as nodeError,
} from "@/features/nodes/schema"
import { timestamp, memory } from "@/features/overview/schema"
import {
  errorMessage,
  partitionDetailSchema,
  partitionTime,
  memoryLimit,
  limit,
  stateHelp,
  type Detail,
} from "./schema"

function Fields({
  title,
  items,
}: {
  title: string
  items: [string, string][]
}) {
  return (
    <section className="border-t pt-5">
      <h2 className="mb-3 text-xs font-semibold">{title}</h2>
      <dl className="grid grid-cols-2 gap-x-5 gap-y-4">
        {items.map(([label, value]) => (
          <div key={label}>
            <dt className="text-2xs text-muted-foreground">{label}</dt>
            <dd className="mt-1.5 font-mono text-xs leading-6 break-all">
              {value || "未显式指定"}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
function Resources({ source }: { source: Detail["resources"] }) {
  if (source.status !== "ok")
    return (
      <section className="rounded-md border p-4">
        <h2 className="text-xs font-semibold">关联节点资源</h2>
        <p
          role="status"
          className="mt-3 text-xs leading-6 text-muted-foreground"
        >
          {source.status === "forbidden"
            ? "未授予节点读取权限，仅展示分区配置容量。"
            : source.message}
        </p>
      </section>
    )
  const { summary: s, configuredNodes, complete } = source.item,
    missing = s.total === 0 && configuredNodes > 0
  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-xs font-semibold">关联节点资源</h2>
        <span className="text-2xs text-muted-foreground">
          {timestamp(source.fetchedAt)} ·{" "}
          {source.cached ? "缓存数据" : "已更新"}
        </span>
      </div>
      {!complete && (
        <p
          role="status"
          className="border-warning/40 bg-warning/5 rounded-md border p-3 text-2xs leading-6"
        >
          当前可见关联节点 {s.total} / 配置 {configuredNodes}
          ，资源统计不完整，仅统计可见节点。
        </p>
      )}
      {missing ? (
        <p className="rounded-md border p-4 text-xs text-muted-foreground">
          未取得可见关联节点，无法统计其资源。
        </p>
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-2">
            {[
              [
                "节点",
                String(s.total),
                `${s.idle} 空闲 · ${s.allocated + s.mixed} 有作业分配 · ${s.unavailable} 不可用`,
              ],
              [
                "CPU",
                String(s.cpus.total),
                `${s.cpus.allocated} 已分配 · ${s.cpus.idle} 空闲 · ${s.cpus.unavailable} 不可用/预留`,
              ],
              [
                "内存",
                memory(s.memoryMiB.total),
                `${memory(s.memoryMiB.allocated)} 已分配 · ${memory(s.memoryMiB.idle)} 空闲`,
              ],
              [
                "GPU",
                s.gpus.total === null ? "未知" : String(s.gpus.total),
                s.gpus.allocated === null
                  ? "分配量未知"
                  : `${s.gpus.allocated} 已分配 · ${s.gpus.idle ?? "—"} 空闲`,
              ],
            ].map(([label, value, description]) => (
              <div key={label} className="rounded-md border p-3">
                <p className="text-2xs text-muted-foreground">{label}</p>
                <p className="mt-2 font-mono text-lg font-semibold">{value}</p>
                <p className="mt-2 text-2xs leading-6 text-muted-foreground">
                  {description}
                </p>
              </div>
            ))}
          </div>
          <div className="flex flex-wrap gap-2">
            {s.states.map((item) => (
              <span key={item.state} className="inline-flex items-center gap-1">
                <SlurmStatusBadge state={item.state} kind="node" />
                <span className="font-mono text-2xs text-muted-foreground">
                  {item.count}
                </span>
              </span>
            ))}
          </div>
        </>
      )}
      <p className="text-2xs leading-6 text-muted-foreground">
        按节点归属去重统计；分区间可能共享节点。空闲量受节点状态影响，GPU
        分配量不代表利用率。
      </p>
    </section>
  )
}
function AssociatedNodes({
  clusterId,
  name,
  page,
  automatic,
  onPage,
}: {
  clusterId: string
  name: string
  page: number
  automatic: boolean
  onPage: (page: number) => void
}) {
  const query = useQuery({
    queryKey: ["partition-nodes", clusterId, name, page],
    queryFn: async ({ signal }) =>
      nodePageSchema.parse(
        await api(
          `/clusters/${clusterId}/nodes?${new URLSearchParams({ partition: name, page: String(page), perPage: "10" })}`,
          { signal }
        )
      ),
    retry: false,
    refetchOnWindowFocus: automatic,
    refetchInterval: (q) =>
      automatic &&
      !(
        q.state.error instanceof ClientResponseError &&
        [403, 404].includes(q.state.error.status)
      )
        ? 10000
        : false,
  })
  const data = query.isError ? undefined : query.data
  return (
    <section className="space-y-3 border-t pt-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-xs font-semibold">关联节点列表</h2>
        <Button
          size="sm"
          variant="outline"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          <RefreshCw />
          刷新关联节点
        </Button>
      </div>
      {query.isPending && (
        <p role="status" className="text-xs">
          正在读取关联节点…
        </p>
      )}
      {query.isError && (
        <p role="alert" className="text-xs leading-6 text-destructive">
          {nodeError(query.error)}
        </p>
      )}
      {data && (
        <>
          <p className="text-2xs text-muted-foreground">
            {timestamp(data.fetchedAt)} · {data.cached ? "缓存数据" : "已更新"}{" "}
            · 共 {data.totalItems} 个可见节点
          </p>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>节点</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>CPU 分配/总量</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.map((n) => (
                <TableRow key={n.name}>
                  <TableCell>
                    <Button
                      asChild
                      variant="link"
                      size="sm"
                      className="h-auto p-0 font-mono text-xs"
                    >
                      <Link
                        to={`/nodes?${new URLSearchParams({ cluster: clusterId, partition: name, node: n.name })}`}
                      >
                        {n.name}
                        <ExternalLink className="size-3" />
                      </Link>
                    </Button>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {n.states.map((state) => (
                        <SlurmStatusBadge
                          key={state}
                          kind="node"
                          state={state}
                        />
                      ))}
                    </div>
                  </TableCell>
                  <TableCell className="font-mono text-2xs">
                    {n.allocatedCpus} / {n.cpus}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!data.items.length && (
            <p className="py-3 text-xs text-muted-foreground">
              {data.totalItems > 0
                ? "本页没有关联节点。"
                : "没有可见关联节点，归属以节点返回的分区列表为准。"}
            </p>
          )}
          <div className="flex items-center justify-between">
            <span className="text-2xs text-muted-foreground">第 {page} 页</span>
            <div className="flex gap-2">
              <Button
                size="icon"
                variant="outline"
                aria-label="上一页关联节点"
                disabled={page <= 1 || query.isFetching}
                onClick={() => onPage(page - 1)}
              >
                <ChevronLeft />
              </Button>
              <Button
                size="icon"
                variant="outline"
                aria-label="下一页关联节点"
                disabled={page * 10 >= data.totalItems || query.isFetching}
                onClick={() => onPage(page + 1)}
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </>
      )}
    </section>
  )
}
export function PartitionDetail({
  clusterId,
  name,
  automatic,
  nodesPage,
  onNodesPage,
  onClose,
}: {
  clusterId: string
  name: string
  automatic: boolean
  nodesPage: number
  onNodesPage: (page: number) => void
  onClose: () => void
}) {
  const access = useAuth((state) => state.user?.access)
  const canReadJobs =
    access?.platformPermissions.includes("jobs.read") ||
    access?.clusters.some(
      (item) =>
        item.cluster.id === clusterId && item.permissions.includes("jobs.read")
    )
  const query = useQuery({
    queryKey: ["partition", clusterId, name],
    queryFn: async ({ signal }) =>
      partitionDetailSchema.parse(
        await api(
          `/clusters/${clusterId}/partitions/${encodeURIComponent(name)}`,
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
        ? 10000
        : false,
  })
  const data = query.isError ? undefined : query.data,
    p = data?.item
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent className="overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-3xl">
        <SheetHeader>
          <SheetTitle className="break-all">分区 {name}</SheetTitle>
          <SheetDescription>
            查看调度状态、资源配置与当前可见关联节点。
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-6 px-5 pb-6">
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
              刷新详情
            </Button>
            {data && canReadJobs && (
              <Button asChild size="sm" variant="outline">
                <Link
                  to={`/jobs?${new URLSearchParams({ cluster: clusterId, partition: name })}`}
                >
                  查看分区作业
                  <ExternalLink />
                </Link>
              </Button>
            )}
          </div>
          {query.isPending && (
            <div role="status" aria-label="正在读取分区详情">
              <Skeleton className="h-64" />
            </div>
          )}
          {query.isError && (
            <p
              role="alert"
              className="rounded-md border p-4 text-xs leading-6 text-destructive"
            >
              {errorMessage(query.error)}
            </p>
          )}
          {p && data && (
            <>
              <section className="space-y-3 rounded-md border bg-muted/20 p-4">
                <div className="flex flex-wrap gap-1">
                  {p.states.map((state) => (
                    <SlurmStatusBadge
                      key={state}
                      state={state}
                      kind="partition"
                    />
                  ))}
                </div>
                <p className="text-xs leading-6">{stateHelp(p.states)}</p>
                <p className="text-2xs text-muted-foreground">
                  {timestamp(data.fetchedAt)} ·{" "}
                  {data.cached ? "缓存数据" : "已更新"}
                </p>
                <p className="font-mono text-2xs text-muted-foreground">
                  配置容量：{p.nodeCount} 节点 · {p.cpus} CPU
                </p>
                <p className="font-mono text-2xs leading-6 break-all">
                  节点表达式：{p.nodes || "未指定"}
                </p>
              </section>
              <Resources source={data.resources} />
              <Fields
                title="时限与节点约束"
                items={[
                  ["默认时限", partitionTime(p.defaultTimeMinutes)],
                  ["最长时限", partitionTime(p.maxTimeMinutes)],
                  ["最少节点", limit(p.minNodes)],
                  ["最多节点", limit(p.maxNodes)],
                  ["每节点 CPU 上限", limit(p.maxCpusPerNode)],
                  ["每插槽 CPU 上限", limit(p.maxCpusPerSocket)],
                  ["抢占宽限", limit(p.graceTimeSeconds, " 秒")],
                  ["允许超时", limit(p.overTimeLimitMinutes, " 分钟")],
                ]}
              />
              <p className="text-2xs leading-6 text-muted-foreground">
                未设置 DefaultTime 时由 Slurm 使用
                MaxTime；未返回的字段不推断为零或无限。
              </p>
              <Fields
                title="内存与共享约束"
                items={[
                  ["默认每 CPU 内存", memoryLimit(p.defaultMemoryPerCpuMiB)],
                  ["默认每节点内存", memoryLimit(p.defaultMemoryPerNodeMiB)],
                  ["每 CPU 内存上限", memoryLimit(p.maxMemoryPerCpuMiB)],
                  ["每节点内存上限", memoryLimit(p.maxMemoryPerNodeMiB)],
                  ["共享作业数配置", limit(p.oversubscribeJobs)],
                  ["共享标记", p.oversubscribeFlags.join(", ")],
                  ["优先级层级", limit(p.priorityTier)],
                  ["作业优先级因子", limit(p.priorityJobFactor)],
                ]}
              />
              <p className="text-2xs leading-6 text-muted-foreground">
                内存字段的配置值 0 依 Slurm 语义表示不限；实际仍受节点容量、QOS
                和关联限额约束。每 CPU/每节点字段分别展示，不重复叠加。
              </p>
              <Fields
                title="访问与调度策略"
                items={[
                  ["允许账户", p.allowedAccounts],
                  ["拒绝账户", p.deniedAccounts],
                  ["允许组", p.allowedGroups],
                  ["允许 QOS", p.allowedQos],
                  ["拒绝 QOS", p.deniedQos],
                  ["分区 QOS", p.assignedQos],
                  ["允许提交节点", p.allowedAllocationNodes],
                  ["替代分区", p.alternate],
                  ["选择类型", p.selectType.join(", ")],
                  ["默认作业参数", p.defaultJob],
                  ["节点集", p.nodeSets],
                  ["拓扑", p.topology],
                  ["配置 TRES", p.configuredTres],
                  ["TRES 计费权重", p.billingWeights],
                ]}
              />
              <p className="text-2xs leading-6 text-muted-foreground">
                空值遵循 Slurm 默认规则，不表示平台授予提交资格；平台读取权限与
                Slurm 调度准入分别校验。
              </p>
              {data.resources.status !== "forbidden" && (
                <AssociatedNodes
                  key={`${clusterId}:${name}`}
                  clusterId={clusterId}
                  name={name}
                  page={nodesPage}
                  automatic={automatic}
                  onPage={onNodesPage}
                />
              )}
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
