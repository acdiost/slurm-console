import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Outlet, useLocation } from "react-router"
import {
  Activity,
  ScrollText,
  ArrowUpRight,
  Boxes,
  ChevronRight,
  CircleHelp,
  Cpu,
  Database,
  FlaskConical,
  Layers3,
  LayoutDashboard,
  Globe2,
  LogOut,
  Menu,
  Monitor,
  Moon,
  Network,
  Palette,
  PanelLeftClose,
  Server,
  Settings2,
  ShieldCheck,
  Sun,
  Users,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { usePreferences, type Theme } from "@/stores/preferences"
import { logout, useAuth } from "@/stores/auth"
import { api } from "@/services/api"
import { ClusterNavLink as NavLink } from "@/features/clusters/cluster-navigation"

const sections = [
  {
    label: "资源与调度",
    items: [{ label: "CPU / GPU 资源", icon: Cpu }],
  },
  {
    label: "身份与策略",
    items: [],
  },
  {
    label: "平台管理",
    items: [
      { label: "监控与统计", icon: Activity },
      { label: "平台设置", icon: Settings2 },
    ],
  },
]

function Navigation({ onNavigate }: { onNavigate?: () => void }) {
  const user = useAuth((state) => state.user)
  const inventory = useQuery({
    queryKey: ["cluster-inventory"],
    queryFn: ({ signal }) =>
      api<{ totalItems: number }>("/clusters?perPage=1", { signal }),
    staleTime: 30000,
    retry: false,
  })
  const canManage =
    user?.access.platformPermissions.includes("platform.clusters.manage") ??
    false
  const canReadAudit =
    user?.access.platformPermissions.includes("platform.audit.read") ?? false
  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="flex h-17 items-center gap-2.5 px-5">
        <div className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
          <Boxes className="size-4.5" />
        </div>
        <div>
          <span className="text-[15px] font-semibold tracking-tight">
            Slurm Console
          </span>
          <div className="font-mono text-micro tracking-[0.18em] text-muted-foreground">
            COMPUTE OPERATIONS
          </div>
        </div>
      </div>
      <div className="mx-3 mb-5 flex items-center gap-2 rounded-md border bg-background/60 px-3 py-2.5 text-xs text-muted-foreground">
        <Database className="size-3.5" />
        <span className="flex-1">
          {inventory.isPending
            ? "正在读取集群…"
            : inventory.isError
              ? "集群信息暂不可用"
              : inventory.data?.totalItems
                ? `${inventory.data.totalItems} 个已登记集群`
                : "暂无可见集群"}
        </span>
        <span className="size-1.5 rounded-full bg-muted-foreground/50" />
      </div>
      <nav aria-label="主导航" className="flex-1 space-y-5 px-3">
        <div>
          <p className="nav-label">工作空间</p>
          <NavLink
            to="/fleet"
            onClick={onNavigate}
            className={({ isActive }) =>
              `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
            }
          >
            <Globe2 />
            <span>集群工作台</span>
          </NavLink>
          <NavLink
            to="/overview"
            onClick={onNavigate}
            className={({ isActive }) =>
              `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
            }
          >
            <LayoutDashboard />
            <span>集群总览</span>
          </NavLink>
          <NavLink
            to="/design-system"
            onClick={onNavigate}
            className={({ isActive }) =>
              `nav-item mt-1 ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
            }
          >
            <Palette />
            <span>设计系统</span>
            <Badge
              variant="outline"
              className="ml-auto border-primary/20 text-micro text-primary"
            >
              MVP
            </Badge>
          </NavLink>
        </div>
        {sections.map((section) => (
          <div key={section.label}>
            <p className="nav-label">{section.label}</p>
            {section.label === "平台管理" && canManage && (
              <NavLink
                to="/clusters"
                onClick={onNavigate}
                className={({ isActive }) =>
                  `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
                }
              >
                <Network />
                <span>集群配置</span>
              </NavLink>
            )}
            {section.label === "资源与调度" && (
              <NavLink
                to="/jobs"
                onClick={onNavigate}
                className={({ isActive }) =>
                  `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
                }
              >
                <Layers3 />
                <span>作业管理</span>
              </NavLink>
            )}
            {section.label === "资源与调度" && (
              <NavLink
                to="/nodes"
                onClick={onNavigate}
                className={({ isActive }) =>
                  `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
                }
              >
                <Server />
                <span>节点管理</span>
              </NavLink>
            )}
            {section.label === "资源与调度" && (
              <NavLink
                to="/partitions"
                onClick={onNavigate}
                className={({ isActive }) =>
                  `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
                }
              >
                <Network />
                <span>分区查询</span>
              </NavLink>
            )}
            {section.label === "身份与策略" && (
              <NavLink
                to="/accounts"
                onClick={onNavigate}
                className={({ isActive }) =>
                  `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
                }
              >
                <ShieldCheck />
                <span>账户与 Association</span>
              </NavLink>
            )}
            {section.label === "身份与策略" && (
              <NavLink
                to="/linux-users"
                onClick={onNavigate}
                className={({ isActive }) =>
                  `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
                }
              >
                <Users />
                <span>Linux 用户</span>
              </NavLink>
            )}
            {section.label === "身份与策略" && (
              <NavLink
                to="/policies"
                onClick={onNavigate}
                className={({ isActive }) =>
                  `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
                }
              >
                <Settings2 />
                <span>QOS 与 Fairshare</span>
              </NavLink>
            )}
            {section.label === "平台管理" && canReadAudit && (
              <NavLink
                to="/audits"
                onClick={onNavigate}
                className={({ isActive }) =>
                  `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
                }
              >
                <ScrollText />
                <span>操作审计</span>
              </NavLink>
            )}
            {section.items.map(({ label, icon: Icon }) => (
              <button key={label} disabled className="nav-item">
                <Icon />
                <span>{label}</span>
              </button>
            ))}
          </div>
        ))}
        <NavLink
          to="/access"
          onClick={onNavigate}
          className={({ isActive }) =>
            `nav-item ${isActive ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`
          }
        >
          <ShieldCheck />
          <span>我的权限</span>
        </NavLink>
      </nav>
      <div className="m-3 mt-8 rounded-lg border bg-background/60 p-3">
        <div className="mb-2 flex items-center gap-2 text-xs font-medium">
          <FlaskConical className="size-3.5 text-primary" />
          MVP 进度
          <span className="ml-auto font-mono text-2xs text-muted-foreground">
            11 / 11
          </span>
        </div>
        <div className="h-1 overflow-hidden rounded-full bg-muted">
          <div className="h-full w-full bg-primary" />
        </div>
        <p className="mt-2 text-2xs leading-relaxed text-muted-foreground">
          阶段一 · MVP 已完成
          <br />
          V1 · 集群、账户与调度策略管理已就绪
        </p>
      </div>
      <div className="flex items-center gap-2.5 border-t p-4">
        <div className="flex size-7 items-center justify-center rounded-full border bg-background text-2xs font-medium">
          {user?.name.slice(0, 1) ||
            user?.email.slice(0, 1).toUpperCase() ||
            "U"}
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-xs font-medium">
            {user?.name || "平台用户"}
          </p>
          <p
            className="truncate text-2xs text-muted-foreground"
            title={user?.email}
          >
            {user?.email}
          </p>
        </div>
        <Button
          variant="ghost"
          size="icon"
          aria-label="退出登录"
          onClick={() => logout()}
        >
          <LogOut />
        </Button>
      </div>
    </div>
  )
}

export function ConsoleLayout() {
  const { pathname } = useLocation()
  const [mobileOpen, setMobileOpen] = useState(false)
  const { theme, setTheme } = usePreferences()
  const ThemeIcon = theme === "light" ? Sun : theme === "dark" ? Moon : Monitor
  return (
    <div className="min-h-svh bg-background">
      <a
        href="#main-content"
        className="sr-only fixed top-2 left-2 z-50 rounded bg-primary p-2 text-primary-foreground focus:not-sr-only"
      >
        跳转到主要内容
      </a>
      <aside className="fixed inset-y-0 left-0 hidden w-57 border-r bg-sidebar lg:block">
        <Navigation />
      </aside>
      <div className="lg:pl-57">
        <header className="flex h-14 items-center gap-3 border-b bg-card px-4 sm:px-7">
          <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
            <SheetTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                aria-label="打开导航"
                className="lg:hidden"
              >
                <Menu />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-64 gap-0 p-0">
              <SheetHeader className="sr-only">
                <SheetTitle>主导航</SheetTitle>
                <SheetDescription>
                  Slurm Console 工作空间与功能导航
                </SheetDescription>
              </SheetHeader>
              <Navigation onNavigate={() => setMobileOpen(false)} />
            </SheetContent>
          </Sheet>
          <PanelLeftClose
            className="hidden size-4 text-muted-foreground lg:block"
            aria-hidden="true"
          />
          <div className="flex items-center gap-2 text-xs">
            <span className="hidden text-muted-foreground sm:inline">
              工作空间
            </span>
            <ChevronRight className="hidden size-3 text-muted-foreground sm:inline" />
            <span>
              {pathname === "/policies"
                ? "QOS 与 Fairshare"
                : pathname === "/accounts"
                  ? "账户与 Association"
                  : pathname === "/linux-users"
                    ? "Linux 系统用户"
                    : pathname === "/fleet"
                      ? "集群工作台"
                      : pathname === "/audits"
                        ? "操作审计"
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
                                    : "设计系统"}
            </span>
          </div>
          <div className="ml-auto flex items-center gap-2 sm:gap-3">
            <Badge
              variant="outline"
              className="hidden gap-1.5 font-normal sm:flex"
            >
              <span className="size-1.5 rounded-full bg-[var(--blue-9)]" />
              已登录
            </Badge>
            <div className="hidden h-4 w-px bg-border sm:block" />
            <ThemeIcon
              className="size-3.5 text-muted-foreground"
              aria-hidden="true"
            />
            <Select
              value={theme}
              onValueChange={(value) => setTheme(value as Theme)}
            >
              <SelectTrigger aria-label="界面主题" className="w-25">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="system">跟随系统</SelectItem>
                <SelectItem value="light">浅色模式</SelectItem>
                <SelectItem value="dark">深色模式</SelectItem>
              </SelectContent>
            </Select>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="查看设计规范"
                  asChild
                >
                  <a
                    href={
                      pathname === "/design-system"
                        ? "#design-notes"
                        : "/design-system#design-notes"
                    }
                  >
                    <CircleHelp />
                  </a>
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                查看设计规范 <ArrowUpRight className="ml-1 inline size-3" />
              </TooltipContent>
            </Tooltip>
          </div>
        </header>
        <main id="main-content" tabIndex={-1}>
          <Outlet />
        </main>
      </div>
    </div>
  )
}
