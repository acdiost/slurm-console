import { lazy, Suspense, useEffect } from "react"
import { QueryClientProvider } from "@tanstack/react-query"
import {
  BrowserRouter,
  Link,
  Navigate,
  Route,
  Routes,
  useLocation,
} from "react-router"
import { Toaster } from "@/components/ui/sonner"
import { Button } from "@/components/ui/button"
import { TooltipProvider } from "@/components/ui/tooltip"
import { ConsoleLayout } from "@/app/console-layout"
import { useAppearance } from "@/hooks/use-appearance"
import { queryClient } from "@/lib/query-client"
import {
  SessionLifecycle,
  RequireSession,
} from "@/features/auth/session-boundary"
const LoginPage = lazy(() =>
  import("@/pages/login-page").then((module) => ({ default: module.LoginPage }))
)

const DesignSystemPage = lazy(() =>
  import("@/pages/design-system-page").then((module) => ({
    default: module.DesignSystemPage,
  }))
)
const AccessPage = lazy(() =>
  import("@/pages/access-page").then((module) => ({
    default: module.AccessPage,
  }))
)
const ClusterConfigurationsPage = lazy(() =>
  import("@/pages/cluster-configurations-page").then((module) => ({
    default: module.ClusterConfigurationsPage,
  }))
)

export function App() {
  useAppearance()
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={300}>
        <BrowserRouter>
          <RouteTitle />
          <SessionLifecycle />
          <Suspense
            fallback={
              <div role="status" className="p-8 text-xs text-muted-foreground">
                正在加载工作空间…
              </div>
            }
          >
            <Routes>
              <Route path="login" element={<LoginPage />} />
              <Route element={<RequireSession />}>
                <Route element={<ConsoleLayout />}>
                  <Route
                    index
                    element={<Navigate to="/design-system" replace />}
                  />
                  <Route path="design-system" element={<DesignSystemPage />} />
                  <Route path="access" element={<AccessPage />} />
                  <Route
                    path="clusters"
                    element={<ClusterConfigurationsPage />}
                  />
                  <Route
                    path="*"
                    element={
                      <div className="space-y-4 p-10">
                        <h1 className="text-xl font-semibold">页面不存在</h1>
                        <p className="text-muted-foreground">
                          请通过左侧导航访问可用功能。
                        </p>
                        <Button asChild>
                          <Link to="/design-system">返回设计系统</Link>
                        </Button>
                      </div>
                    }
                  />
                </Route>
              </Route>
            </Routes>
          </Suspense>
        </BrowserRouter>
        <Toaster richColors closeButton />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

function RouteTitle() {
  const { pathname } = useLocation()
  useEffect(() => {
    const title =
      pathname === "/login"
        ? "登录"
        : pathname === "/clusters"
          ? "集群配置"
          : pathname === "/access"
            ? "我的权限"
            : "设计系统"
    document.title = `${title} · Slurm Console`
  }, [pathname])
  return null
}
