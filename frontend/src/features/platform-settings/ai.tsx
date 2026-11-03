import { useState } from "react"
import { useForm, Controller } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { z } from "zod"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select"
import { api } from "@/services/api"
import {
  aiConfigSchema,
  aiSettingsSchema,
  runtimeSchema,
  type AISettings,
} from "./schema"
import { Confirmation, errorMessage, textareaClass } from "./common"
const formSchema = aiConfigSchema
  .omit({ clusterIds: true })
  .extend({ clusterIds: z.string().max(2000) })
type Values = z.infer<typeof formSchema>
export function IntegrationSettings({
  actor,
  manage,
}: {
  actor: string
  manage: boolean
}) {
  const config = useQuery({
    queryKey: ["platform-ai-settings", actor],
    queryFn: async ({ signal }) =>
      aiSettingsSchema.parse(await api("/platform-settings/ai", { signal })),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const runtime = useQuery({
    queryKey: ["platform-runtime", actor],
    queryFn: async ({ signal }) =>
      runtimeSchema.parse(await api("/platform-settings/runtime", { signal })),
    retry: false,
    refetchOnWindowFocus: false,
  })
  return (
    <div className="space-y-6">
      <section className="panel space-y-4 p-5 sm:p-6">
        <div>
          <h2 className="text-lg font-semibold">集成状态</h2>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            仅表示当前进程已加载配置，不代表外部服务连通。PAM、LDAP、Slurm
            与监控来源由服务器管理员维护。
          </p>
        </div>
        {runtime.isPending ? (
          <p role="status">正在读取集成状态…</p>
        ) : runtime.isError ? (
          <p role="alert">集成状态不可用，请刷新页面。</p>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {runtime.data?.integrations.map((i) => (
              <div key={i.id} className="rounded-md border p-4">
                <h3 className="text-sm font-medium">{i.name}</h3>
                <p className="mt-2 text-sm text-muted-foreground">
                  {i.configured ? "已加载配置" : "未启用 / 未配置"}
                  {i.count > 0 ? ` · ${i.count} 项` : ""}
                </p>
              </div>
            ))}
          </div>
        )}
      </section>
      {config.isPending ? (
        <p role="status">正在读取 AI 设置…</p>
      ) : config.isError || !config.data ? (
        <section role="alert" className="panel space-y-3 p-5">
          <p>AI 设置不可用或权限已变化。</p>
          <Button onClick={() => void config.refetch()}>
            重新读取 AI 设置
          </Button>
        </section>
      ) : (
        <AIForm
          key={config.data.revision}
          actor={actor}
          data={config.data}
          manage={manage}
        />
      )}
    </div>
  )
}
function AIForm({
  actor,
  data,
  manage,
}: {
  actor: string
  data: AISettings
  manage: boolean
}) {
  const client = useQueryClient(),
    [review, setReview] = useState<z.infer<typeof aiConfigSchema>>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("")
  const form = useForm<Values>({
    resolver: zodResolver(formSchema),
    defaultValues: { ...data, clusterIds: data.clusterIds.join("\n") },
  })
  const prepare = form.handleSubmit((values) => {
    setError("")
    const parsed = aiConfigSchema.safeParse({
      ...values,
      clusterIds: values.clusterIds.split(/[\s,]+/).filter(Boolean),
    })
    if (!parsed.success) {
      setError("请填写有效的集群 ID，每行一个，最多 100 个。")
      return
    }
    if (
      parsed.data.enabled &&
      (!parsed.data.endpoint ||
        !parsed.data.model ||
        parsed.data.clusterIds.length === 0)
    ) {
      setError("启用 AI 时必须填写接收地址、模型和至少一个允许集群。")
      return
    }
    setReview(parsed.data)
  })
  const apply = async () => {
    if (!review) return
    setBusy(true)
    setError("")
    try {
      const saved = aiSettingsSchema.parse(
        await api("/platform-settings/ai", {
          method: "PATCH",
          body: {
            config: review,
            revision: data.revision,
            confirmation: "APPLY AI",
          },
        })
      )
      client.setQueryData(["platform-ai-settings", actor], saved)
      void client.invalidateQueries({ queryKey: ["platform-settings"] })
      void client.invalidateQueries({ queryKey: ["platform-runtime"] })
      void client.invalidateQueries({ queryKey: ["ai-assistance"] })
      toast.success("AI 配置已保存并生效")
      setReview(undefined)
    } catch (e) {
      setReview(undefined)
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <section className="panel space-y-6 p-5 sm:p-6">
      <div>
        <h2 className="text-lg font-semibold">AI 运维辅助</h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          配置 Chat Completions
          兼容服务。保存配置不会发送模型请求；作业诊断仍需逐次预览和确认。
        </p>
      </div>
      <form className="space-y-5" noValidate onSubmit={prepare}>
        <fieldset
          disabled={!manage || busy}
          className="space-y-5 disabled:opacity-70"
        >
          <Controller
            control={form.control}
            name="enabled"
            render={({ field }) => (
              <label className="flex items-center gap-3 text-sm">
                <Checkbox
                  checked={field.value}
                  onCheckedChange={(v) => field.onChange(v === true)}
                />
                启用 AI 辅助
              </label>
            )}
          />
          <div className="grid gap-5 sm:grid-cols-2">
            {(
              [
                ["label", "服务名称"],
                ["model", "模型名"],
                ["endpoint", "完整接收地址"],
              ] as const
            ).map(([key, label]) => (
              <div
                key={key}
                className={`space-y-2 ${key === "endpoint" ? "sm:col-span-2" : ""}`}
              >
                <Label htmlFor={`ai-${key}`}>{label}</Label>
                <Input
                  id={`ai-${key}`}
                  autoComplete="off"
                  placeholder={
                    key === "endpoint"
                      ? "https://ai.example.internal/v1/chat/completions"
                      : undefined
                  }
                  {...form.register(key)}
                />
                {form.formState.errors[key] && (
                  <p role="alert" className="text-sm text-destructive">
                    请检查{label}。
                  </p>
                )}
              </div>
            ))}
          </div>
          <div className="space-y-2">
            <Label htmlFor="ai-clusters">允许发送证据的集群 ID</Label>
            <textarea
              id="ai-clusters"
              className={`${textareaClass} font-mono`}
              {...form.register("clusterIds")}
            />
            <p className="text-xs leading-6 text-muted-foreground">
              每行一个 ID，最多 100
              个。可从集群配置复制，不接受通配符；仍需用户具备该集群 AI
              及作业读取权限。
            </p>
          </div>
          <div className="grid gap-5 sm:grid-cols-3">
            <div className="space-y-2">
              <Label htmlFor="ai-timeout">超时（秒）</Label>
              <Input
                id="ai-timeout"
                type="number"
                min={5}
                max={60}
                {...form.register("timeoutSeconds", { valueAsNumber: true })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ai-tokens">输出 token 上限</Label>
              <Input
                id="ai-tokens"
                type="number"
                min={512}
                max={4096}
                {...form.register("maxOutputTokens", { valueAsNumber: true })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ai-parameter">Token 参数</Label>
              <Controller
                control={form.control}
                name="tokenParameter"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="ai-parameter">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="max_completion_tokens">
                        max_completion_tokens
                      </SelectItem>
                      <SelectItem value="max_tokens">max_tokens</SelectItem>
                    </SelectContent>
                  </Select>
                )}
              />
            </div>
          </div>
          {Object.keys(form.formState.errors).length > 0 && (
            <p role="alert" className="text-sm text-destructive">
              请检查表单，超时为 5–60 秒，输出上限为 512–4096。
            </p>
          )}
        </fieldset>
        <p className="rounded-md bg-muted/40 p-4 text-sm leading-7">
          服务器密钥：
          {data.keyConfigured
            ? "已引用受保护文件"
            : "未配置（仅适用于不需要密钥的服务）"}
          <br />
          自定义 CA：{data.caConfigured ? "已配置" : "使用系统信任库"}
          <br />
          密钥与证书继续在服务器配置文件中维护，页面不读取或更改其内容。HTTPS
          始终校验证书；无凭据的字面回环 IP 可使用 HTTP。
        </p>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <div className="flex flex-wrap items-center gap-3">
          <Button
            disabled={!manage || busy || !form.formState.isDirty}
            type="submit"
          >
            核对 AI 配置
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            onClick={() => {
              form.reset()
              setError("")
            }}
          >
            放弃修改
          </Button>
          <span className="text-xs text-muted-foreground">
            修订 {data.revision} ·{" "}
            {data.source === "server" ? "服务器配置" : "平台保存配置"}
          </span>
        </div>
      </form>
      {review && (
        <Confirmation
          title="确认 AI 接入配置"
          phrase="APPLY AI"
          busy={busy}
          onCancel={() => setReview(undefined)}
          onConfirm={() => void apply()}
        >
          <p className="break-all">
            服务：{review.label}
            <br />
            接收地址：{review.endpoint || "未填写"}
            <br />
            模型：{review.model || "未填写"}
            <br />
            状态：{review.enabled ? "启用" : "停用"}
          </p>
          <p className="break-all">
            允许集群：{review.clusterIds.join("、") || "无"}
          </p>
          {data.keyConfigured && (
            <p>
              启用后，服务器现有密钥会用于此接收地址。请确认该服务有权接收凭据和已确认的作业证据。
            </p>
          )}
          <p>
            保存后立即生效，旧 AI
            预览失效，进行中的旧请求会中止；已发出的内容无法撤回。此步骤不调用模型。
          </p>
        </Confirmation>
      )}
    </section>
  )
}
