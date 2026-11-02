import { useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { GitCompareArrows, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableHeader,
  TableHead,
  TableRow,
  TableBody,
  TableCell,
} from "@/components/ui/table"
import { ConfigurationDiff } from "@/features/config-history/diff-table"
import {
  canHistory,
  validId,
  versionsSchema,
  comparisonSchema,
  errorMessage,
  type Version,
} from "@/features/config-history/schema"
import { dateTime, actions } from "@/features/audits/schema"
import { pageNumber } from "@/features/clusters/cluster-picker"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
function Evidence({ version }: { version: Version }) {
  return (
    <article className="rounded-md border p-4 text-sm">
      <h3 className="font-semibold">
        v{version.revision} ·{" "}
        {version.kind === "baseline"
          ? "上线基线"
          : actions[version.action] || version.action}
      </h3>
      <p className="mt-2">记录于 {dateTime(version.recordedAt)}</p>
      <p className="mt-2 break-all">
        {version.kind === "baseline"
          ? "迁移时的当前配置，原始修改人未记录"
          : `${version.actorLabel || version.actorId || "未记录操作人"} · ${version.actorSource === "local" ? "本机管理命令" : "平台用户"}`}
      </p>
      {version.operationId && (
        <Link
          className="mt-2 inline-block underline underline-offset-4"
          to={`/audits?${new URLSearchParams({ operation: version.operationId })}`}
        >
          查看关联审计
        </Link>
      )}
      <p className="mt-2 font-mono text-xs break-all">版本 ID {version.id}</p>
    </article>
  )
}
export function ConfigurationHistoryPage() {
  const user = useAuth((s) => s.user),
    [params, setParams] = useSearchParams(),
    cid = params.get("cluster") || ""
  return (
    <div className="mx-auto max-w-screen-2xl space-y-6 p-5 sm:p-8">
      <header>
        <p className="eyebrow">CONFIGURATION HISTORY</p>
        <h1 className="mt-2 flex items-center gap-3 text-3xl font-semibold">
          <GitCompareArrows className="size-7 text-primary" />
          配置变更对比
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          比较平台集群连接配置的已保存版本，追溯记录时间和审计来源。
        </p>
      </header>
      {!canHistory(user?.access.platformPermissions || []) ? (
        <p role="alert" className="panel p-5">
          需要平台配置管理和审计读取权限。
        </p>
      ) : (
        <>
          <form
            className="panel flex flex-wrap items-end gap-3 p-5"
            aria-label="选择配置历史集群"
            onSubmit={(e) => {
              e.preventDefault()
              const id = String(
                new FormData(e.currentTarget).get("cluster") || ""
              ).trim()
              setParams({ cluster: id })
            }}
          >
            <Label className="grid min-w-64 flex-1 gap-2">
              集群 ID
              <Input
                key={cid}
                name="cluster"
                defaultValue={cid}
                maxLength={15}
                required
                pattern="[a-z0-9]{15}"
                placeholder="在集群配置中进入，或填写保留的集群 ID"
              />
            </Label>
            <Button>查看版本</Button>
            <Button variant="outline" asChild>
              <Link to="/clusters">集群配置</Link>
            </Button>
          </form>
          {cid && !validId(cid) ? (
            <p role="alert">请输入合法的 15 位集群 ID。</p>
          ) : !cid ? (
            <p role="status" className="panel p-5">
              从集群配置的“版本对比”进入，也可使用已删除集群的保留 ID。
            </p>
          ) : (
            <Workspace
              key={`${user?.id}:${cid}:${params.get("from")}:${params.get("to")}`}
              cid={cid}
            />
          )}
        </>
      )}
    </div>
  )
}
function Workspace({ cid }: { cid: string }) {
  const user = useAuth((s) => s.user),
    [params, setParams] = useSearchParams(),
    page = pageNumber(params.get("page")),
    from = params.get("from") || "",
    to = params.get("to") || ""
  const [draftFrom, setDraftFrom] = useState(from),
    [draftTo, setDraftTo] = useState(to)
  const base = `/cluster-configurations/${cid}`
  const versions = useQuery({
    queryKey: ["configuration-versions", user?.id, cid, page],
    queryFn: async ({ signal }) =>
      versionsSchema.parse(
        await api(`${base}/versions?page=${page}&perPage=20`, { signal })
      ),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const compare = useQuery({
    queryKey: ["configuration-comparison", user?.id, cid, from, to],
    enabled: validId(from) && validId(to),
    queryFn: async ({ signal }) => {
      const d = comparisonSchema.parse(
        await api(`${base}/compare?${new URLSearchParams({ from, to })}`, {
          signal,
        })
      )
      if (d.clusterId !== cid || d.from.id !== from || d.to.id !== to)
        throw new Error("Comparison identity changed")
      return d
    },
    retry: false,
    refetchOnWindowFocus: false,
  })
  const blocked = versions.isError || compare.isError,
    data = !blocked ? versions.data : undefined,
    diff = !blocked ? compare.data : undefined
  const update = (key: string, value: string) =>
    setParams((p) => {
      const n = new URLSearchParams(p)
      n.set(key, value)
      return n
    })
  return (
    <>
      <section
        className="panel space-y-3 p-5 text-sm"
        aria-label="版本覆盖说明"
      >
        <p>
          版本记录从上线基线开始；基线记录时间是采集时间，不代表原始修改时间。快照仅保存名称、标识、说明、连接引用、API
          版本、启停和超时。
        </p>
        <p className="text-muted-foreground">
          这里只读对比，不恢复或应用历史配置。第三方配置文件、服务端连接方式文件与直接数据库修改不在版本写入覆盖内。编号可能不连续，缺失版本不从旧审计推算。
        </p>
        {data && (
          <p
            className={
              data.currentStatus === "different" ? "text-amber-600" : ""
            }
          >
            {
              {
                matched: "当前配置与最新快照一致",
                different: "当前配置与最新快照不同，可能存在未记录的修改",
                missing: "当前集群已不存在；保留的历史版本仍可对比",
                no_history: "此集群尚无版本快照",
              }[data.currentStatus]
            }{" "}
            · 当前版本{" "}
            {data.currentRevision === null
              ? "不可用"
              : `v${data.currentRevision}`}{" "}
            · 读取 {dateTime(data.fetchedAt)}
          </p>
        )}
      </section>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-semibold">集群 {cid} 的版本记录</h2>
        <Button
          variant="outline"
          disabled={versions.isFetching || compare.isFetching}
          onClick={() => {
            void versions.refetch()
            if (validId(from) && validId(to)) void compare.refetch()
          }}
        >
          <RefreshCw />
          刷新版本
        </Button>
      </div>
      {blocked && (
        <p role="alert" className="panel p-5 text-destructive">
          {errorMessage(versions.error || compare.error)}
        </p>
      )}
      {versions.isPending && <p role="status">正在读取版本记录…</p>}
      {data && (
        <section className="panel overflow-hidden" aria-label="配置版本列表">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>版本</TableHead>
                <TableHead>记录时间</TableHead>
                <TableHead>来源</TableHead>
                <TableHead>操作人</TableHead>
                <TableHead>选择对比</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.map((v) => (
                <TableRow key={v.id}>
                  <TableCell className="font-mono">
                    v{v.revision}
                    {v.kind === "baseline" && (
                      <Badge variant="outline" className="ml-2">
                        上线基线
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>{dateTime(v.recordedAt)}</TableCell>
                  <TableCell>
                    {v.kind === "baseline"
                      ? "迁移基线"
                      : actions[v.action] || v.action}
                  </TableCell>
                  <TableCell className="max-w-44 break-all">
                    {v.kind === "baseline"
                      ? "未记录"
                      : v.actorLabel || v.actorId || "未记录"}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setDraftFrom(v.id)}
                        aria-label={`以 v${v.revision} 为基准`}
                      >
                        设为基准
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setDraftTo(v.id)}
                        aria-label={`以 v${v.revision} 为比较版本`}
                      >
                        设为比较
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!data.items.length && (
            <p role="status" className="p-8 text-center text-muted-foreground">
              没有可用版本。
            </p>
          )}
          <div className="flex items-center justify-between p-5 text-sm">
            <span>
              共 {data.totalItems} 条 · 第 {page} 页
            </span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                disabled={page <= 1}
                onClick={() => update("page", String(page - 1))}
              >
                上一页
              </Button>
              <Button
                variant="outline"
                disabled={page * 20 >= data.totalItems}
                onClick={() => update("page", String(page + 1))}
              >
                下一页
              </Button>
            </div>
          </div>
        </section>
      )}
      <form
        className="panel space-y-4 p-5"
        aria-label="选择两个配置版本"
        onSubmit={(e) => {
          e.preventDefault()
          if (validId(draftFrom) && validId(draftTo))
            setParams((p) => {
              const n = new URLSearchParams(p)
              n.set("from", draftFrom)
              n.set("to", draftTo)
              return n
            })
        }}
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Label className="grid gap-2">
            基准版本 ID
            <Input
              value={draftFrom}
              onChange={(e) => setDraftFrom(e.target.value.trim())}
              maxLength={15}
              placeholder="从列表选择，支持跨页"
            />
          </Label>
          <Label className="grid gap-2">
            比较版本 ID
            <Input
              value={draftTo}
              onChange={(e) => setDraftTo(e.target.value.trim())}
              maxLength={15}
              placeholder="从列表选择，支持反向或同版本比较"
            />
          </Label>
        </div>
        <Button
          disabled={
            !validId(draftFrom) || !validId(draftTo) || versions.isFetching
          }
        >
          比较所选版本
        </Button>
        {(from || to) && !(validId(from) && validId(to)) && (
          <p role="alert" className="text-sm text-destructive">
            URL 中的版本 ID 不合法，请重新选择。
          </p>
        )}
      </form>
      {validId(from) && validId(to) && compare.isPending && (
        <p role="status">正在对比保存的版本…</p>
      )}
      {diff && (
        <section className="panel space-y-5 p-5" aria-label="历史配置对比结果">
          <h2 className="text-lg font-semibold">
            v{diff.from.revision} → v{diff.to.revision} · 只读对比
          </h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <Evidence version={diff.from} />
            <Evidence version={diff.to} />
          </div>
          <ConfigurationDiff key={`${from}:${to}`} fields={diff.fields} />
          <p className="text-xs text-muted-foreground">
            比较方向仅决定差异展示，不执行回滚。记录不是防篡改证明；原生数据库管理员仍可修改本地数据。
          </p>
        </section>
      )}
    </>
  )
}
