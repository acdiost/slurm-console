import { lazy, Suspense } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { BrowserRouter, Link, Navigate, Route, Routes } from "react-router"
import { Toaster } from "@/components/ui/sonner"
import { Button } from "@/components/ui/button"
import { TooltipProvider } from "@/components/ui/tooltip"
import { ConsoleLayout } from "@/app/console-layout"
import { useAppearance } from "@/hooks/use-appearance"

const DesignSystemPage = lazy(() =>
  import("@/pages/design-system-page").then((module) => ({
    default: module.DesignSystemPage,
  }))
)

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 10_000, retry: 1, refetchOnWindowFocus: false },
    mutations: { retry: false },
  },
})

export function App() {
  useAppearance()
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={300}>
        <BrowserRouter>
          <Suspense
            fallback={
              <div role="status" className="p-8 text-xs text-muted-foreground">
                正在加载工作空间…
              </div>
            }
          >
            <Routes>
              <Route element={<ConsoleLayout />}>
                <Route
                  index
                  element={<Navigate to="/design-system" replace />}
                />
                <Route path="design-system" element={<DesignSystemPage />} />
                <Route
                  path="*"
                  element={
                    <div className="space-y-4 p-10">
                      <h1 className="text-xl font-semibold">页面不存在</h1>
                      <p className="text-muted-foreground">
                        当前阶段提供 UI 设计系统预览。
                      </p>
                      <Button asChild>
                        <Link to="/design-system">返回设计系统</Link>
                      </Button>
                    </div>
                  }
                />
              </Route>
            </Routes>
          </Suspense>
        </BrowserRouter>
        <Toaster richColors closeButton />
      </TooltipProvider>
    </QueryClientProvider>
  )
}
