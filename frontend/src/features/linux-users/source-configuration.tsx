import { useEffect, useRef, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { ClientResponseError } from "pocketbase"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Confirmation,
  textareaClass,
} from "@/features/platform-settings/common"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { linuxUserSourceSchema, type LinuxUserSource } from "./schema"

const byteLength = (value: string) => new TextEncoder().encode(value).byteLength
const fieldsSchema = z.object({
  label: z
    .string()
    .trim()
    .min(1, "请输入来源名称")
    .refine(
      (value) => byteLength(value) <= 160 && !/[\p{Cc}]/u.test(value),
      "来源名称最多 160 字节，不能包含控制字符"
    ),
  transport: z.enum(["local", "ssh"]),
  host: z.string().trim(),
  port: z.number({ error: "请输入有效端口" }),
  username: z.string().trim(),
  privateKey: z.string(),
  knownHosts: z.string(),
  manageLocal: z.boolean(),
})
type Fields = z.infer<typeof fieldsSchema>

function sourceError(error: unknown) {
  const status = error instanceof ClientResponseError ? error.status : 0
  switch (status) {
    case 400:
      return "配置无效，请检查来源名称、SSH 地址、非 root 用户、私钥和 known_hosts 内容后重试。"
    case 403:
      return "没有配置此集群身份来源的权限，需要平台设置管理和此集群 Linux 用户读取权限。"
    case 404:
      return "此集群已不存在，请刷新集群列表。"
    case 409:
      return "身份来源已变更，请刷新来源配置，核对后重新提交。"
    case 503:
      return "身份来源验证失败，原配置未更改。请检查主机连接、私钥、可信主机记录及 NSS 查询能力后重试。"
    default:
      return "请求未完成，请刷新来源配置核对结果后重试。"
  }
}

export function LinuxUserSourceConfiguration({
  clusterId,
  clusterName,
  onChanged,
}: {
  clusterId: string
  clusterName: string
  onChanged: () => void
}) {
  const access = useAuth((state) => state.user?.access)
  const allowed =
    access?.platformPermissions.includes("platform.settings.manage") &&
    (access.platformPermissions.includes("linux_users.read") ||
      access.clusters.some(
        (entry) =>
          entry.cluster.id === clusterId &&
          entry.permissions.includes("linux_users.read")
      ))
  return allowed ? (
    <SourceConfiguration
      key={clusterId}
      clusterId={clusterId}
      clusterName={clusterName}
      onChanged={onChanged}
    />
  ) : null
}

function SourceConfiguration({
  clusterId,
  clusterName,
  onChanged,
}: {
  clusterId: string
  clusterName: string
  onChanged: () => void
}) {
  const [open, setOpen] = useState(false)
  const [message, setMessage] = useState("")
  const [busy, setBusy] = useState(false)
  const query = useQuery({
    queryKey: ["linux-user-source", clusterId],
    queryFn: async ({ signal }) =>
      linuxUserSourceSchema.parse(
        await api(
          `/clusters/${encodeURIComponent(clusterId)}/linux-user-source`,
          { signal }
        )
      ),
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: false,
  })
  const source = query.isError ? undefined : query.data
  return (
    <section className="panel space-y-4 p-4" aria-label="身份来源配置">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">身份来源 · {clusterName}</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            {source
              ? source.configured
                ? `${source.label} · ${source.transport === "ssh" ? "SSH（只读）" : "本地 NSS"}`
                : "此集群尚未配置身份来源。配置并验证来源后即可查询 Linux 用户。"
              : "读取此集群的身份来源配置。"}
          </p>
        </div>
        <Button
          variant="outline"
          disabled={!source || busy}
          onClick={() => setOpen(!open)}
        >
          {open ? "收起身份来源配置" : "配置身份来源"}
        </Button>
      </div>
      {query.isPending && (
        <p role="status" className="text-sm">
          正在读取身份来源配置…
        </p>
      )}
      {query.isError && (
        <p role="alert" className="text-sm text-destructive">
          {sourceError(query.error)}
        </p>
      )}
      {(open || query.isError) && (
        <Button
          variant="ghost"
          disabled={query.isFetching || busy}
          onClick={() => void query.refetch()}
        >
          刷新来源配置
        </Button>
      )}
      {message && (
        <p role="status" className="text-sm">
          {message}
        </p>
      )}
      {open && source && (
        <SourceForm
          key={`${clusterId}:${query.dataUpdatedAt}`}
          source={source}
          clusterId={clusterId}
          busy={busy}
          setBusy={setBusy}
          onChanged={() => {
            setMessage("身份来源已更新。")
            onChanged()
          }}
        />
      )}
    </section>
  )
}

