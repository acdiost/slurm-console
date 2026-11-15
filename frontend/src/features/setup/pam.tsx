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
}: {
  status: PAMStatus
  canManage: boolean
  canUsers: boolean
  onContinue: () => void
}) {
  const client = useQueryClient()
  const [checked, setChecked] = useState<{
    base: PAMStatus
    value: PAMStatus
  } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const current = checked?.base === status ? checked.value : status
  async function run(enable: boolean) {
    setBusy(true)
    setError("")
    try {
      const result = pamStatusSchema.parse(
        await api(enable ? "/setup/pam" : "/setup/pam/check", {
          method: enable ? "PATCH" : "POST",
          body: enable ? { enabled: true, revision: current.revision } : {},
        })
      )
      setChecked({ base: status, value: result })
      await invalidateSetupQueries(client)
    } catch (e) {
      setError(setupError(e))
    } finally {
      setBusy(false)
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
          系统身份登录（可选）
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
          <dt className="text-muted-foreground">认证 helper</dt>
          <dd className="mt-1">
            {current.helperInstalled ? "已安装" : "尚未准备"}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">应用专属 PAM 策略</dt>
          <dd className="mt-1">
            {current.policyInstalled ? "已安装" : "尚未准备"}
          </dd>
        </div>
      </dl>
      <p role="status" className="text-sm leading-6">
        {current.message ||
          (current.available
            ? "前置条件已就绪，可以检查并启用。"
            : "请先准备 helper 和应用专属 PAM 策略。")}
      </p>
      {!current.configured && (
        <div className="space-y-3 rounded-lg border p-4 text-sm leading-7">
          <h3 className="font-medium">在后端 Linux 主机上安装 PAM 集成</h3>
          <p>
            以下命令适用于已安装本应用、服务以 root 运行且具有
            /etc/pam.d/password-auth 的 Rocky Linux 9 / RHEL
            9。先取得与部署版本一致的完整源码，并在源码根目录执行；构建需要
            Go、make、gcc 和 PAM 开发包。
          </p>
          <pre className="overflow-x-auto rounded-md bg-muted p-4 font-mono text-xs leading-6">
            <code>
              {
                "sudo dnf install -y make gcc pam-devel\n# 在完整源码根目录，使用已安装的 Go 工具链\nmake pam-helper\nsudo bash deploy/setup-pam.sh"
              }
            </code>
          </pre>
          <p>
            setup-pam.sh 会安装 helper、准备应用专属策略，并在服务环境中启用
            PAM；配置变化时可能重启正在运行的本应用。若网页中已保存 PAM
            设置，则以保存值为准：已保存的停用状态会保持停用，直到在本页明确启用。执行后请点击「检查
            PAM 环境」重新读取状态；若已启用，可直接继续下一步。
          </p>
          <p>
            只有应用二进制和 service 文件的两文件发行包无法单独编译
            helper：必须另行取得源码，或由管理员提供匹配目标 Linux 架构的
            helper。setup-pam.sh 从源码目录的 backend/slurm-console-pam-helper
            安装 helper；请勿在发行包目录直接运行 make。
          </p>
          <p>
            共享 PAM 策略、系统账户及 Slurm 由管理员维护。其他 Linux
            发行版需由管理员适配本应用的 /etc/pam.d/slurm-console
            策略，不能直接套用上述 Rocky/RHEL 脚本。
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
        {canManage && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => void run(false)}
          >
            {busy && <LoaderCircle className="animate-spin" />}检查 PAM 环境
          </Button>
        )}
        {canManage && !current.configured && (
          <Button
            disabled={busy || !current.supported || !current.available}
            onClick={() => void run(true)}
          >
            检查并启用 PAM
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
