import {
  createContext,
  useContext,
  useEffect,
  useId,
  useMemo,
  useState,
  type ReactNode,
} from "react"
import { createPortal } from "react-dom"
import { cn } from "cn"
import { useLocation, useSearchParams } from "react-router"
import { useQuery, type UseQueryResult } from "@tanstack/react-query"
import { useForm, type UseFormReturn } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import {
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Database,
  LoaderCircle,
  Search,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { clusterPageSchema, clusterSchema } from "@/features/overview/schema"
import { api } from "@/services/api"

const ClusterPickerNavigationContext = createContext<{
  target: HTMLDivElement | null
  setTarget: (target: HTMLDivElement | null) => void
} | null>(null)

export function ClusterPickerNavigationProvider({
  children,
}: {
  children: ReactNode
}) {
  const [target, setTarget] = useState<HTMLDivElement | null>(null)
  const navigation = useMemo(() => ({ target, setTarget }), [target])
  return (
    <ClusterPickerNavigationContext.Provider value={navigation}>
      {children}
    </ClusterPickerNavigationContext.Provider>
  )
}

export function ClusterPickerNavigationTarget({
  className,
}: {
  className?: string
}) {
  const navigation = useContext(ClusterPickerNavigationContext)
  return (
    <div
      ref={navigation?.setTarget}
      className={cn("min-w-0 empty:hidden", className)}
    />
  )
}

export function pageNumber(value: string | null, fallback = 1, max = 1000000) {
  const n = Number(value ?? fallback)
  return Number.isInteger(n) && n >= 1 && n <= max ? n : fallback
}

export type AccessibleClusters = {
  id: string
  page: number
  search: string
  current: z.infer<typeof clusterSchema> | undefined
  options: { id: string; name: string }[]
  inventory: UseQueryResult<z.infer<typeof clusterPageSchema>, Error>
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

type ClusterPickerProps = {
  selection: AccessibleClusters
  label: string
  onSwitch?: () => void
  placement?: "navigation" | "inline"
}

type ClusterPickerController = AccessibleClusters & {
  form: UseFormReturn<z.infer<typeof schema>>
  browse: (page: number, search?: string) => void
  switchCluster: (id: string) => void
}

export function ClusterPicker({
  placement = "navigation",
  ...props
}: ClusterPickerProps) {
  const navigation = useContext(ClusterPickerNavigationContext)
  const { pathname } = useLocation()
  if (placement === "inline") return <InlineClusterPicker {...props} />
  if (!navigation?.target) return null
  return createPortal(
    <NavigationClusterPicker key={pathname} {...props} />,
    navigation.target
  )
}

function useClusterPicker({ selection, onSwitch }: ClusterPickerProps) {
  const [, setParams] = useSearchParams()
  const { id, page, search } = selection
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { q: search },
  })
  const { reset } = form
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
  const switchCluster = (value: string) => {
    if (value === id) return
    onSwitch?.()
    setParams({
      cluster: value,
      clusterPage: String(page),
      ...(search ? { clusterQ: search } : {}),
    })
  }
  return { ...selection, form, browse, switchCluster }
}

type PickerControlsProps = {
  picker: ClusterPickerController
  label: string
}

function ClusterPickerSearch({ picker, label }: PickerControlsProps) {
  const errorId = useId()
  const { form, browse, search } = picker
  const {
    register,
    reset,
    handleSubmit,
    formState: { errors },
  } = form
  return (
    <form
      className="flex flex-wrap items-center gap-2"
      noValidate
      onSubmit={handleSubmit((v) => browse(1, v.q))}
    >
      <Input
        aria-label={`搜索${label}可选集群`}
        placeholder="搜索名称、标识或描述"
        autoComplete="off"
        className="min-w-0 flex-1"
        {...register("q")}
        aria-invalid={!!errors.q}
        aria-describedby={errors.q ? errorId : undefined}
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
      <p className="w-full text-2xs text-muted-foreground">
        浏览候选列表不会切换当前集群；选择后才切换。
      </p>
      {errors.q && (
        <p id={errorId} role="alert" className="text-2xs text-destructive">
          {errors.q.message}
        </p>
      )}
    </form>
  )
}

function ClusterPickerPagination({ picker, label }: PickerControlsProps) {
  const { page, inventory, browse } = picker
  return (
    <div className="flex items-center gap-1">
      <span className="mr-auto text-2xs text-muted-foreground">
        第 {page} 页 ·{" "}
        {inventory.isError ? "—" : (inventory.data?.totalItems ?? "—")}{" "}
        个匹配集群
      </span>
      <Button
        type="button"
        size="icon"
        variant="ghost"
        aria-label={`上一组${label}集群`}
        disabled={page <= 1 || inventory.isFetching}
        onClick={() => browse(page - 1)}
      >
        <ChevronLeft />
      </Button>
      <Button
        type="button"
        size="icon"
        variant="ghost"
        aria-label={`下一组${label}集群`}
        disabled={
          inventory.isError ||
          !inventory.data ||
          page * 20 >= inventory.data.totalItems ||
          inventory.isFetching
        }
        onClick={() => browse(page + 1)}
      >
        <ChevronRight />
      </Button>
    </div>
  )
}

