import { useQuery } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import { api } from "@/services/api"
import { runtimeSchema } from "./schema"
import { date } from "@/features/capacity/schema"
export function RuntimeSettings({ actor }: { actor: string }) {
  const q = useQuery({
    queryKey: ["platform-runtime", actor],
    queryFn: async ({ signal }) =>
      runtimeSchema.parse(await api("/platform-settings/runtime", { signal })),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const r = !q.isError ? q.data?.runtime : undefined
  return (
    <section className="panel space-y-5 p-5 sm:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">运行信息</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            当前应用进程和构建版本。
          </p>
        </div>
        <Button
          variant="outline"
          disabled={q.isFetching}
          onClick={() => void q.refetch()}
        >
          刷新运行信息
        </Button>
      </div>
      {q.isPending && <p role="status">正在读取运行信息…</p>}
      {q.isError && <p role="alert">运行信息不可用或访问权限已变化。</p>}
      {r && (
        <dl className="grid gap-4 sm:grid-cols-2">
          {[
            [
              "构建修订",
              `${r.revision}${r.modified ? "（含未提交修改）" : ""}`,
            ],
            ["运行平台", `${r.os} / ${r.arch}`],
            ["Go 版本", r.goVersion],
            ["应用数据库", r.database],
            ["进程启动时间", date(r.startedAt)],
            ["应用执行身份", r.runAsRoot ? "root" : "非 root"],
          ].map(([k, v]) => (
            <div key={k} className="rounded-md border p-4">
              <dt className="text-xs text-muted-foreground">{k}</dt>
              <dd className="mt-2 text-sm break-all">{v}</dd>
            </div>
          ))}
        </dl>
      )}
      <div className="rounded-md bg-muted/40 p-4 text-sm leading-7 text-muted-foreground">
        备份、恢复、升级及服务重启由服务器维护工具执行。本页展示进程与已加载配置，不将配置状态视为外部服务健康检查。
      </div>
    </section>
  )
}
