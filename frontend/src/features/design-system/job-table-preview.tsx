import { useState } from "react"
import {
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState,
} from "@tanstack/react-table"
import { ArrowDownUp, ListFilter, Search, Table2 } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { usePreferences, type Density } from "@/stores/preferences"
import { demoJobs, type DemoJob } from "./demo-jobs"

const columns: ColumnDef<DemoJob>[] = [
  {
    accessorKey: "id",
    header: "Job ID",
    cell: ({ getValue }) => (
      <span className="font-mono text-primary">#{getValue<string>()}</span>
    ),
  },
  {
    accessorKey: "name",
    header: "作业名称",
    cell: ({ getValue }) => (
      <span className="font-medium">{getValue<string>()}</span>
    ),
  },
  { accessorKey: "user", header: "用户" },
  {
    accessorKey: "partition",
    header: "分区",
    cell: ({ getValue }) => (
      <span className="font-mono text-caption text-muted-foreground">
        {getValue<string>()}
      </span>
    ),
  },
  {
    accessorKey: "state",
    header: "状态",
    cell: ({ getValue }) => <SlurmStatusBadge state={getValue<string>()} />,
  },
  {
    accessorKey: "cpus",
    header: "CPU",
    cell: ({ getValue }) => (
      <span className="font-mono tabular-nums">{getValue<number>()}</span>
    ),
  },
  {
    accessorKey: "duration",
    header: "运行时长",
    cell: ({ getValue }) => (
      <span className="font-mono text-caption text-muted-foreground tabular-nums">
        {getValue<string>()}
      </span>
    ),
  },
]

export function JobTablePreview() {
  const [filter, setFilter] = useState("")
  const [sorting, setSorting] = useState<SortingState>([])
  const { density, setDensity } = usePreferences()
  // TanStack Table 的实例包含函数，不能由 React Compiler 自动记忆化。
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: demoJobs,
    columns,
    state: { globalFilter: filter, sorting },
    onGlobalFilterChange: setFilter,
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })
  return (
    <section className="panel min-w-0">
      <div className="panel-heading">
        <div>
          <div className="section-eyebrow">DATA DISPLAY</div>
          <h2>高密度数据表格</h2>
        </div>
        <span className="component-tag">TanStack Table</span>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3">
        <div className="relative w-full sm:w-65">
          <Search className="pointer-events-none absolute top-2 left-2.5 size-3.5 text-muted-foreground" />
          <Input
            aria-label="搜索示例作业"
            placeholder="搜索作业、用户、分区…"
            className="pl-8"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
        </div>
        <div className="flex items-center gap-2">
          <ListFilter className="size-3.5 text-muted-foreground" />
          <span className="mr-1 text-caption text-muted-foreground">
            行密度
          </span>
          <ToggleGroup
            type="single"
            value={density}
            onValueChange={(value) => {
              if (value) setDensity(value as Density)
            }}
            variant="outline"
            size="sm"
            aria-label="表格行密度"
          >
            <ToggleGroupItem value="standard" aria-label="标准密度">
              标准
            </ToggleGroupItem>
            <ToggleGroupItem value="compact" aria-label="紧凑密度">
              紧凑
            </ToggleGroupItem>
          </ToggleGroup>
        </div>
      </div>
      <Table data-density={density} className="demo-table">
        <caption className="sr-only">
          固定示例作业，支持本地搜索与排序，不代表真实集群数据
        </caption>
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id}>
              {group.headers.map((header) => (
                <TableHead
                  key={header.id}
                  scope="col"
                  aria-sort={
                    header.column.getIsSorted() === "asc"
                      ? "ascending"
                      : header.column.getIsSorted() === "desc"
                        ? "descending"
                        : "none"
                  }
                >
                  <Button
                    variant="ghost"
                    size="sm"
                    className="-ml-2 text-caption text-muted-foreground"
                    onClick={header.column.getToggleSortingHandler()}
                    aria-label={`按${header.column.columnDef.header as string}排序`}
                  >
                    {flexRender(
                      header.column.columnDef.header,
                      header.getContext()
                    )}
                    <ArrowDownUp className="size-3" />
                  </Button>
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows.map((row) => (
            <TableRow key={row.id}>
              {row.getVisibleCells().map((cell) => (
                <TableCell key={cell.id}>
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </TableCell>
              ))}
            </TableRow>
          ))}
          {!table.getRowModel().rows.length && (
            <TableRow>
              <TableCell colSpan={columns.length} className="h-36 text-center">
                <Search className="mx-auto mb-2 size-5 text-muted-foreground" />
                <p>没有匹配的示例作业</p>
                <Button variant="link" onClick={() => setFilter("")}>
                  清除搜索
                </Button>
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <div className="flex flex-wrap items-center justify-between gap-2 border-t px-5 py-3 text-2xs text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <Table2 className="size-3" />
          显示 {table.getRowModel().rows.length} / {demoJobs.length} 条示例
        </span>
        <span>正式业务采用服务端分页与筛选</span>
      </div>
    </section>
  )
}
