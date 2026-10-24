import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Check, RotateCcw } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

const schema = z.object({
  displayName: z
    .string()
    .trim()
    .min(2, "请输入 2–32 个字符")
    .max(32, "请输入 2–32 个字符"),
})
type FormValues = z.infer<typeof schema>
export function SettingsFormPreview() {
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { displayName: "" },
  })
  return (
    <form
      onSubmit={handleSubmit(({ displayName }) =>
        toast.success(`校验通过：${displayName}`, {
          description: "仅演示表单反馈，未保存集群配置。",
        })
      )}
      className="space-y-3"
      noValidate
    >
      <div className="space-y-1.5">
        <Label htmlFor="display-name">
          集群显示名称 <span className="text-destructive">*</span>
        </Label>
        <Input
          id="display-name"
          placeholder="例如：科研计算集群"
          aria-invalid={!!errors.displayName}
          aria-describedby="display-name-help display-name-error"
          {...register("displayName")}
        />
        <p id="display-name-help" className="text-2xs text-muted-foreground">
          2–32 个字符 · 仅作表单校验演示
        </p>
        <p
          id="display-name-error"
          aria-live="polite"
          className="text-xs text-destructive"
        >
          {errors.displayName?.message}
        </p>
      </div>
      <div className="flex gap-2">
        <Button type="submit">
          <Check />
          验证输入
        </Button>
        <Button type="button" variant="outline" onClick={() => reset()}>
          <RotateCcw />
          重置
        </Button>
      </div>
    </form>
  )
}
