import { useEffect } from "react"
import { getTokenPayload } from "pocketbase"
import { Navigate, Outlet } from "react-router"
import { LoaderCircle, ShieldAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import { pb } from "@/services/pocketbase"
import { logout, restoreSession, useAuth } from "@/stores/auth"

export function SessionLifecycle() {
  const status = useAuth((state) => state.status)
  useEffect(() => {
    void restoreSession()
  }, [])
  useEffect(() => {
    if (status !== "authenticated") return
    const expiry = Number(getTokenPayload(pb.authStore.token).exp) * 1000
    const timer = setTimeout(
      () => logout("会话已过期，请重新登录。"),
      Math.max(0, expiry - Date.now())
    )
    return () => clearTimeout(timer)
  }, [status])
  return null
}

export function RequireSession() {
  const { status, message } = useAuth()
  if (status === "checking")
    return (
      <div
        role="status"
        className="flex min-h-svh items-center justify-center gap-2 text-sm text-muted-foreground"
      >
        <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none" />
        正在校验登录状态…
      </div>
    )
  if (status === "unavailable")
    return (
      <div className="flex min-h-svh items-center justify-center p-6">
        <div role="alert" className="panel max-w-md space-y-4 p-6">
          <ShieldAlert className="size-6 text-destructive" />
          <h1 className="text-lg font-semibold">认证服务暂时不可用</h1>
          <p className="text-sm text-muted-foreground">{message}</p>
          <div className="flex gap-2">
            <Button onClick={() => void restoreSession()}>重试连接</Button>
            <Button variant="outline" onClick={() => logout()}>
              返回登录
            </Button>
          </div>
        </div>
      </div>
    )
  if (status !== "authenticated") return <Navigate to="/login" replace />
  return <Outlet />
}
