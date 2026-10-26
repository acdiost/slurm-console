import { useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Search, X } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select"
const text = z.string().trim().max(128, "最多 128 个字符")
const schema = z.object({
  q: text,
  partition: z.string().trim().max(255, "最多 255 个字符"),
  state: text,
})
export type Filters = z.infer<typeof schema>
const states = [
  "IDLE",
  "ALLOCATED",
  "MIXED",
  "DRAIN",
  "DOWN",
  "NOT_RESPONDING",
  "COMPLETING",
  "MAINTENANCE",
  "POWERED_DOWN",
  "UNKNOWN",
]
export function NodeFilters({
  initial,
  onApply,
}: {
  initial: Filters
  onApply: (values: Filters) => void
}) {
  const {
    register,
    control,
    setValue,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<Filters>({
    resolver: zodResolver(schema),
    defaultValues: initial,
  })
  const state = useWatch({ control, name: "state" })
  return (
    <form
      onSubmit={handleSubmit(onApply)}
      noValidate
      className="panel space-y-4 p-4"
    >
      <div className="grid gap-3 sm:grid-cols-3">
        {(
          [
            ["q", "搜索节点", "名称或主机名"],
            ["partition", "分区", "精确分区"],
          ] as const
        ).map(([key, label, placeholder]) => (
          <div key={key} className="space-y-2">
            <Label htmlFor={`nodes-${key}`}>{label}</Label>
            <Input
              id={`nodes-${key}`}
              autoComplete="off"
              placeholder={placeholder}
              aria-invalid={!!errors[key]}
              {...register(key)}
            />
            {errors[key] && (
              <p role="alert" className="text-2xs text-destructive">
                {errors[key].message}
              </p>
            )}
          </div>
        ))}
        <div className="space-y-2">
          <Label htmlFor="nodes-state">状态标记</Label>
          <Select
            value={state || "any"}
            onValueChange={(value) =>
              setValue("state", value === "any" ? "" : value)
            }
          >
            <SelectTrigger id="nodes-state" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="any">全部状态</SelectItem>
              {[
                ...states,
                ...(state && !states.includes(state) ? [state] : []),
              ].map((code) => (
                <SelectItem key={code} value={code}>
                  {code}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-2xs text-muted-foreground">
          服务端筛选与分页 · 多重状态可按任一标记筛选
        </p>
        <div className="flex gap-2">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => {
              reset({ q: "", partition: "", state: "" })
              onApply({ q: "", partition: "", state: "" })
            }}
          >
            <X />
            清除筛选
          </Button>
          <Button type="submit" size="sm">
            <Search />
            应用筛选
          </Button>
        </div>
      </div>
    </form>
  )
}
