import { useMemo } from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { ArrowDown, ArrowUpDown } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "@/components/ui/table"
import { formatNumber, type QOS, type Fairshare } from "./schema"
type Item = QOS | Fairshare
export function PolicyTable({
  kind,
  items,
  sortKey,
  order,
  onSort,
  onOpen,
}: {
  kind: "qos" | "fairshare"
  items: Item[]
  sortKey: string
  order: string
  onSort: (key: string) => void
  onOpen: (id: string) => void
}) {
  const columns = useMemo<ColumnDef<Item>[]>(() => {
    const heading = (key: string, label: string) => (
      <Button variant="ghost" size="sm" onClick={() => onSort(key)}>
        {label}
        {sortKey === key ? (
          <ArrowDown
            className={`size-3 ${order === "asc" ? "rotate-180" : ""}`}
          />
        ) : (
          <ArrowUpDown className="size-3" />
        )}
      </Button>
    )
    if (kind === "qos")
      return [
        {
          id: "name",
          header: () => heading("name", "QOS"),
          cell: ({ row }) => (
            <Button
              variant="link"
              className="px-0 font-mono"
              onClick={() => onOpen((row.original as QOS).name)}
            >
              {(row.original as QOS).name}
            </Button>
          ),
        },
        {
          id: "description",
          header: "描述",
          cell: ({ row }) => (
            <span className="block max-w-64 whitespace-normal">
              {(row.original as QOS).description || "—"}
            </span>
          ),
        },
        {
          id: "priority",
          header: () => heading("priority", "优先级"),
          cell: ({ row }) => formatNumber((row.original as QOS).priority),
        },
        {
          id: "usageFactor",
          header: "计费用量因子",
          cell: ({ row }) => formatNumber((row.original as QOS).usageFactor),
        },
        {
          id: "maxJobsPerUser",
          header: "运行 / 用户",
          cell: ({ row }) => formatNumber((row.original as QOS).maxJobsPerUser),
        },
        {
          id: "maxSubmitJobsPerUser",
          header: "提交 / 用户",
          cell: ({ row }) =>
            formatNumber((row.original as QOS).maxSubmitJobsPerUser),
        },
        {
          id: "maxWallMinutes",
          header: "时限（分钟）",
          cell: ({ row }) => formatNumber((row.original as QOS).maxWallMinutes),
        },
      ]
    return [
      {
        id: "account",
        header: () => heading("account", "Account"),
        cell: ({ row }) => (
          <Button
            variant="link"
            className="px-0 font-mono"
            onClick={() => onOpen((row.original as Fairshare).id)}
          >
            {(row.original as Fairshare).account}
          </Button>
        ),
      },
      {
        id: "user",
        header: () => heading("user", "Slurm 用户"),
        cell: ({ row }) => (
          <span className="font-mono">
            {(row.original as Fairshare).user || "Account 层级"}
          </span>
        ),
      },
      {
        id: "shares",
        header: "权重",
        cell: ({ row }) => formatNumber((row.original as Fairshare).shares),
      },
      {
        id: "normalizedShares",
        header: "规范份额",
        cell: ({ row }) =>
          formatNumber((row.original as Fairshare).normalizedShares),
      },
      {
        id: "usage",
        header: "累计用量",
        cell: ({ row }) => (
          <span className="font-mono">
            {BigInt((row.original as Fairshare).rawUsage).toLocaleString(
              "zh-CN"
            )}
          </span>
        ),
      },
      {
        id: "factor",
        header: () => heading("factor", "Fairshare 因子"),
        cell: ({ row }) => formatNumber((row.original as Fairshare).factor),
      },
      {
        id: "partition",
        header: "分区",
        cell: ({ row }) => (row.original as Fairshare).partition || "通用关联",
      },
    ]
  }, [kind, onOpen, onSort, order, sortKey])
  // TanStack Table returns functions that React Compiler cannot memoize safely.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: items,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualSorting: true,
    getRowId: (row) => (row as QOS).name || (row as Fairshare).id,
  })
  return (
    <Table aria-label={kind === "qos" ? "QOS 策略" : "Fairshare 份额"}>
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
              className="h-32 text-center text-muted-foreground"
            >
              没有匹配的{kind === "qos" ? "可见 QOS" : "Fairshare 记录"}。
            </TableCell>
          </TableRow>
        )}
      </TableBody>
    </Table>
  )
}
