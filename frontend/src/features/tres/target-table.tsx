import { useMemo } from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { ArrowUpDown } from "lucide-react"
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "@/components/ui/table"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { protectedTarget, type Target } from "./schema"
export function TRESTargetTable({
  items,
  onOpen,
  onSort,
}: {
  items: Target[]
  onOpen: (id: string) => void
  onSort: (key: string) => void
}) {
  const columns = useMemo<ColumnDef<Target>[]>(
    () => [
      {
        id: "id",
        header: () => (
          <Button variant="ghost" size="sm" onClick={() => onSort("id")}>
            ID
            <ArrowUpDown className="size-3" />
          </Button>
        ),
        cell: ({ row }) => (
          <Button
            variant="link"
            className="px-0 font-mono"
            onClick={() => onOpen(row.original.id)}
          >
            #{row.original.id}
          </Button>
        ),
      },
      {
        id: "name",
        header: () => (
          <Button
            variant="ghost"
            size="sm"
            onClick={() =>
              onSort(items[0]?.kind === "qos" ? "name" : "account")
            }
          >
            对象
            <ArrowUpDown className="size-3" />
          </Button>
        ),
        cell: ({ row }) => (
          <span className="font-mono">
            {row.original.name || row.original.account}
          </span>
        ),
      },
      {
        id: "user",
        header: "Slurm 用户 / 层级",
        cell: ({ row }) =>
          row.original.kind === "qos"
            ? "共享 QOS"
            : row.original.user || "Account 层级",
      },
      {
        id: "partition",
        header: "分区",
        cell: ({ row }) => row.original.partition || "—",
      },
      {
        id: "count",
        header: "本层限额项",
        cell: ({ row }) =>
          Object.values(row.original.limits).reduce(
            (sum, rows) => sum + rows.length,
            0
          ),
      },
      {
        id: "scope",
        header: "类型",
        cell: ({ row }) => (
          <div className="flex gap-1">
            {row.original.relative && <Badge variant="outline">Relative</Badge>}
            {protectedTarget(row.original) ? (
              <Badge variant="outline">系统关联</Badge>
            ) : (
              <span className="text-muted-foreground">
                {row.original.kind === "qos" ? "跨集群策略" : "集群关联"}
              </span>
            )}
          </div>
        ),
      },
    ],
    [items, onOpen, onSort]
  )
  // TanStack Table returns callbacks that React Compiler cannot safely memoize.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: items,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualSorting: true,
    getRowId: (r) => r.id,
  })
  return (
    <Table aria-label="TRES 限额对象">
      <TableHeader>
        {table.getHeaderGroups().map((g) => (
          <TableRow key={g.id}>
            {g.headers.map((h) => (
              <TableHead key={h.id}>
                {flexRender(h.column.columnDef.header, h.getContext())}
              </TableHead>
            ))}
          </TableRow>
        ))}
      </TableHeader>
      <TableBody>
        {table.getRowModel().rows.map((r) => (
          <TableRow key={r.id}>
            {r.getVisibleCells().map((c) => (
              <TableCell key={c.id}>
                {flexRender(c.column.columnDef.cell, c.getContext())}
              </TableCell>
            ))}
          </TableRow>
        ))}
        {!items.length && (
          <TableRow>
            <TableCell
              colSpan={columns.length}
              className="h-32 text-center text-muted-foreground"
            >
              没有匹配的限额对象。
            </TableCell>
          </TableRow>
        )}
      </TableBody>
    </Table>
  )
}
