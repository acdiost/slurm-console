import { useState, type ReactNode } from "react"
import { ClientResponseError } from "pocketbase"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@/components/ui/alert-dialog"
export function errorMessage(e: unknown) {
  return e instanceof ClientResponseError
    ? String(e.response?.error?.message || "操作未完成，请刷新后核对。")
    : "服务或返回格式不可用，请刷新后核对。"
}
export function Confirmation({
  title,
  phrase,
  children,
  busy,
  onCancel,
  onConfirm,
}: {
  title: string
  phrase: string
  children: ReactNode
  busy: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  const [text, setText] = useState("")
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onCancel()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-3 text-sm leading-6">{children}</div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-2">
          <Label htmlFor="settings-confirmation">输入 {phrase} 确认</Label>
          <Input
            id="settings-confirmation"
            value={text}
            autoComplete="off"
            onChange={(e) => setText(e.target.value)}
            disabled={busy}
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy} onClick={onCancel}>
            返回修改
          </AlertDialogCancel>
          <Button disabled={busy || text !== phrase} onClick={onConfirm}>
            {busy ? "正在提交…" : "确认提交"}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
export const textareaClass =
  "min-h-24 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 disabled:opacity-50"
