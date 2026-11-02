import { useState } from "react"
import { Checkbox } from "@/components/ui/checkbox"
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
import { labels, display, type DiffField } from "./schema"
export function ConfigurationDiff({ fields }: { fields: DiffField[] }) {
  const [onlyChanged, setOnlyChanged] = useState(true),
    changed = fields.filter((f) => f.changed).length
  return (
    <section className="space-y-3" aria-label="配置字段对比">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm font-medium">{changed} 个配置字段有变化</p>
        <Label className="flex gap-2 text-sm">
          <Checkbox
            checked={onlyChanged}
            onCheckedChange={(v) => setOnlyChanged(v === true)}
          />
          仅显示变化字段
        </Label>
      </div>
      {changed === 0 && (
        <p
          role="status"
          className="rounded-md border p-4 text-sm text-muted-foreground"
        >
          配置字段相同；版本编号、记录时间或操作人可能不同。
        </p>
      )}
      {(!onlyChanged || changed > 0) && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>字段</TableHead>
              <TableHead>基准 / 保存前</TableHead>
              <TableHead>比较 / 保存后</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {fields
              .filter((f) => !onlyChanged || f.changed)
              .map((f) => (
                <TableRow key={f.field}>
                  <TableCell className="min-w-28 align-top font-medium">
                    {labels[f.field]}{" "}
                    {f.changed && <Badge variant="outline">变化</Badge>}
                  </TableCell>
                  <TableCell className="max-w-96 align-top break-all whitespace-pre-wrap">
                    {display(f.field, f.before)}
                  </TableCell>
                  <TableCell className="max-w-96 align-top break-all whitespace-pre-wrap">
                    {display(f.field, f.after)}
                  </TableCell>
                </TableRow>
              ))}
          </TableBody>
        </Table>
      )}
    </section>
  )
}
