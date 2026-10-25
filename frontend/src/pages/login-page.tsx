import { useState } from "react"
import { Navigate, useNavigate } from "react-router"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import {
  ArrowRight,
  Boxes,
  Eye,
  EyeOff,
  LoaderCircle,
  LockKeyhole,
  ShieldCheck,
  Terminal,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { authErrorMessage, login, useAuth } from "@/stores/auth"

const schema = z.object({
  identity: z.string().trim().min(1, "请输入用户名或邮箱").max(255),
  password: z.string().min(1, "请输入密码").max(255, "密码过长"),
})
type LoginValues = z.infer<typeof schema>

export function LoginPage() {
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState("")
  const { status, message } = useAuth()
  const navigate = useNavigate()
  const {
    register,
    handleSubmit,
    resetField,
    formState: { errors, isSubmitting },
  } = useForm<LoginValues>({
    resolver: zodResolver(schema),
    defaultValues: { identity: "", password: "" },
  })
  if (status === "authenticated") return <Navigate to="/overview" replace />
  const submit = handleSubmit(async ({ identity, password }) => {
    setError("")
    try {
      await login(identity, password)
      if (useAuth.getState().status === "authenticated")
        navigate("/overview", { replace: true })
    } catch (cause) {
      setError(authErrorMessage(cause))
    } finally {
      resetField("password")
    }
  })
  return (
    <div className="grid min-h-svh bg-card lg:grid-cols-[1fr_1.05fr]">
      <section className="relative hidden overflow-hidden border-r bg-sidebar p-12 lg:flex lg:flex-col xl:p-16">
        <div className="hero-grid pointer-events-none absolute inset-0 opacity-50" />
        <div className="relative flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <Boxes className="size-6" />
          </div>
          <span className="text-lg font-semibold tracking-tight">
            Slurm Console
          </span>
        </div>
        <div className="relative my-auto max-w-md py-20">
          <span className="font-mono text-2xs tracking-widest text-primary">
            COMPUTE OPERATIONS
          </span>
          <h1 className="mt-5 text-4xl leading-tight font-semibold tracking-tight">
            每一份算力，
            <br />
            尽在掌握。
          </h1>
          <p className="mt-6 text-sm leading-7 text-muted-foreground">
            连接集群、洞察资源、管理调度。
            <br />
            为团队提供清晰、一致的算力运维工作空间。
          </p>
          <div className="mt-10 flex items-center gap-3 rounded-xl border bg-card/80 p-5">
            <ShieldCheck className="size-6 shrink-0 text-primary" />
            <div>
              <p className="text-sm font-medium">统一的平台身份</p>
              <p className="mt-1 text-2xs text-muted-foreground">
                集群权限独立授权，操作范围清晰可控。
              </p>
            </div>
          </div>
        </div>
        <p className="relative flex items-center gap-2 font-mono text-2xs text-muted-foreground">
          <Terminal className="size-3.5" />
          Enterprise Compute Management
        </p>
      </section>
      <main className="flex min-w-0 flex-col items-center justify-center px-6 py-12 sm:px-12">
        <div className="w-full max-w-sm">
          <div className="mb-10 flex items-center gap-2 text-sm font-semibold lg:hidden">
            <Boxes className="size-5 text-primary" />
            Slurm Console
          </div>
          <div className="mb-7">
            <div className="mb-4 flex size-10 items-center justify-center rounded-xl border bg-muted/40">
              <LockKeyhole className="size-5 text-primary" />
            </div>
            <h2 className="text-2xl font-semibold tracking-tight">
              登录工作空间
            </h2>
            <p className="mt-2 text-sm text-muted-foreground">
              使用平台邮箱或已关联的用户名登录。
            </p>
          </div>
          <form className="space-y-5" onSubmit={submit} noValidate>
            <fieldset
              disabled={isSubmitting || status === "checking"}
              className="space-y-5 disabled:opacity-70"
            >
              <div className="space-y-2">
                <Label htmlFor="login-identity">用户名或邮箱</Label>
                <Input
                  id="login-identity"
                  type="text"
                  autoComplete="username"
                  autoCapitalize="none"
                  spellCheck={false}
                  placeholder="username 或 name@company.com"
                  className="h-10"
                  aria-invalid={!!errors.identity}
                  aria-describedby="login-identity-error"
                  {...register("identity")}
                />
                <p
                  id="login-identity-error"
                  aria-live="polite"
                  className="text-2xs text-destructive"
                >
                  {errors.identity?.message}
                </p>
              </div>
              <div className="space-y-2">
                <Label htmlFor="login-password">密码</Label>
                <div className="relative">
                  <Input
                    id="login-password"
                    type={showPassword ? "text" : "password"}
                    autoComplete="current-password"
                    placeholder="请输入密码"
                    className="h-10 pr-10"
                    aria-invalid={!!errors.password}
                    aria-describedby="login-password-error"
                    {...register("password")}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="absolute top-1.5 right-1.5"
                    aria-label={showPassword ? "隐藏密码" : "显示密码"}
                    onClick={() => setShowPassword(!showPassword)}
                  >
                    {showPassword ? <EyeOff /> : <Eye />}
                  </Button>
                </div>
                <p
                  id="login-password-error"
                  aria-live="polite"
                  className="text-2xs text-destructive"
                >
                  {errors.password?.message}
                </p>
              </div>
              {(error || message) && (
                <div
                  role="alert"
                  className="rounded-md border border-destructive/20 bg-destructive/5 p-3 text-xs leading-relaxed text-destructive"
                >
                  {error || message}
                </div>
              )}
              <Button type="submit" className="h-10 w-full">
                {isSubmitting || status === "checking" ? (
                  <>
                    <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none" />
                    {isSubmitting ? "正在登录…" : "正在校验会话…"}
                  </>
                ) : (
                  <>
                    登录
                    <ArrowRight />
                  </>
                )}
              </Button>
            </fieldset>
          </form>
          <p className="mt-6 text-center text-2xs leading-6 text-muted-foreground">
            平台账户由管理员统一管理。
            <br />
            需要开通账户或重置密码，请联系管理员。
          </p>
          <p className="mt-12 text-center font-mono text-micro text-muted-foreground/70">
            SLURM CONSOLE · INTERNAL ACCESS
          </p>
        </div>
      </main>
    </div>
  )
}
