import { useEffect, useState } from "react"
import { Link } from "react-router"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Bell } from "lucide-react"
import { Button } from "@/components/ui/button"
import { pb } from "@/services/pocketbase"
import { api } from "@/services/api"
import { useAuth } from "@/stores/auth"
import { notificationPageSchema } from "./schema"
// SSE carries invalidation hints only; counts and resource details always use authorized API reads.
export function parseHints(text: string) {
  if (text.length > 8192) throw new Error("Notification frame too large")
  const frames = text.split("\n\n")
  const rest = frames.pop() || ""
  return {
    rest,
    changed: frames.some((f) => /^event: (?:changed|ready)$/m.test(f)),
  }
}
export function NotificationBell() {
  const user = useAuth((s) => s.user),
    client = useQueryClient(),
    [connected, setConnected] = useState(false)
  const query = useQuery({
    queryKey: ["notifications", user?.id, "unread"],
    enabled: !!user,
    queryFn: async ({ signal }) =>
      notificationPageSchema.parse(
        await api("/notifications?perPage=1", { signal })
      ),
    refetchInterval: 30000,
    retry: false,
  })
  useEffect(() => {
    if (!user) return
    const abort = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    const run = async () => {
      if (!pb.authStore.token) return
      try {
        const response = await fetch("/api/v1/notifications/stream", {
          headers: { Authorization: pb.authStore.token },
          cache: "no-store",
          signal: abort.signal,
        })
        if (!response.ok || !response.body)
          throw new Error("Stream unavailable")
        setConnected(true)
        const reader = response.body.getReader(),
          decoder = new TextDecoder()
        let buffer = ""
        try {
          while (!abort.signal.aborted) {
            const { done, value } = await reader.read()
            if (done) break
            buffer += decoder
              .decode(value, { stream: true })
              .replace(/\r\n/g, "\n")
            const frame = parseHints(buffer)
            buffer = frame.rest
            if (frame.changed) {
              void client.invalidateQueries({
                queryKey: ["notifications", user.id],
              })
              void client.invalidateQueries({
                queryKey: ["notification-rules", user.id],
              })
            }
          }
        } finally {
          await reader.cancel().catch(() => {})
        }
      } catch {
        /* Polling remains available when streaming is interrupted. */
      } finally {
        if (!abort.signal.aborted) {
          setConnected(false)
          timer = setTimeout(() => void run(), 5000)
        }
      }
    }
    void run()
    return () => {
      abort.abort()
      if (timer) clearTimeout(timer)
    }
  }, [user, client])
  const count = query.isError ? undefined : query.data?.unreadCount
  return (
    <Button variant="ghost" size="sm" asChild>
      <Link
        to="/notifications"
        aria-label={
          query.isError
            ? "告警通知暂不可用"
            : `告警通知，${count ?? "正在读取"} 条未读`
        }
        title={connected ? "实时通知已连接" : "每 30 秒刷新通知"}
      >
        <Bell className="size-4" />
        <span className="hidden sm:inline">通知</span>
        {count !== undefined && count > 0 && (
          <span className="rounded-full bg-destructive px-1.5 text-xs text-white">
            {count > 99 ? "99+" : count}
          </span>
        )}
      </Link>
    </Button>
  )
}
