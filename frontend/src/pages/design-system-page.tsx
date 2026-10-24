import {
  ArrowDown,
  ArrowUpRight,
  Check,
  CheckCheck,
  CircleDashed,
  Code2,
  Component,
  Layers3,
  LoaderCircle,
  Palette,
  ShieldCheck,
  Terminal,
  Workflow,
} from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Skeleton } from "@/components/ui/skeleton"
import { Progress } from "@/components/ui/progress"
import { Separator } from "@/components/ui/separator"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { SlurmStatusBadge } from "@/components/slurm/status-badge"
import { OperationConfirmDialog } from "@/components/slurm/operation-confirm-dialog"
import { JobTablePreview } from "@/features/design-system/job-table-preview"
import { SettingsFormPreview } from "@/features/design-system/settings-form-preview"
import { usePreferences, type Accent } from "@/stores/preferences"

const statusGroups = [
  {
    label: "作业状态",
    kind: "job" as const,
    states: ["RUNNING", "PENDING", "COMPLETED", "FAILED", "CANCELLED"],
  },
  {
    label: "节点状态",
    kind: "node" as const,
    states: ["IDLE", "ALLOCATED", "MIXED", "DRAIN", "DOWN"],
  },
]
const colors = [
  { label: "科研红", variable: "red", hint: "品牌 / 风险" },
  { label: "Slurm 蓝", variable: "blue", hint: "品牌 / 信息" },
  { label: "运行绿", variable: "grass", hint: "运行 / 正常" },
  { label: "等待琥珀", variable: "amber", hint: "等待 / 维护" },
  { label: "中性灰", variable: "slate", hint: "取消 / 未知" },
]

