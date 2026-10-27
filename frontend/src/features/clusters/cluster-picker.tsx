import { useEffect } from "react"
import { useSearchParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { ChevronLeft, ChevronRight, Database, Search } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { clusterPageSchema, clusterSchema } from "@/features/overview/schema"
import { api } from "@/services/api"
export function pageNumber(value: string | null, fallback = 1, max = 1000000) {
  const n = Number(value ?? fallback)
  return Number.isInteger(n) && n >= 1 && n <= max ? n : fallback
}
export function useAccessibleClusters(legacyPage = false) {
  const [params, setParams] = useSearchParams(),
    page = pageNumber(
      params.get("clusterPage") || (legacyPage ? params.get("page") : null)
    ),
    search = params.get("clusterQ") || ""
  const list = new URLSearchParams({ page: String(page), perPage: "20" })
  if (search) list.set("q", search)
  const inventory = useQuery({
    queryKey: ["accessible-clusters", page, search],
    queryFn: async ({ signal }) =>
      clusterPageSchema.parse(await api(`/clusters?${list}`, { signal })),
    retry: false,
  })
  const items = inventory.isError ? [] : (inventory.data?.items ?? []),
    first = items[0]?.id
  const id = params.get("cluster") || first || ""
  // Canonical URLs preserve an implicitly selected page-two cluster across modules.
  useEffect(() => {
    if (
      (!params.get("cluster") && first) ||
      (legacyPage && params.has("page"))
    ) {
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous)
          if (!next.get("cluster") && first) next.set("cluster", first)
          if (legacyPage && next.has("page")) {
            if (!next.has("clusterPage")) next.set("clusterPage", String(page))
            next.delete("page")
          }
          return next
        },
        { replace: true }
      )
    }
  }, [params, first, legacyPage, page, setParams])
  const selected = useQuery({
    queryKey: ["cluster-metadata", id],
    enabled: !!id && !items.some((v) => v.id === id),
    queryFn: async ({ signal }) =>
      clusterSchema.parse(
        await api(`/clusters/${encodeURIComponent(id)}`, { signal })
      ),
    retry: false,
  })
  const current =
    items.find((v) => v.id === id) ||
    (!selected.isError ? selected.data : undefined)
  const options = [
    ...(id && !items.some((v) => v.id === id)
      ? [{ id, name: current?.name || "所选集群" }]
      : []),
    ...items,
  ]
  return { id, page, search, current, options, inventory }
}
const schema = z.object({ q: z.string().trim().max(128, "最多 128 个字符") })
export function ClusterPicker({
  selection,
  label,
  onSwitch,
}: {
  selection: ReturnType<typeof useAccessibleClusters>
  label: string
  onSwitch?: () => void
}) {
  const [, setParams] = useSearchParams(),
    { id, page, search, options, inventory } = selection
  const {
    register,
    reset,
    handleSubmit,
    formState: { errors },
  } = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { q: search },
  })
  useEffect(() => {
    reset({ q: search })
  }, [search, reset])
  const browse = (nextPage: number, nextSearch = search) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set("clusterPage", String(nextPage))
      if (id) next.set("cluster", id)
      if (nextSearch) next.set("clusterQ", nextSearch)
      else next.delete("clusterQ")
      return next
    })
  return (
    <section className="panel space-y-3 p-4" aria-label={`${label}集群选择`}>
      <div className="flex flex-wrap items-center gap-3">
        <Database className="size-4 text-muted-foreground" />
        <Select
          value={id}
          disabled={!options.length}
          onValueChange={(value) => {
            onSwitch?.()
            setParams({
              cluster: value,
              clusterPage: String(page),
              ...(search ? { clusterQ: search } : {}),
            })
          }}
        >
          <SelectTrigger
            aria-label={`选择${label}集群`}
            className="w-full sm:w-64"
          >
            <SelectValue placeholder="选择集群" />
          </SelectTrigger>
          <SelectContent>
            {options.map((v) => (
              <SelectItem key={v.id} value={v.id}>
                {v.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="text-2xs text-muted-foreground">
          第 {page} 页 ·{" "}
          {inventory.isError ? "—" : (inventory.data?.totalItems ?? "—")}{" "}
          个匹配集群
        </span>
        <Button
          size="icon"
          variant="ghost"
          aria-label={`上一组${label}集群`}
          disabled={page <= 1 || inventory.isFetching}
          onClick={() => browse(page - 1)}
        >
          <ChevronLeft />
        </Button>
        <Button
          size="icon"
          variant="ghost"
          aria-label={`下一组${label}集群`}
          disabled={
            !inventory.data ||
            page * 20 >= inventory.data.totalItems ||
            inventory.isFetching
          }
          onClick={() => browse(page + 1)}
        >
          <ChevronRight />
        </Button>
      </div>
      <form
        className="flex flex-wrap items-center gap-2"
        noValidate
        onSubmit={handleSubmit((v) => browse(1, v.q))}
      >
        <Input
          aria-label={`搜索${label}可选集群`}
          placeholder="搜索可选集群名称、标识或描述"
          autoComplete="off"
          className="w-full sm:max-w-80"
          {...register("q")}
          aria-invalid={!!errors.q}
        />
        <Button size="sm" variant="outline" type="submit">
          <Search />
          搜索集群
        </Button>
        {search && (
          <Button
            size="sm"
            variant="ghost"
            type="button"
            onClick={() => {
              reset({ q: "" })
              browse(1, "")
            }}
          >
            清除集群搜索
          </Button>
        )}
        <span className="text-2xs text-muted-foreground">
          浏览候选列表不会切换当前集群；选择后才切换。
        </span>
        {errors.q && (
          <p role="alert" className="text-2xs text-destructive">
            {errors.q.message}
          </p>
        )}
      </form>
    </section>
  )
}
