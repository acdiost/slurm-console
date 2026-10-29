import { useMemo } from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableCell,
  TableRow,
} from "@/components/ui/table"
import type { Account, SlurmUser, Association, Kind } from "./schema"
type Item = Account | SlurmUser | Association
export function AccountingTable({
  kind,
  items,
  sortKey,
  order,
  onSort,
  onOpen,
}: {
  kind: Kind
  items: Item[]
  sortKey: string
  order: string
  onSort: (key: string) => void
  onOpen: (id: string) => void
}) {
  const columns = useMemo<ColumnDef<Item>[]>(() => {
    const heading = (id: string, label: string) => (
      <Button variant="ghost" size="sm" onClick={() => onSort(id)}>
        {label}
        {id === sortKey ? (
          order === "desc" ? (
            <ArrowDown className="size-3" />
          ) : (
            <ArrowUp className="size-3" />
          )
        ) : (
          <ArrowUpDown className="size-3" />
        )}
      </Button>
    )
    const name: ColumnDef<Item> = {
      id: "name",
      header: () => heading("name", "名称"),
      cell: ({ row }) => {
        const v = row.original as Account | SlurmUser
        return (
          <Button
            variant="link"
            className="px-0 font-mono"
            onClick={() => onOpen(v.name)}
          >
            {v.name}
          </Button>
        )
      },
    }
    if (kind === "accounts")
      return [
        name,
        {
          id: "description",
          header: "描述",
          cell: ({ row }) => (
            <span className="block max-w-80 whitespace-normal">
              {(row.original as Account).description || "—"}
            </span>
          ),
        },
        {
          id: "organization",
          header: "组织",
          cell: ({ row }) => (row.original as Account).organization || "—",
        },
        {
          id: "count",
          header: "关联 / 用户",
          cell: ({ row }) => {
            const a = row.original as Account
            return (
              <span className="font-mono">
                {a.associationCount} / {a.userCount}
              </span>
            )
          },
        },
      ]
    if (kind === "slurm-users")
      return [
        name,
        {
          id: "defaultAccount",
          header: "默认 Account",
          cell: ({ row }) => {
            const u = row.original as SlurmUser
            return u.defaultAccountScope === "outside_cluster"
              ? "其他集群（已隐藏）"
              : u.defaultAccount || "未设置"
          },
        },
        {
          id: "level",
          header: "记账管理级别",
          cell: ({ row }) =>
            (row.original as SlurmUser).administratorLevel.join(" / ") ||
            "未设置",
        },
        {
          id: "count",
          header: "当前集群关联",
          cell: ({ row }) => (row.original as SlurmUser).associationCount,
        },
      ]
    return [
      {
        id: "id",
        header: () => heading("id", "ID"),
        cell: ({ row }) => {
          const a = row.original as Association
          return (
            <Button
              variant="link"
              className="px-0 font-mono"
              onClick={() => onOpen(a.id)}
            >
              #{a.id}
            </Button>
          )
        },
      },
      {
        id: "account",
        header: () => heading("account", "Account"),
        cell: ({ row }) => (
          <span className="font-mono">
            {(row.original as Association).account}
          </span>
        ),
      },
      {
        id: "user",
        header: () => heading("user", "Slurm 用户"),
        cell: ({ row }) => (
          <span className="font-mono">
            {(row.original as Association).user || "Account 层级"}
          </span>
        ),
      },
      {
        id: "partition",
        header: () => heading("partition", "分区"),
        cell: ({ row }) => (row.original as Association).partition || "通用",
      },
      {
        id: "default",
        header: "层级 / 默认",
        cell: ({ row }) => {
          const a = row.original as Association
          return a.isDefault ? (
            <Badge variant="secondary">默认</Badge>
          ) : (
            a.parentAccount || "—"
          )
        },
      },
    ]
  }, [kind, onSort, onOpen, sortKey, order])
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: items,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualSorting: true,
    autoResetAll: false,
    getRowId: (row) => ("id" in row ? row.id : row.name),
  })
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id}>
              {group.headers.map((header) => (
                <TableHead
                  key={header.id}
                  aria-sort={
                    header.id === sortKey
                      ? order === "desc"
                        ? "descending"
                        : "ascending"
                      : undefined
                  }
                >
                  {flexRender(
                    header.column.columnDef.header,
                    header.getContext()
                  )}
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
          {!items.length && (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className="h-40 text-center text-muted-foreground"
              >
                没有匹配的记账记录。仅展示关联当前集群的身份。
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  )
}