export function DesignSystemPage() {
  const { accent, setAccent } = usePreferences()
  return (
    <div className="mx-auto max-w-400 space-y-6 px-4 py-6 sm:px-7 sm:py-7 xl:px-9">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="mb-2 flex items-center gap-2">
            <span className="font-mono text-2xs tracking-[0.16em] text-muted-foreground">
              FOUNDATION / 01
            </span>
            <Badge variant="secondary" className="text-micro font-normal">
              Mira
            </Badge>
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">
            设计系统
            <span className="ml-3 font-mono text-sm font-normal text-muted-foreground">
              Design System
            </span>
          </h1>
          <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
            清晰、一致、专注。为 Slurm 集群运维建立统一的界面语言。
          </p>
        </div>
        <Button variant="outline" asChild>
          <a href="#component-preview">
            <Component />
            浏览组件
            <ArrowDown />
          </a>
        </Button>
      </div>
      <section className="relative overflow-hidden rounded-xl border bg-card p-5 sm:p-7">
        <div className="hero-grid pointer-events-none absolute inset-y-0 right-0 w-1/2 opacity-50" />
        <div className="relative flex flex-wrap items-center justify-between gap-6">
          <div className="max-w-xl">
            <div className="mb-3 flex items-center gap-2 font-mono text-2xs tracking-wider text-primary">
              <span className="size-1.5 rounded-full bg-primary" />
              BUILT FOR COMPUTE OPERATIONS
            </div>
            <h2 className="text-xl font-semibold tracking-tight sm:text-2xl">
              让复杂算力，井然有序。
            </h2>
            <p className="mt-3 max-w-lg text-xs leading-6 text-muted-foreground">
              以高信息密度呈现资源与调度，用明确的状态和交互辅助每一次运维决策。
              <br className="hidden sm:block" />
              从统一的设计基础开始，逐步连接集群、作业与计算节点。
            </p>
            <div className="mt-4 flex flex-wrap gap-2">
              {["React + TypeScript", "shadcn/ui · Mira", "Radix Colors"].map(
                (item) => (
                  <Badge
                    key={item}
                    variant="outline"
                    className="bg-background/80 font-mono text-micro font-normal text-muted-foreground"
                  >
                    {item}
                  </Badge>
                )
              )}
            </div>
          </div>
          <div className="relative flex items-center gap-3 rounded-xl border bg-background/90 p-4 sm:mr-3">
            <div className="grid grid-cols-3 gap-1.5" aria-hidden="true">
              {Array.from({ length: 9 }, (_, i) => (
                <div
                  key={i}
                  className="flex size-7 items-center justify-center rounded border border-primary/15 bg-primary/5"
                >
                  <div
                    className={`size-1.5 rounded-sm ${i === 7 ? "bg-[var(--amber-9)]" : i === 2 ? "bg-primary/25" : "bg-primary"}`}
                  />
                </div>
              ))}
            </div>
            <div className="pl-1">
              <p className="font-mono text-xs font-semibold">One console.</p>
              <p className="mt-1 font-mono text-xs text-muted-foreground">
                Every cluster.
              </p>
              <Separator className="my-2" />
              <span className="flex items-center gap-1 text-2xs text-primary">
                <CheckCheck className="size-3" />
                统一的设计基础
              </span>
            </div>
          </div>
        </div>
      </section>
      <div className="grid gap-3 sm:grid-cols-3">
        {[
          {
            icon: Palette,
            title: "语义化色彩",
            desc: "双品牌色 · 浅色与深色主题",
            code: "01",
          },
          {
            icon: Layers3,
            title: "面向运维的密度",
            desc: "标准布局 · 紧凑数据表格",
            code: "02",
          },
          {
            icon: ShieldCheck,
            title: "明确的操作反馈",
            desc: "输入校验 · 二次确认 · 状态提示",
            code: "03",
          },
        ].map(({ icon: Icon, title, desc, code }) => (
          <div
            key={code}
            className="flex items-center gap-3 rounded-lg border bg-card px-4 py-4"
          >
            <div className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/40">
              <Icon className="size-4 text-muted-foreground" />
            </div>
            <div>
              <h2 className="text-xs font-semibold">{title}</h2>
              <p className="mt-1 text-2xs text-muted-foreground">{desc}</p>
            </div>
            <span className="ml-auto self-start font-mono text-2xs text-muted-foreground/50">
              {code}
            </span>
          </div>
        ))}
      </div>
      <div
        id="component-preview"
        className="grid scroll-mt-6 items-start gap-5 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)]"
      >
        <section className="panel">
          <div className="panel-heading">
            <div>
              <div className="section-eyebrow">COLOR & THEME</div>
              <h2>色彩与主题</h2>
            </div>
            <ToggleGroup
              type="single"
              value={accent}
              onValueChange={(value) => {
                if (value) setAccent(value as Accent)
              }}
              variant="outline"
              size="sm"
              aria-label="品牌配色"
            >
              <ToggleGroupItem value="blue" aria-label="Slurm 蓝">
                <span className="size-2 rounded-full bg-[var(--blue-9)]" />
                Slurm 蓝
              </ToggleGroupItem>
              <ToggleGroupItem value="red" aria-label="科研红">
                <span className="size-2 rounded-full bg-[var(--red-9)]" />
                科研红
              </ToggleGroupItem>
            </ToggleGroup>
          </div>
          <div className="grid grid-cols-5 gap-2 p-5">
            {colors.map(({ label, variable, hint }) => (
              <div key={variable}>
                <div className="flex h-16 overflow-hidden rounded-md border">
                  {[3, 5, 9, 11].map((step) => (
                    <span
                      key={step}
                      className="flex-1"
                      style={{ backgroundColor: `var(--${variable}-${step})` }}
                    />
                  ))}
                </div>
                <p className="mt-2 text-caption font-medium">{label}</p>
                <p className="mt-1 text-micro text-muted-foreground">{hint}</p>
              </div>
            ))}
          </div>
          <div className="flex items-center gap-2 border-t bg-muted/15 px-5 py-3 text-2xs text-muted-foreground">
            <Code2 className="size-3.5" />
            颜色由语义 Token 管理，主题切换保持状态含义一致。
          </div>
        </section>
        <section className="panel">
          <div className="panel-heading">
            <div>
              <div className="section-eyebrow">SLURM STATUS</div>
              <h2>状态语义</h2>
            </div>
            <span className="component-tag">Status Badge</span>
          </div>
          <div className="space-y-5 p-5">
            {statusGroups.map(({ label, kind, states }) => (
              <div key={label}>
                <p className="mb-2.5 text-2xs text-muted-foreground">{label}</p>
                <div className="flex flex-wrap gap-2">
                  {states.map((state) => (
                    <SlurmStatusBadge key={state} kind={kind} state={state} />
                  ))}
                </div>
              </div>
            ))}
          </div>
          <div className="border-t bg-muted/15 px-5 py-3 text-2xs text-muted-foreground">
            颜色、文字与状态点共同表达含义，未知状态保留原始代码。
          </div>
        </section>
      </div>
      <div className="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
        <Terminal className="size-3.5" />
        <span className="font-medium text-foreground">组件预览</span>
        <span className="mx-1 text-border">/</span>
        以下为固定示例数据，尚未接入真实集群。
      </div>
      <JobTablePreview />
      <div className="grid items-start gap-5 lg:grid-cols-3">
        <section className="panel">
          <div className="panel-heading">
            <div>
              <div className="section-eyebrow">ACTIONS</div>
              <h2>操作与反馈</h2>
            </div>
            <Workflow className="size-4 text-muted-foreground" />
          </div>
          <div className="space-y-5 p-5">
            <div>
              <p className="mb-2 text-2xs text-muted-foreground">操作层级</p>
              <div className="flex flex-wrap gap-2">
                <Button onClick={() => toast.success("主要操作反馈示例")}>
                  <Check />
                  主要操作
                </Button>
                <Button
                  variant="outline"
                  onClick={() => toast.info("次要操作反馈示例")}
                >
                  次要操作
                </Button>
                <Button variant="ghost" disabled>
                  不可用
                </Button>
              </div>
            </div>
            <Separator />
            <div>
              <p className="mb-2 text-2xs text-muted-foreground">
                状态变更前的明确确认
              </p>
              <OperationConfirmDialog
                onConfirm={() =>
                  toast.success("演示已确认", {
                    description: "未调用任何集群管理接口。",
                  })
                }
              />
              <p className="mt-2 text-2xs leading-5 text-muted-foreground">
                通过确认组件呈现目标与影响，降低误操作。
              </p>
            </div>
          </div>
        </section>
        <section className="panel">
          <div className="panel-heading">
            <div>
              <div className="section-eyebrow">FORM VALIDATION</div>
              <h2>表单与输入</h2>
            </div>
            <span className="component-tag">RHF + Zod</span>
          </div>
          <div className="p-5">
            <SettingsFormPreview />
          </div>
        </section>
        <section className="panel">
          <div className="panel-heading">
            <div>
              <div className="section-eyebrow">SYSTEM STATES</div>
              <h2>加载与空状态</h2>
            </div>
            <CircleDashed className="size-4 text-muted-foreground" />
          </div>
          <div className="p-5">
            <Tabs defaultValue="empty">
              <TabsList className="mb-4 w-full">
                <TabsTrigger value="empty">空状态</TabsTrigger>
                <TabsTrigger value="loading">加载中</TabsTrigger>
                <TabsTrigger value="error">异常</TabsTrigger>
              </TabsList>
              <TabsContent value="empty">
                <div className="flex min-h-28 flex-col items-center justify-center rounded-md border border-dashed p-3 text-center">
                  <Layers3 className="mb-2 size-5 text-muted-foreground/60" />
                  <p className="text-xs font-medium">还没有集群</p>
                  <p className="mt-1 text-2xs text-muted-foreground">
                    完成认证与配置后，开始连接集群。
                  </p>
                </div>
              </TabsContent>
              <TabsContent value="loading">
                <div
                  role="status"
                  className="min-h-28 space-y-3 rounded-md border p-4"
                >
                  <span className="flex items-center gap-2 text-2xs text-muted-foreground">
                    <LoaderCircle className="size-3 animate-spin motion-reduce:animate-none" />
                    加载状态示例
                  </span>
                  <Skeleton className="h-3 w-3/4" />
                  <Skeleton className="h-3 w-full" />
                  <Skeleton className="h-3 w-1/2" />
                </div>
              </TabsContent>
              <TabsContent value="error">
                <div
                  role="alert"
                  className="flex min-h-28 flex-col justify-center rounded-md border border-destructive/20 bg-destructive/5 p-4"
                >
                  <p className="text-xs font-medium text-destructive">
                    暂时无法连接集群
                  </p>
                  <p className="mt-1 text-2xs leading-5 text-muted-foreground">
                    异常展示示例，请检查服务状态和连接配置。
                  </p>
                </div>
              </TabsContent>
            </Tabs>
          </div>
        </section>
      </div>
      <section id="design-notes" className="panel scroll-mt-6">
        <div className="flex flex-wrap items-center justify-between gap-4 px-5 py-4">
          <div>
            <h2 className="text-xs font-semibold">
              一致的基础，可扩展的工作空间
            </h2>
            <p className="mt-1.5 text-2xs text-muted-foreground">
              组件复用 shadcn/ui Mira · Lucide 图标 · 4px 间距基准 ·
              系统字体回退 · 键盘可访问
            </p>
          </div>
          <div className="flex w-46 items-center gap-3">
            <Progress
              value={300 / 11}
              aria-label="阶段一完成进度，11 项中已完成 3 项"
              className="h-1.5"
            />
            <span className="shrink-0 font-mono text-2xs text-muted-foreground">
              3 / 11
            </span>
          </div>
        </div>
      </section>
      <footer className="flex flex-wrap justify-between gap-2 pb-1 text-2xs text-muted-foreground">
        <span>
          Slurm Console <span className="mx-1.5 text-border">/</span> Enterprise
          Compute Management
        </span>
        <span className="flex items-center gap-1">
          UI Foundation · v0.1.0
          <ArrowUpRight className="size-3" />
        </span>
      </footer>
    </div>
  )
}
