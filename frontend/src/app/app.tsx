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
const PoliciesPage = lazy(() =>
  import("@/pages/policies-page").then((module) => ({
    default: module.PoliciesPage,
  }))
)
const AccountingPage = lazy(() =>
  import("@/pages/accounting-page").then((module) => ({
    default: module.AccountingPage,
  }))
)
const LinuxUsersPage = lazy(() =>
  import("@/pages/linux-users-page").then((module) => ({
    default: module.LinuxUsersPage,
  }))
)
const FleetPage = lazy(() =>
  import("@/pages/fleet-page").then((module) => ({ default: module.FleetPage }))
)
const LoginPage = lazy(() =>
  import("@/pages/login-page").then((module) => ({ default: module.LoginPage }))
)

const DesignSystemPage = lazy(() =>
  import("@/pages/design-system-page").then((module) => ({
    default: module.DesignSystemPage,
  }))
)
const AuditsPage = lazy(() =>
  import("@/pages/audits-page").then((module) => ({
    default: module.AuditsPage,
  }))
)
const PartitionsPage = lazy(() =>
  import("@/pages/partitions-page").then((module) => ({
    default: module.PartitionsPage,
  }))
)
const NodesPage = lazy(() =>
  import("@/pages/nodes-page").then((module) => ({ default: module.NodesPage }))
)
const JobsPage = lazy(() =>
  import("@/pages/jobs-page").then((module) => ({ default: module.JobsPage }))
)
const OverviewPage = lazy(() =>
  import("@/pages/overview-page").then((module) => ({
    default: module.OverviewPage,
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
                  <Route index element={<Navigate to="/fleet" replace />} />
                  <Route path="fleet" element={<FleetPage />} />
                  <Route path="linux-users" element={<LinuxUsersPage />} />
                  <Route path="accounts" element={<AccountingPage />} />
                  <Route path="policies" element={<PoliciesPage />} />
                  <Route path="overview" element={<OverviewPage />} />
                  <Route path="jobs" element={<JobsPage />} />
                  <Route path="nodes" element={<NodesPage />} />
                  <Route path="partitions" element={<PartitionsPage />} />
                  <Route path="audits" element={<AuditsPage />} />
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
                          <Link to="/fleet">返回集群工作台</Link>
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
      pathname === "/policies"
        ? "QOS 与 Fairshare"
        : pathname === "/accounts"
          ? "账户与 Association"
          : pathname === "/linux-users"
            ? "Linux 系统用户"
            : pathname === "/fleet"
              ? "集群工作台"
              : pathname === "/audits"
                ? "操作审计"
                : pathname === "/login"
                  ? "登录"
                  : pathname === "/partitions"
                    ? "分区查询"
                    : pathname === "/nodes"
                      ? "节点管理"
                      : pathname === "/jobs"
                        ? "作业管理"
                        : pathname === "/overview"
                          ? "集群总览"
                          : pathname === "/clusters"
                            ? "集群配置"
                            : pathname === "/access"
                              ? "我的权限"
                              : "设计系统"
    document.title = `${title} · Slurm Console`
  }, [pathname])
  return null
}
