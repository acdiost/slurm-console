import { useEffect, useRef, useState } from "react"
import { Link } from "react-router"
import { useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { useQueryClient } from "@tanstack/react-query"
import { Cable, LoaderCircle } from "lucide-react"
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
import { textareaClass } from "@/features/platform-settings/common"
import {
  authenticationMethodLabel,
  type AuthenticationMethod,
  configurationSchema,
  type ClusterConfiguration,
} from "@/features/clusters/schema"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import {
  slurmSetupSchema,
  type SlurmSetupValues,
  type SetupStatus,
} from "./schema"
import { invalidateSetupQueries, setupError } from "./status"

export function SlurmSetup({
  status,
  authenticationMethod,
  canManage,
  onBack,
  onComplete,
}: {
  status: SetupStatus["slurm"]
  authenticationMethod: AuthenticationMethod
  canManage: boolean
  onBack: () => void
  onComplete: (config?: ClusterConfiguration) => void
}) {
  const client = useQueryClient()
  const owner = useAuth((s) => s.user?.id)
  const request = useRef<AbortController | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const {
    register,
    control,
    setValue,
    handleSubmit,
    resetField,
    formState: { errors },
  } = useForm<SlurmSetupValues>({
    resolver: zodResolver(slurmSetupSchema),
    defaultValues: {
      name: "",
      slug: "",
      authenticationMethod,
      authType: "local",
      endpoint: "",
      username: "",
      token: "",
      caPem: "",
      apiVersion: "auto",
      timeoutSeconds: 5,
    },
  })
  const authType = useWatch({ control, name: "authType" })
  const version = useWatch({ control, name: "apiVersion" })
  useEffect(
    () => () => {
      request.current?.abort()
      resetField("token")
    },
    [resetField]
  )
  async function save(values: SlurmSetupValues) {
    const controller = new AbortController()
    request.current = controller
    setBusy(true)
    setError("")
    try {
      const config = configurationSchema.parse(
        await api("/setup/slurm", {
          method: "POST",
          signal: controller.signal,
          body:
            values.authType === "local"
              ? { ...values, username: "", token: "", caPem: "" }
              : values,
        })
      )
      resetField("token")
      if (controller.signal.aborted || useAuth.getState().user?.id !== owner)
        return
      await invalidateSetupQueries(client)
      if (!controller.signal.aborted) onComplete(config)
    } catch (e) {
      if (!controller.signal.aborted) setError(setupError(e))
    } finally {
      if (request.current === controller) {
        request.current = null
        setBusy(false)
      }
    }
  }
  const fieldError = (name: keyof SlurmSetupValues) =>
    errors[name] && (
      <p
        id={`setup-${name}-error`}
        role="alert"
        className="text-xs text-destructive"
      >
        {errors[name]?.message}
      </p>
    )
  return (
    <section
      className="panel space-y-5 p-5 sm:p-7"
      aria-labelledby="slurm-setup-title"
    >
      <div className="flex items-center gap-3">
        <Cable className="size-5 text-primary" />
        <h2 id="slurm-setup-title" className="text-lg font-semibold">
          连接 slurmrestd
        </h2>
      </div>
      <p className="text-sm leading-7 text-muted-foreground">
        使用管理员已经部署的 Slurm / slurmrestd
        服务。保存时由后端检测版本、验证连接并持久化配置；检测失败时可以修改后重试。
      </p>
      <p className="text-sm">
        身份认证方式：{authenticationMethodLabel(authenticationMethod)}
      </p>
      {status.clusterCount > 0 && (
        <div className="space-y-3 rounded-lg border bg-muted/20 p-4 text-sm leading-7">
          <p>
            平台已有 {status.clusterCount} 个登记集群、{status.profileCount}{" "}
            个连接配置。
            {status.configured
              ? "已有可用配置，可以直接继续，也可在下方新增集群。"
              : "已有集群可能处于停用或未配置状态，可先检查并修复，避免重复新增。"}
          </p>
          <div className="flex flex-wrap gap-3">
            {canManage && (
              <Button asChild variant="outline">
                <Link to="/clusters">管理现有集群配置</Link>
              </Button>
            )}
            {status.configured && (
              <Button onClick={() => onComplete()}>使用现有配置继续</Button>
            )}
          </div>
        </div>
      )}
      {!canManage ? (
        <div className="space-y-4 text-sm">
          <p>需要集群管理权限才能添加连接，请联系平台管理员。</p>
          <Button variant="outline" onClick={onBack}>
            返回身份认证设置
          </Button>
        </div>
      ) : (
        <form
          noValidate
          onSubmit={(event) => void handleSubmit(save)(event)}
          className="space-y-6"
        >
          <fieldset disabled={busy} className="space-y-5 disabled:opacity-70">
            <div className="grid gap-5 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="setup-name">集群名称</Label>
                <Input
                  id="setup-name"
                  placeholder="例如：科研计算集群"
                  aria-invalid={!!errors.name}
                  aria-describedby="setup-name-error"
                  {...register("name")}
                />
                {fieldError("name")}
              </div>
              <div className="space-y-2">
                <Label htmlFor="setup-slug">集群标识</Label>
                <Input
                  id="setup-slug"
                  placeholder="例如：research-a"
                  autoComplete="off"
                  aria-invalid={!!errors.slug}
                  aria-describedby="setup-slug-error"
                  {...register("slug")}
                />
                <p className="text-xs text-muted-foreground">
                  使用小写字母、数字或连字符，创建后保持不变。
                </p>
                {fieldError("slug")}
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="setup-authType">连接方式</Label>
              <Select
                value={authType}
                onValueChange={(value) => {
                  setValue("authType", value === "jwt" ? "jwt" : "local")
                  setValue("endpoint", "")
                  resetField("token")
                  resetField("username")
                  resetField("caPem")
                  setError("")
                }}
              >
                <SelectTrigger id="setup-authType" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="local">本机 Unix socket</SelectItem>
                  <SelectItem value="jwt">远程 HTTPS / JWT</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="rounded-lg border bg-muted/20 p-4 text-sm leading-7">
              {authType === "local" ? (
                <>
                  <p className="font-medium">Unix socket 前置条件</p>
                  <p>
                    slurmrestd 与本应用位于同一 Linux 主机，已启用本地认证且
                    socket 可由本应用的 root 服务访问。填入真实 socket
                    的绝对路径；连接使用 root 身份，无需填写密码或 JWT。
                  </p>
                  <p className="font-mono text-xs">
                    unix:///run/slurmrestd/slurmrestd.sock
                  </p>
                </>
              ) : (
                <>
                  <p className="font-medium">HTTPS / JWT 前置条件</p>
                  <p>
                    管理员已配置 HTTPS 入口、有效证书和 slurmrestd JWT
                    认证。填写服务源地址、JWT 对应的 Slurm
                    用户名和未过期令牌。可在受信任的 Slurm 管理主机通过 scontrol
                    token username=&lt;用户名&gt; lifespan=3600
                    获取令牌，并按运行策略维护续期。
                  </p>
                  <p>
                    使用私有 CA 时粘贴 PEM
                    证书；留空使用系统信任库。证书必须匹配服务主机名，不能跳过
                    TLS 校验。
                  </p>
                </>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="setup-endpoint">
                {authType === "local" ? "Unix socket 地址" : "HTTPS 源地址"}
              </Label>
              <Input
                id="setup-endpoint"
                placeholder={
                  authType === "local"
                    ? "unix:///run/slurmrestd/slurmrestd.sock"
                    : "https://slurm.example.com"
                }
                autoComplete="off"
                aria-invalid={!!errors.endpoint}
                aria-describedby="setup-endpoint-error"
                {...register("endpoint")}
              />
              {fieldError("endpoint")}
            </div>
            {authType === "jwt" && (
              <>
                <div className="space-y-2">
                  <Label htmlFor="setup-username">Slurm 用户名</Label>
                  <Input
                    id="setup-username"
                    autoComplete="off"
                    aria-invalid={!!errors.username}
                    aria-describedby="setup-username-error"
                    {...register("username")}
                  />
                  {fieldError("username")}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="setup-token">Slurm JWT</Label>
                  <Input
                    id="setup-token"
                    type="password"
                    autoComplete="new-password"
                    spellCheck={false}
                    aria-invalid={!!errors.token}
                    aria-describedby="setup-token-error setup-token-help"
                    {...register("token")}
                  />
                  <p
                    id="setup-token-help"
                    className="text-xs text-muted-foreground"
                  >
                    只粘贴 SLURM_JWT=
                    后面的令牌值。令牌仅在当前表单内暂存；保存成功或离开此步骤即清空。后端安全保存后用于连接集群。
                  </p>
                  {fieldError("token")}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="setup-caPem">CA 证书 PEM（可选）</Label>
                  <textarea
                    id="setup-caPem"
                    className={`${textareaClass} font-mono text-xs`}
                    placeholder="-----BEGIN CERTIFICATE-----"
                    autoComplete="off"
                    spellCheck={false}
                    aria-invalid={!!errors.caPem}
                    aria-describedby="setup-caPem-error"
                    {...register("caPem")}
                  />
                  {fieldError("caPem")}
                </div>
              </>
            )}
            <div className="grid gap-5 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="setup-apiVersion">REST API 版本</Label>
                <Select
                  value={version}
                  onValueChange={(value) =>
                    setValue(
                      "apiVersion",
                      value as SlurmSetupValues["apiVersion"],
                      { shouldValidate: true }
                    )
                  }
                >
                  <SelectTrigger id="setup-apiVersion" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="auto">自动检测（推荐）</SelectItem>
                    {[
                      "v0.0.44",
                      "v0.0.43",
                      "v0.0.42",
                      "v0.0.41",
                      "v0.0.40",
                    ].map((v) => (
                      <SelectItem key={v} value={v}>
                        {v}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-xs leading-6 text-muted-foreground">
                  自动选择服务端已提供且平台支持的最高版本。
                </p>
                {fieldError("apiVersion")}
              </div>
              <div className="space-y-2">
                <Label htmlFor="setup-timeoutSeconds">请求超时（秒）</Label>
                <Input
                  id="setup-timeoutSeconds"
                  type="number"
                  min={1}
                  max={30}
                  aria-invalid={!!errors.timeoutSeconds}
                  aria-describedby="setup-timeoutSeconds-error"
                  {...register("timeoutSeconds", { valueAsNumber: true })}
                />
                {fieldError("timeoutSeconds")}
              </div>
            </div>
          </fieldset>
          {error && (
            <div
              role="alert"
              className="space-y-2 rounded-md border border-destructive/30 p-4 text-sm"
            >
              <p className="text-destructive">{error}</p>
              <p className="leading-6 text-muted-foreground">
                核对服务运行状态、socket 权限或 HTTPS 证书与
                JWT，再重试。若请求中断，请先刷新设置状态或查看现有集群，确认是否已保存。
              </p>
            </div>
          )}
          <div className="flex flex-wrap gap-3">
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={onBack}
            >
              返回身份认证设置
            </Button>
            <Button type="submit" disabled={busy}>
              {busy && <LoaderCircle className="animate-spin" />}
              {busy ? "正在检测并保存…" : "验证连接并保存"}
            </Button>
          </div>
        </form>
      )}
    </section>
  )
}
