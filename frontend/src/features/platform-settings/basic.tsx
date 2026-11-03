import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api } from "@/services/api"
import { identitySchema, settingsSchema, type Identity } from "./schema"
import { usePlatformIdentity } from "./identity"
import { errorMessage, textareaClass } from "./common"
export function BasicSettings({
  actor,
  manage,
}: {
  actor: string
  manage: boolean
}) {
  const query = useQuery({
    queryKey: ["platform-settings", actor],
    queryFn: async ({ signal }) =>
      settingsSchema.parse(await api("/platform-settings", { signal })),
    retry: false,
    refetchOnWindowFocus: false,
  })
  if (query.isPending) return <p role="status">正在读取基础设置…</p>
  if (query.isError || !query.data)
    return (
      <div role="alert" className="panel space-y-3 p-5">
        <p>基础设置不可用或访问权限已变化。</p>
        <Button onClick={() => void query.refetch()}>重新读取设置</Button>
      </div>
    )
  return (
    <BasicForm
      key={`${query.data.revision}:${query.data.updatedAt}`}
      data={query.data}
      actor={actor}
      manage={manage}
    />
  )
}
function BasicForm({
  data,
  actor,
  manage,
}: {
  data: ReturnType<typeof settingsSchema.parse>
  actor: string
  manage: boolean
}) {
  const client = useQueryClient(),
    [error, setError] = useState("")
  const form = useForm<Identity>({
    resolver: zodResolver(identitySchema),
    defaultValues: data,
  })
  const save = form.handleSubmit(async (values) => {
    setError("")
    try {
      const result = settingsSchema.parse(
        await api("/platform-settings", {
          method: "PATCH",
          body: { ...values, revision: data.revision },
        })
      )
      await client.cancelQueries({ queryKey: ["platform-public"] })
      client.setQueryData(["platform-settings", actor], result)
      client.setQueryData(["platform-public"], identitySchema.parse(result))
      usePlatformIdentity.setState(identitySchema.parse(result))
      void client.invalidateQueries({ queryKey: ["platform-ai-settings"] })
      toast.success("平台基础设置已保存")
    } catch (e) {
      setError(errorMessage(e))
    }
  })
  return (
    <form onSubmit={save} noValidate className="panel space-y-6 p-5 sm:p-6">
      <div>
        <h2 className="text-lg font-semibold">基础信息</h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          这些内容会在登录前公开。名称同步用于工作空间导航与页面标题，公告以纯文本显示。
        </p>
      </div>
      <fieldset
        disabled={!manage || form.formState.isSubmitting}
        className="space-y-5 disabled:opacity-70"
      >
        {(
          [
            ["name", "平台名称", 64],
            ["description", "平台说明", 240],
            ["loginNotice", "登录公告", 1000],
            ["supportContact", "管理员联系方式", 160],
          ] as const
        ).map(([key, label, max]) => (
          <div key={key} className="space-y-2">
            <Label htmlFor={`platform-${key}`}>
              {label}
              {key === "name" ? " *" : ""}
            </Label>
            {key === "description" || key === "loginNotice" ? (
              <textarea
                id={`platform-${key}`}
                className={textareaClass}
                maxLength={max}
                {...form.register(key)}
              />
            ) : (
              <Input
                id={`platform-${key}`}
                maxLength={max}
                {...form.register(key)}
              />
            )}
            <p className="text-xs text-muted-foreground">
              {key === "loginNotice"
                ? "清空即可隐藏公告。"
                : key === "supportContact"
                  ? "填写邮箱、电话或内部联络说明；不自动生成链接。"
                  : key === "name"
                    ? "2–64 个字符。"
                    : `最多 ${max} 个字符。`}
            </p>
            {form.formState.errors[key] && (
              <p role="alert" className="text-sm text-destructive">
                请检查{label}长度与格式。
              </p>
            )}
          </div>
        ))}
      </fieldset>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="submit"
          disabled={
            !manage || !form.formState.isDirty || form.formState.isSubmitting
          }
        >
          {form.formState.isSubmitting ? "正在保存…" : "保存基础设置"}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={form.formState.isSubmitting}
          onClick={() => {
            form.reset()
            setError("")
          }}
        >
          放弃修改
        </Button>
        <span className="text-xs text-muted-foreground">
          修订 {data.revision}
          {!manage ? " · 只读权限" : ""}
        </span>
      </div>
    </form>
  )
}
