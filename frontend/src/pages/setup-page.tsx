import { useState } from "react"
import { Link } from "react-router"
import { CheckCircle2, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { PAMSetup } from "@/features/setup/pam"
import { SlurmSetup } from "@/features/setup/slurm"
import { setupError, useSetupStatus } from "@/features/setup/status"
import type { ClusterConfiguration } from "@/features/clusters/schema"
import { useAuth } from "@/stores/auth"

export function SetupPage() {
  const user = useAuth((s) => s.user)
  return <SetupWizard key={user?.id} />
}

function SetupWizard() {
  const { query, canPAM, canSlurm, allowed } = useSetupStatus()
  const permissions = useAuth((s) => s.user?.access.platformPermissions) ?? []
  const canUsers =
    permissions.includes("platform.users.manage") ||
    permissions.includes("platform.roles.manage")
  const [step, setStep] = useState<"pam" | "slurm" | "done">("pam")
  const [saved, setSaved] = useState<ClusterConfiguration>()
  if (!allowed)
    return (
      <div role="alert" className="p-8 text-sm">
        需要平台设置管理或集群管理权限才能使用接入引导。
        <Button asChild variant="link">
          <Link to="/fleet">返回集群工作台</Link>
        </Button>
      </div>
    )
  const status = query.data
  return (
    <div className="mx-auto max-w-5xl space-y-6 p-4 sm:p-8">
      <header className="page-hero space-y-3">
        <p className="section-eyebrow">GETTING STARTED</p>
        <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">
          首次接入引导
        </h1>
        <p className="text-sm leading-7 text-muted-foreground">
          准备可选的系统身份登录，然后连接已有的 slurmrestd
          服务，开始使用集群工作台。
        </p>
        <div className="flex flex-wrap gap-3">
          <Button asChild variant="outline" size="sm">
            <Link to="/fleet">返回工作台</Link>
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
            刷新设置状态
          </Button>
          {canSlurm && (
            <Button asChild variant="outline" size="sm">
              <Link to="/clusters">管理集群配置</Link>
            </Button>
          )}
        </div>
      </header>
      <ol aria-label="接入步骤" className="grid gap-3 sm:grid-cols-3">
        {(
          [
            { value: "pam", label: "系统身份登录（可选）" },
            { value: "slurm", label: "连接 slurmrestd" },
            { value: "done", label: "完成设置" },
          ] as const
        ).map((item, index) => (
          <li
            key={item.value}
            aria-current={step === item.value ? "step" : undefined}
            className={`rounded-lg border px-4 py-3 text-sm ${step === item.value ? "border-primary bg-primary/5 font-medium text-primary" : "text-muted-foreground"}`}
          >
            {index + 1}. {item.label}
          </li>
        ))}
      </ol>
      {query.isPending && (
        <Skeleton className="h-72" aria-label="正在读取接入状态" />
      )}
      {query.isError && (
        <div role="alert" className="panel space-y-3 p-5">
          <p className="text-sm text-destructive">{setupError(query.error)}</p>
          <Button variant="outline" onClick={() => void query.refetch()}>
            重新读取状态
          </Button>
        </div>
      )}
      {status && !query.isError && (
        <>
          {!status.needsSetup && step !== "done" && (
            <p
              role="status"
              className="rounded-lg border bg-muted/20 p-4 text-sm leading-7"
            >
              平台已有启用且配置有效的集群。仍可在向导中管理 PAM
              或新增连接，亦可直接进入工作台。
            </p>
          )}
          {step === "pam" && (
            <PAMSetup
              status={status.pam}
              canManage={canPAM}
              canUsers={canUsers}
              onContinue={() => setStep("slurm")}
            />
          )}
          {step === "slurm" && (
            <SlurmSetup
              status={status.slurm}
              canManage={canSlurm}
              onBack={() => setStep("pam")}
              onComplete={(config) => {
                setSaved(config)
                setStep("done")
              }}
            />
          )}
          {step === "done" && (
            <section
              className="panel space-y-5 p-6 sm:p-8"
              aria-labelledby="setup-done-title"
            >
              <CheckCircle2 className="size-9 text-primary" />
              <h2 id="setup-done-title" className="text-xl font-semibold">
                {saved ? "集群连接已保存" : "已有集群配置可用"}
              </h2>
              <p className="text-sm leading-7 text-muted-foreground">
                {saved
                  ? `已保存「${saved.name}」，使用 ${saved.apiVersion}。${saved.lastProbe.message}`
                  : "可以进入工作台查看授权范围内的集群。"}{" "}
                PAM 为可选设置，本地管理员继续使用原有平台密码。
              </p>
              <div className="flex flex-wrap gap-3">
                {saved && (
                  <Button asChild>
                    <Link
                      to={`/overview?cluster=${encodeURIComponent(saved.id)}`}
                    >
                      进入集群
                    </Link>
                  </Button>
                )}
                <Button asChild variant={saved ? "outline" : "default"}>
                  <Link to="/fleet">进入集群工作台</Link>
                </Button>
                {canUsers && (
                  <Button asChild variant="outline">
                    <Link to="/settings?tab=users">管理用户与角色</Link>
                  </Button>
                )}
                <Button variant="ghost" onClick={() => setStep("pam")}>
                  返回设置步骤
                </Button>
              </div>
            </section>
          )}
        </>
      )}
    </div>
  )
}
