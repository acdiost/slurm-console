import { useState } from "react"
import { Link } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { CheckCircle2, LoaderCircle, ShieldCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { api } from "@/services/api"
import { pamStatusSchema, type PAMStatus } from "./schema"
import { invalidateSetupQueries, setupError } from "./status"

export function PAMSetup({
  status,
  canManage,
  canUsers,
  onContinue,
  onBack,
}: {
  status: PAMStatus
  canManage: boolean
  canUsers: boolean
  onContinue: () => void
  onBack: () => void
}) {
  const client = useQueryClient()
  const [checked, setChecked] = useState<{
    base: PAMStatus
    value: PAMStatus
  } | null>(null)
  const [operation, setOperation] = useState<"configure" | "check" | null>(null)
  const busy = operation !== null
  const [error, setError] = useState("")
  const current = checked?.base === status ? checked.value : status
  async function run(configure: boolean) {
    setOperation(configure ? "configure" : "check")
    setError("")
    try {
      const result = pamStatusSchema.parse(
        await api(configure ? "/setup/pam/configure" : "/setup/pam/check", {
          method: "POST",
          body: configure ? { revision: current.revision } : {},
        })
      )
      setChecked({ base: status, value: result })
      await invalidateSetupQueries(client)
    } catch (e) {
      setError(setupError(e))
      await invalidateSetupQueries(client)
    } finally {
      setOperation(null)
    }
  }
  return (
    <section
      className="panel space-y-5 p-5 sm:p-7"
      aria-labelledby="pam-setup-title"
    >
      <div className="flex flex-wrap items-center gap-3">
        <ShieldCheck className="size-5 text-primary" />
        <h2 id="pam-setup-title" className="text-lg font-semibold">
          准备 PAM 系统认证
        </h2>
        <Badge variant="outline">
          {current.configured ? "已启用 PAM" : "未启用 PAM"}
        </Badge>
      </div>
      <p className="text-sm leading-7 text-muted-foreground">
        PAM 让已关联的平台账户使用后端 Linux
        主机的系统密码登录。现有本地管理员继续使用平台密码；启用 PAM
        不会切换其登录方式，也不会自动创建账户或授予权限。
      </p>
      <dl className="grid gap-3 rounded-lg border bg-muted/20 p-4 text-sm sm:grid-cols-3">
        <div>
          <dt className="text-muted-foreground">运行环境</dt>
          <dd className="mt-1">
            {current.supported
              ? "Linux / root，可配置"
              : "当前主机不支持（需要 Linux / root）"}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">认证组件</dt>
          <dd className="mt-1">
            {current.helperInstalled ? "已安装" : "可从发布包自动配置"}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">应用专属 PAM 策略</dt>
          <dd className="mt-1">
            {current.policyInstalled ? "已准备" : "待自动配置"}
          </dd>
        </div>
      </dl>
      <p role="status" className="text-sm leading-6">
        {current.message ||
          (current.available
            ? "前置条件已就绪，可以检查并启用。"
            : "点击一键配置，系统将准备认证组件和专属 PAM 策略并检测。")}
      </p>
      {!current.configured && (
        <div className="space-y-3 rounded-lg border p-4 text-sm leading-7">
          <h3 className="font-medium">自动配置并复核</h3>
          <p>
            点击下方按钮后，系统将安装发布包内置的认证组件，基于本机现有认证栈创建应用专属策略，并在检测通过后启用
            PAM。已有的专属策略会保留，无需重启应用。
          </p>
          <p>
            支持 RHEL / Rocky 和 Debian / Ubuntu 的常规 PAM 配置。后端需以 root
            运行，并具备系统 PAM 运行库；发布包已包含认证组件，用户无需源码、Go
            或编译工具。
          </p>
        </div>
      )}
      {!canManage && (
        <p className="text-sm text-muted-foreground">
          当前账户没有平台设置管理权限，可以跳过此步骤并继续配置集群。
        </p>
      )}
      {error && (
        <p
          role="alert"
          className="rounded-md border border-destructive/30 p-3 text-sm text-destructive"
        >
          {error}
        </p>
      )}
      {current.configured && (
        <div className="space-y-2 rounded-lg border p-4 text-sm leading-7">
          <p className="flex items-center gap-2">
            <CheckCircle2 className="size-4 text-primary" />
            PAM 已启用。本地管理员登录方式保持不变。
          </p>
          <p>
            为需要系统密码登录的用户创建或关联 PAM
            身份，并单独分配平台角色与集群权限。请保留当前本地管理员账户。
          </p>
          {canUsers ? (
            <Button asChild variant="outline">
              <Link to="/settings?tab=users">管理用户与角色</Link>
            </Button>
          ) : (
            <p className="text-muted-foreground">
              请联系具有用户管理权限的管理员关联 PAM 用户。
            </p>
          )}
        </div>
      )}
      <div className="flex flex-wrap gap-3">
        <Button variant="ghost" disabled={busy} onClick={onBack}>
          返回选择身份认证方式
        </Button>
        {canManage && (
          <Button
            disabled={busy || !current.supported}
            onClick={() => void run(true)}
          >
            {operation === "configure" && (
              <LoaderCircle className="animate-spin" />
            )}
            {operation === "configure"
              ? "正在配置并复核…"
              : current.configured
                ? "重新配置并复核 PAM"
                : "一键配置并启用 PAM"}
          </Button>
        )}
        {canManage && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => void run(false)}
          >
            {operation === "check" && <LoaderCircle className="animate-spin" />}
            重新检测
          </Button>
        )}
        <Button
          variant={current.configured ? "default" : "outline"}
          disabled={busy}
          onClick={onContinue}
        >
          {current.configured ? "继续配置 slurmrestd" : "暂不启用，继续"}
        </Button>
      </div>
    </section>
  )
}
