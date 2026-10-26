import { useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Search, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { actions, statuses, localInput } from "./schema"
const date = z
  .string()
  .refine((v) => !v || !Number.isNaN(new Date(v).valueOf()), "请选择有效时间")
const schema = z
  .object({
    q: z.string().trim().max(255, "最多 255 个字符"),
    cluster: z.string().trim().max(32, "最多 32 个字符"),
    actor: z.string().trim().max(32, "最多 32 个字符"),
    action: z.string().max(64),
    status: z.string().max(32),
    from: date,
    to: date,
  })
  .refine((v) => !v.from || !v.to || new Date(v.from) < new Date(v.to), {
    path: ["to"],
    message: "结束时间须晚于开始时间",
  })
export type Filters = z.infer<typeof schema>
export function AuditFilters({
  initial,
  onApply,
}: {
  initial: Filters
  onApply: (filters: Filters) => void
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
    defaultValues: {
      ...initial,
      from: localInput(initial.from),
      to: localInput(initial.to),
    },
  })
  const action = useWatch({ control, name: "action" }),
    status = useWatch({ control, name: "status" })
  return (
    <form
      noValidate
      onSubmit={handleSubmit((v) =>
        onApply({
          ...v,
          from: v.from ? new Date(v.from).toISOString() : "",
          to: v.to ? new Date(v.to).toISOString() : "",
        })
      )}
      className="panel space-y-4 p-4"
    >
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {(
          [
            ["q", "搜索审计", "操作/请求编号、目标、人员、集群或原因"],
            ["cluster", "集群 ID", "留空查询所有集群及平台操作"],
            ["actor", "操作人 ID", "平台用户 ID 或 local:uid:UID"],
          ] as const
        ).map(([key, label, placeholder]) => (
          <div key={key} className="space-y-2">
            <Label htmlFor={`audit-${key}`}>{label}</Label>
            <Input
              id={`audit-${key}`}
              placeholder={placeholder}
              {...register(key)}
              aria-invalid={!!errors[key]}
            />
            {errors[key] && (
              <p role="alert" className="text-2xs text-destructive">
                {errors[key].message}
              </p>
            )}
          </div>
        ))}
        {(
          [
            ["action", "操作动作", action, actions],
            ["status", "操作结果", status, statuses],
          ] as const
        ).map(([key, label, value, choices]) => (
          <div key={key} className="space-y-2">
            <Label htmlFor={`audit-${key}`}>{label}</Label>
            <Select
              value={value || "any"}
              onValueChange={(v) => setValue(key, v === "any" ? "" : v)}
            >
              <SelectTrigger id={`audit-${key}`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="any">
                  全部{key === "action" ? "动作" : "结果"}
                </SelectItem>
                {Object.entries({
                  ...choices,
                  ...(value && !choices[value] ? { [value]: value } : {}),
                }).map(([v, text]) => (
                  <SelectItem value={v} key={v}>
                    {text}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ))}
        <div className="space-y-2">
          <Label htmlFor="audit-from">开始时间（含）</Label>
          <Input
            type="datetime-local"
            step="1"
            id="audit-from"
            {...register("from")}
          />
          {errors.from && (
            <p role="alert" className="text-2xs text-destructive">
              {errors.from.message}
            </p>
          )}
        </div>
        <div className="space-y-2">
          <Label htmlFor="audit-to">结束时间（不含）</Label>
          <Input
            type="datetime-local"
            step="1"
            id="audit-to"
            {...register("to")}
          />
          {errors.to && (
            <p role="alert" className="text-2xs text-destructive">
              {errors.to.message}
            </p>
          )}
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-2xs leading-5 text-muted-foreground">
          时间按本机时区 {Intl.DateTimeFormat().resolvedOptions().timeZone} 输入
          · ID 精确匹配，可从详情直接筛选
        </p>
        <div className="flex gap-2">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => {
              const empty = {
                q: "",
                cluster: "",
                actor: "",
                action: "",
                status: "",
                from: "",
                to: "",
              }
              reset(empty)
              onApply(empty)
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
