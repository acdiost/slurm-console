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
const schema = z.object({ q: text, state: text })
export type Filters = z.infer<typeof schema>
const states = ["UP", "DOWN", "DRAIN", "INACTIVE", "UNKNOWN"]
export function PartitionFilters({
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
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="partitions-q">搜索分区</Label>
          <Input
            id="partitions-q"
            placeholder="分区名称"
            autoComplete="off"
            aria-invalid={!!errors.q}
            {...register("q")}
          />
          {errors.q && (
            <p role="alert" className="text-2xs text-destructive">
              {errors.q.message}
            </p>
          )}
        </div>
        <div className="space-y-2">
          <Label htmlFor="partitions-state">分区状态</Label>
          <Select
            value={state || "any"}
            onValueChange={(value) =>
              setValue("state", value === "any" ? "" : value)
            }
          >
            <SelectTrigger id="partitions-state" className="w-full">
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
          服务端筛选与分页 · 分区状态与节点状态分别展示
        </p>
        <div className="flex gap-2">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => {
              reset({ q: "", state: "" })
              onApply({ q: "", state: "" })
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
