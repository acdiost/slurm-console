import { Link } from "react-router"
import { ArrowUpRight, Server, Layers3, Network } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { memory, timestamp } from "@/features/overview/schema"
import { clusterLink, stateLabels, type FleetItem } from "./schema"
export function FleetCard({ item }: { item: FleetItem }) {
  const { cluster, nodes, jobs, health } = item,
    n = nodes.status === "ok" ? nodes.item : undefined,
    j = jobs.status === "ok" ? jobs.item : undefined
  const tone =
    item.state === "connected"
      ? "border-success/30 text-success"
      : ["unavailable", "controller_down"].includes(item.state)
        ? "border-destructive/30 text-destructive"
        : "border-warning/30 text-warning"
  return (
    <article
      className="panel flex flex-col overflow-hidden"
      aria-label={`集群 ${cluster.name}`}
    >
      <div className="space-y-3 border-b p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <Badge variant="outline" className={tone}>
            {stateLabels[item.state]}
          </Badge>
          <span className="font-mono text-2xs text-muted-foreground">
            {cluster.slug}
          </span>
        </div>
        <h2 className="text-lg font-semibold break-all">
          <Link
            to={clusterLink("/overview", cluster.id)}
            className="hover:text-primary"
          >
            {cluster.name}
          </Link>
        </h2>
        <p
          className="line-clamp-2 min-h-10 text-xs leading-5 text-muted-foreground"
          title={cluster.description}
        >
          {cluster.description || "暂无集群描述"}
        </p>
        <p className="text-2xs text-muted-foreground">
          {health.status === "ok"
            ? `${health.item.serverVersion} · ${health.item.apiVersion} · ${timestamp(health.fetchedAt)}${health.cached ? " 缓存" : ""}`
            : health.message}
        </p>
      </div>
      <div className="flex-1 space-y-5 p-5">
        <section aria-label={`${cluster.name} 节点资源`} className="space-y-3">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-xs font-semibold">节点与容量</h3>
            <span className="text-2xs text-muted-foreground">
              {n
                ? `${timestamp(nodes.fetchedAt)}${nodes.cached ? " · 缓存" : ""}`
                : ""}
            </span>
          </div>
          {n ? (
            <>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <p className="text-2xs text-muted-foreground">节点总数</p>
                  <p className="mt-1 font-mono text-xl font-semibold">
                    {n.total.toLocaleString("zh-CN")}
                  </p>
                  <p className="mt-1 text-2xs text-muted-foreground">
                    {n.unavailable} 不可用
                  </p>
                </div>
                <div>
                  <p className="text-2xs text-muted-foreground">CPU 总量</p>
                  <p className="mt-1 font-mono text-xl font-semibold">
                    {n.cpus.total.toLocaleString("zh-CN")}
                  </p>
                  <p className="mt-1 text-2xs text-muted-foreground">
                    {n.cpus.idle} 空闲 · {n.cpus.unavailable} 不可用/预留
                  </p>
                </div>
              </div>
              <Progress
                value={
                  n.cpus.total
                    ? Math.min(100, (100 * n.cpus.allocated) / n.cpus.total)
                    : 0
                }
                aria-label={`${cluster.name} CPU 已分配比例`}
              />
              <p className="text-2xs leading-5 text-muted-foreground">
                CPU 已分配 {n.cpus.allocated} · 内存 {memory(n.memoryMiB.total)}{" "}
                · GPU {n.gpus.total === null ? "未知" : n.gpus.total}
                {n.gpus.unknownNodes > 0
                  ? `（${n.gpus.unknownNodes} 节点数量未确认）`
                  : ""}
              </p>
            </>
          ) : (
            <p className="rounded-md border bg-muted/20 p-3 text-xs leading-6 text-muted-foreground">
              {nodes.status === "forbidden"
                ? "未授予节点读取权限。"
                : nodes.message}
              <span className="mt-1 block font-mono">容量 —</span>
            </p>
          )}
        </section>
        <section
          aria-label={`${cluster.name} 作业`}
          className="space-y-3 border-t pt-4"
        >
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-xs font-semibold">当前可见作业</h3>
            <span className="text-2xs text-muted-foreground">
              {j
                ? `${timestamp(jobs.fetchedAt)}${jobs.cached ? " · 缓存" : ""}`
                : ""}
            </span>
          </div>
          {j ? (
            <div className="grid grid-cols-3 gap-2 text-center">
              {[
                ["运行中", j.running],
                ["排队中", j.pending],
                ["异常结束", j.failed],
              ].map(([label, value]) => (
                <div key={label} className="rounded-md border bg-muted/10 p-2">
                  <p className="font-mono text-lg font-semibold">{value}</p>
                  <p className="mt-1 text-2xs text-muted-foreground">{label}</p>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-xs leading-6 text-muted-foreground">
              {jobs.status === "forbidden"
                ? "未授予作业读取权限。"
                : jobs.message}
            </p>
          )}
        </section>
      </div>
      <div className="flex flex-wrap items-center gap-2 border-t p-4">
        <Button asChild size="sm" variant="outline">
          <Link to={clusterLink("/overview", cluster.id)}>
            <ArrowUpRight />
            集群总览
          </Link>
        </Button>
        {item.configuration === "enabled" &&
          item.permissions.includes("jobs.read") && (
            <Button asChild size="sm" variant="ghost">
              <Link to={clusterLink("/jobs", cluster.id)}>
                <Layers3 />
                作业
              </Link>
            </Button>
          )}
        {item.configuration === "enabled" &&
          item.permissions.includes("nodes.read") && (
            <Button asChild size="sm" variant="ghost">
              <Link to={clusterLink("/nodes", cluster.id)}>
                <Server />
                节点
              </Link>
            </Button>
          )}
        {item.configuration === "enabled" &&
          item.permissions.includes("partitions.read") && (
            <Button asChild size="sm" variant="ghost">
              <Link to={clusterLink("/partitions", cluster.id)}>
                <Network />
                分区
              </Link>
            </Button>
          )}
      </div>
    </article>
  )
}
