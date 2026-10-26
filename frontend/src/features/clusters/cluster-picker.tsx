import { useSearchParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ChevronLeft, ChevronRight, Database } from "lucide-react"
import { Button } from "@/components/ui/button"
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
export function useAccessibleClusters() {
  const [params] = useSearchParams(),
    page = pageNumber(params.get("clusterPage"))
  const inventory = useQuery({
    queryKey: ["accessible-clusters", page],
    queryFn: async ({ signal }) =>
      clusterPageSchema.parse(
        await api(`/clusters?page=${page}&perPage=20`, { signal })
      ),
    retry: false,
  })
  const items = inventory.isError ? [] : (inventory.data?.items ?? [])
  const id = params.get("cluster") || items[0]?.id || ""
  const selected = useQuery({
    queryKey: ["cluster-metadata", id],
    enabled: !!id && !items.some((value) => value.id === id),
    queryFn: async ({ signal }) =>
      clusterSchema.parse(
        await api(`/clusters/${encodeURIComponent(id)}`, { signal })
      ),
    retry: false,
  })
  const current =
    items.find((value) => value.id === id) ||
    (!selected.isError ? selected.data : undefined)
  const options = [
    ...(id && !items.some((value) => value.id === id)
      ? [{ id, name: current?.name || "所选集群" }]
      : []),
    ...items,
  ]
  return { id, page, current, options, inventory }
}
export function ClusterPicker({
  selection,
  label,
}: {
  selection: ReturnType<typeof useAccessibleClusters>
  label: string
}) {
  const [, setParams] = useSearchParams(),
    { id, page, options, inventory } = selection
  return (
    <section
      className="panel flex flex-wrap items-center gap-3 px-4 py-3"
      aria-label={`${label}集群选择`}
    >
      <Database className="size-4 text-muted-foreground" />
      <Select
        value={id}
        disabled={!options.length}
        onValueChange={(value) =>
          setParams({ cluster: value, clusterPage: String(page) })
        }
      >
        <SelectTrigger
          aria-label={`选择${label}集群`}
          className="w-full sm:w-64"
        >
          <SelectValue placeholder="选择集群" />
        </SelectTrigger>
        <SelectContent>
          {options.map((value) => (
            <SelectItem key={value.id} value={value.id}>
              {value.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <span className="text-2xs text-muted-foreground">
        第 {page} 页 ·{" "}
        {inventory.isError ? "—" : (inventory.data?.totalItems ?? "—")}{" "}
        个可见集群
      </span>
      <Button
        size="icon"
        variant="ghost"
        aria-label={`上一组${label}集群`}
        disabled={page <= 1 || inventory.isFetching}
        onClick={() => setParams({ clusterPage: String(page - 1) })}
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
        onClick={() => setParams({ clusterPage: String(page + 1) })}
      >
        <ChevronRight />
      </Button>
    </section>
  )
}
