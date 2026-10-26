import { useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Search, X } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
const text = z.string().trim().max(128, "最多 128 个字符")
const schema = z.object({
  q: text,
  user: text,
  account: text,
  partition: z.string().trim().max(255, "最多 255 个字符"),
  qos: text,
  state: text,
})
export type Filters = z.infer<typeof schema>
export const emptyFilters: Filters = {
  q: "",
  user: "",
  account: "",
  partition: "",
  qos: "",
  state: "",
}
export function JobFilters({
  initial,
  onApply,
}: {
  initial: Filters
  onApply: (value: Filters) => void
}) {
  const {
    register,
    control,
    setValue,
    reset,
    handleSubmit,
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
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-6">
        {(
          [
            ["q", "搜索作业", "Job ID 或名称"],
            ["user", "用户", "精确用户名"],
            ["account", "账户", "精确 Account"],
            ["partition", "分区", "精确分区"],
            ["qos", "QOS", "精确 QOS"],
          ] as const
        ).map(([key, label, placeholder]) => (
          <div key={key} className="space-y-2">
            <Label htmlFor={`jobs-${key}`}>{label}</Label>
            <Input
              id={`jobs-${key}`}
              placeholder={placeholder}
              autoComplete="off"
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
          <Label htmlFor="jobs-state">状态</Label>
          <Select
            value={state || "any"}
            onValueChange={(value) =>
              setValue("state", value === "any" ? "" : value)
            }
          >
            <SelectTrigger id="jobs-state" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {[
                ["any", "全部状态"],
                ["RUNNING", "RUNNING"],
                ["PENDING", "PENDING"],
                ["COMPLETING", "COMPLETING"],
                ["COMPLETED", "COMPLETED"],
                ["CANCELLED", "CANCELLED"],
                ["FAILED", "FAILED"],
                ["TIMEOUT", "TIMEOUT"],
                ["SUSPENDED", "SUSPENDED"],
                ["NODE_FAIL", "NODE_FAIL"],
                ["OUT_OF_MEMORY", "OUT_OF_MEMORY"],
                ...(![
                  "",
                  "RUNNING",
                  "PENDING",
                  "COMPLETING",
                  "COMPLETED",
                  "CANCELLED",
                  "FAILED",
                  "TIMEOUT",
                  "SUSPENDED",
                  "NODE_FAIL",
                  "OUT_OF_MEMORY",
                ].includes(state)
                  ? [[state, state]]
                  : []),
              ].map(([value, label]) => (
                <SelectItem key={value} value={value}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-2xs text-muted-foreground">
          服务端筛选与分页 · 显示当前控制器保留的可见记录
        </p>
        <div className="flex gap-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              reset(emptyFilters)
              onApply(emptyFilters)
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