function ClusterPickerStatus({ picker }: Pick<PickerControlsProps, "picker">) {
  const { inventory, search } = picker
  if (inventory.isError) {
    return (
      <div className="flex items-center justify-between gap-2 rounded-md bg-destructive/5 p-3">
        <p role="alert" className="text-xs text-destructive">
          无法读取可选集群
        </p>
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={inventory.isFetching}
          onClick={() => void inventory.refetch()}
        >
          {inventory.isFetching ? "正在重试…" : "重试"}
        </Button>
      </div>
    )
  }
  if (inventory.isFetching) {
    return (
      <p
        role="status"
        className="flex items-center gap-2 p-3 text-xs text-muted-foreground"
      >
        <LoaderCircle className="size-3.5 animate-spin" aria-hidden="true" />
        {inventory.isPending ? "正在读取集群…" : "正在更新集群…"}
      </p>
    )
  }
  if (!inventory.data?.items.length) {
    return (
      <p role="status" className="p-3 text-xs text-muted-foreground">
        {search ? "没有匹配的集群" : "暂无可见集群"}
      </p>
    )
  }
  return null
}

function NavigationClusterPicker(props: ClusterPickerProps) {
  const picker = useClusterPicker(props)
  const { label } = props
  const { id, current, options, inventory, switchCluster } = picker
  const [open, setOpen] = useState(false)
  const [focusedId, setFocusedId] = useState(id)
  const candidates = inventory.isError ? [] : (inventory.data?.items ?? [])
  const tabStop = candidates.some((v) => v.id === focusedId)
    ? focusedId
    : candidates.some((v) => v.id === id)
      ? id
      : candidates[0]?.id
  const currentName =
    current?.name ||
    options.find((v) => v.id === id)?.name ||
    (inventory.isPending
      ? "正在读取集群…"
      : inventory.isError
        ? "集群暂不可用"
        : "选择集群")
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          className="w-28 max-w-full min-w-0 gap-1.5 sm:w-44 lg:w-52"
          aria-label={`选择${label}集群：${currentName}`}
          title={currentName}
        >
          <Database
            className="size-3.5 text-muted-foreground"
            aria-hidden="true"
          />
          <span className="min-w-0 flex-1 truncate text-left">
            {currentName}
          </span>
          <ChevronDown
            className="size-3.5 text-muted-foreground"
            aria-hidden="true"
          />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        aria-label={`${label}集群选择`}
        className="w-96 space-y-3"
      >
        <p className="text-xs font-medium">选择{label}集群</p>
        <ClusterPickerSearch picker={picker} label={label} />
        <ClusterPickerStatus picker={picker} />
        {candidates.length > 0 && (
          <div
            role="listbox"
            aria-label={`${label}可选集群`}
            aria-busy={inventory.isFetching}
            className="max-h-64 space-y-1 overflow-y-auto"
            onKeyDown={(event) => {
              if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key))
                return
              const buttons = Array.from(
                event.currentTarget.querySelectorAll<HTMLButtonElement>(
                  '[role="option"]'
                )
              )
              const index = buttons.indexOf(event.target as HTMLButtonElement)
              if (index < 0) return
              event.preventDefault()
              const next =
                event.key === "Home"
                  ? 0
                  : event.key === "End"
                    ? buttons.length - 1
                    : (index +
                        (event.key === "ArrowDown" ? 1 : -1) +
                        buttons.length) %
                      buttons.length
              buttons[next]?.focus()
            }}
          >
            {candidates.map((candidate) => (
              <button
                key={candidate.id}
                type="button"
                role="option"
                aria-selected={candidate.id === id}
                tabIndex={candidate.id === tabStop ? 0 : -1}
                className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-xs outline-none hover:bg-accent focus-visible:bg-accent focus-visible:ring-2 focus-visible:ring-ring/30 aria-selected:bg-primary/10 aria-selected:text-primary"
                onFocus={() => setFocusedId(candidate.id)}
                onClick={() => {
                  setOpen(false)
                  switchCluster(candidate.id)
                }}
              >
                <span className="min-w-0 flex-1 break-words">
                  {candidate.name}
                </span>
                {candidate.id === id && (
                  <Check className="size-3.5 shrink-0" aria-hidden="true" />
                )}
              </button>
            ))}
          </div>
        )}
        <div className="border-t pt-2">
          <ClusterPickerPagination picker={picker} label={label} />
        </div>
      </PopoverContent>
    </Popover>
  )
}

function InlineClusterPicker(props: ClusterPickerProps) {
  const picker = useClusterPicker(props)
  const { label } = props
  const { id, options, switchCluster } = picker
  return (
    <section className="panel space-y-3 p-4" aria-label={`${label}集群选择`}>
      <div className="flex flex-wrap items-center gap-3">
        <Database className="size-4 text-muted-foreground" aria-hidden="true" />
        <Select
          value={id}
          disabled={!options.length}
          onValueChange={switchCluster}
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
      </div>
      <ClusterPickerSearch picker={picker} label={label} />
      <ClusterPickerStatus picker={picker} />
      <ClusterPickerPagination picker={picker} label={label} />
    </section>
  )
}