function SourceForm({
  source,
  clusterId,
  busy,
  setBusy,
  onChanged,
}: {
  source: LinuxUserSource
  clusterId: string
  busy: boolean
  setBusy: (value: boolean) => void
  onChanged: () => void
}) {
  const client = useQueryClient()
  const [error, setError] = useState("")
  const [conflict, setConflict] = useState(false)
  const [removing, setRemoving] = useState(false)
  const request = useRef<AbortController | null>(null)
  useEffect(
    () => () => {
      request.current?.abort()
      setBusy(false)
    },
    [setBusy]
  )
  const schema = fieldsSchema.superRefine((value, ctx) => {
    if (value.transport !== "ssh") return
    const invalid = (path: keyof Fields, message: string) =>
      ctx.addIssue({ code: "custom", path: [path], message })
    if (
      !/^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$/.test(value.host) &&
      !z.ipv6().safeParse(value.host).success
    )
      invalid("host", "请输入主机名或 IP 地址，不能填写 URL 或文件路径")
    if (!Number.isInteger(value.port) || value.port < 1 || value.port > 65535)
      invalid("port", "端口必须为 1–65535 的整数")
    if (
      !/^[a-z_][a-z0-9_-]{0,31}$/.test(value.username) ||
      value.username === "root"
    )
      invalid("username", "请输入合法的非 root SSH 用户名")
    if (
      (!value.privateKey.trim() && !source.privateKeyConfigured) ||
      value.privateKey.includes("\0") ||
      byteLength(value.privateKey) > 65536
    )
      invalid("privateKey", "请填写 SSH 私钥内容（最多 64 KiB），不能包含 NUL")
    if (
      !value.knownHosts.trim() ||
      value.knownHosts.includes("\0") ||
      byteLength(value.knownHosts) > 1048576
    )
      invalid(
        "knownHosts",
        "请填写可信 known_hosts 内容（最多 1 MiB），不能包含 NUL"
      )
  })
  const form = useForm<Fields>({
    resolver: zodResolver(schema),
    defaultValues: {
      label: source.label,
      transport: source.transport,
      host: source.host,
      port: source.port || 22,
      username: source.username,
      privateKey: "",
      knownHosts: source.knownHosts,
      manageLocal: source.manageLocal,
    },
  })
  const transport = useWatch({ control: form.control, name: "transport" })
  const manageLocal = useWatch({ control: form.control, name: "manageLocal" })

  async function submit(method: "PUT" | "DELETE", values?: Fields) {
    if (request.current || conflict) return
    const controller = new AbortController()
    request.current = controller
    setBusy(true)
    setError("")
    try {
      const body =
        method === "DELETE"
          ? { revision: source.revision }
          : {
              ...values!,
              revision: source.revision,
              ...(values!.transport === "local"
                ? {
                    host: "",
                    port: 0,
                    username: "",
                    privateKey: "",
                    knownHosts: "",
                  }
                : { manageLocal: false }),
            }
      await api(
        `/clusters/${encodeURIComponent(clusterId)}/linux-user-source`,
        { method, body, signal: controller.signal }
      )
      // Cancel old reads before clearing them so an earlier source cannot repopulate the UI.
      const keys = [
        "linux-users",
        "linux-user-detail",
        "linux-user-capabilities",
        "linux-user-operation",
      ]
      await Promise.all(
        keys.map((key) => client.cancelQueries({ queryKey: [key, clusterId] }))
      )
      if (!controller.signal.aborted) {
        form.setValue("privateKey", "")
        setRemoving(false)
        onChanged()
      }
      await Promise.all(
        keys.map((key) => client.resetQueries({ queryKey: [key, clusterId] }))
      )
      await client.invalidateQueries({
        queryKey: ["linux-user-source", clusterId],
      })
    } catch (failure) {
      if (!controller.signal.aborted) {
        setError(sourceError(failure))
        setRemoving(false)
        setConflict(
          failure instanceof ClientResponseError && failure.status === 409
        )
      }
    } finally {
      if (!controller.signal.aborted) {
        request.current = null
        setBusy(false)
      }
    }
  }

  return (
    <>
      <form
        className="space-y-4"
        noValidate
        onSubmit={(event) => {
          void form.handleSubmit((values) => submit("PUT", values))(event)
        }}
      >
        <fieldset disabled={busy || conflict} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="identity-source-label">来源名称</Label>
            <Input
              id="identity-source-label"
              {...form.register("label")}
              aria-invalid={!!form.formState.errors.label}
            />
            {form.formState.errors.label && (
              <p role="alert" className="text-xs text-destructive">
                {form.formState.errors.label.message}
              </p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="identity-source-transport">连接方式</Label>
            <Select
              value={transport}
              disabled={busy || conflict}
              onValueChange={(value) => {
                form.setValue("transport", value as Fields["transport"])
                form.setValue("manageLocal", false)
                form.clearErrors()
                if (value === "local") {
                  form.setValue("host", "")
                  form.setValue("port", 22)
                  form.setValue("username", "")
                  form.setValue("privateKey", "")
                  form.setValue("knownHosts", "")
                }
              }}
            >
              <SelectTrigger id="identity-source-transport">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="local">本地 NSS</SelectItem>
                <SelectItem value="ssh">SSH（只读）</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {transport === "ssh" ? (
            <>
              {(
                [
                  ["host", "SSH 主机"],
                  ["port", "SSH 端口"],
                  ["username", "SSH 用户名（非 root）"],
                ] as const
              ).map(([field, label]) => (
                <div key={field} className="space-y-2">
                  <Label htmlFor={`identity-source-${field}`}>{label}</Label>
                  <Input
                    id={`identity-source-${field}`}
                    autoComplete="off"
                    type={field === "port" ? "number" : "text"}
                    {...form.register(
                      field,
                      field === "port" ? { valueAsNumber: true } : {}
                    )}
                    aria-invalid={!!form.formState.errors[field]}
                  />
                  {form.formState.errors[field] && (
                    <p role="alert" className="text-xs text-destructive">
                      {form.formState.errors[field]?.message}
                    </p>
                  )}
                </div>
              ))}
              <div className="space-y-2">
                <Label htmlFor="identity-source-key">SSH 私钥内容</Label>
                <textarea
                  id="identity-source-key"
                  className={`${textareaClass} font-mono`}
                  autoComplete="off"
                  spellCheck={false}
                  {...form.register("privateKey")}
                  aria-invalid={!!form.formState.errors.privateKey}
                  aria-describedby="identity-source-key-help"
                />
                <p
                  id="identity-source-key-help"
                  className="text-xs text-muted-foreground"
                >
                  {source.privateKeyConfigured
                    ? "已保存私钥；留空保留已保存的私钥，填写内容将替换。私钥不会回显。"
                    : "粘贴用于连接的私钥内容。"}
                </p>
                {form.formState.errors.privateKey && (
                  <p role="alert" className="text-xs text-destructive">
                    {form.formState.errors.privateKey.message}
                  </p>
                )}
              </div>
              <div className="space-y-2">
                <Label htmlFor="identity-source-known-hosts">
                  可信 known_hosts 内容
                </Label>
                <textarea
                  id="identity-source-known-hosts"
                  className={`${textareaClass} font-mono`}
                  autoComplete="off"
                  spellCheck={false}
                  {...form.register("knownHosts")}
                  aria-invalid={!!form.formState.errors.knownHosts}
                />
                <p className="text-xs text-muted-foreground">
                  粘贴已通过可信渠道核实的 SSH 主机公钥记录；主机指纹必须匹配。
                </p>
                {form.formState.errors.knownHosts && (
                  <p role="alert" className="text-xs text-destructive">
                    {form.formState.errors.knownHosts.message}
                  </p>
                )}
              </div>
              <p className="text-xs text-muted-foreground">
                SSH 来源仅查询身份，不支持本地账户写入。
              </p>
            </>
          ) : (
            <>
              <p className="text-xs text-muted-foreground">
                读取控制台服务器的 NSS 身份。保存本地来源将清除已保存的 SSH
                主机、用户、私钥和 known_hosts 内容。
              </p>
              <label className="flex items-center gap-3 text-sm">
                <Checkbox
                  checked={manageLocal}
                  disabled={busy || conflict}
                  onCheckedChange={(value) =>
                    form.setValue("manageLocal", value === true)
                  }
                />
                允许本地账户管理
              </label>
              <p className="text-xs text-muted-foreground">
                此选项还需服务器单独启用 root 账户管理策略，并满足操作权限及 UID
                范围限制；配置身份来源不会启用该策略。
              </p>
            </>
          )}
        </fieldset>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={busy || conflict}>
            {busy ? "正在验证并保存…" : "验证并保存"}
          </Button>
          {source.configured && (
            <Button
              type="button"
              variant="destructive"
              disabled={busy || conflict}
              onClick={() => setRemoving(true)}
            >
              移除身份来源
            </Button>
          )}
        </div>
      </form>
      {removing && (
        <Confirmation
          title="移除身份来源"
          phrase={source.label}
          busy={busy}
          onCancel={() => setRemoving(false)}
          onConfirm={() => void submit("DELETE")}
        >
          <p>
            移除此集群的身份来源及已保存的 SSH 私钥。之后需重新配置才能查询
            Linux 用户。
          </p>
          {error && (
            <p role="alert" className="text-destructive">
              {error}
            </p>
          )}
        </Confirmation>
      )}
    </>
  )
}
