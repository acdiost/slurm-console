import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@/components/ui/alert-dialog"
import { ConfigurationDiff } from "./diff-table"
import type { Preview } from "./schema"
export function ConfigurationReview({
  preview,
  pending,
  error,
  onCancel,
  onConfirm,
}: {
  preview: Preview
  pending: boolean
  error: string
  onCancel: () => void
  onConfirm: () => void
}) {
  return (
    <AlertDialog
      open
      onOpenChange={(v) => {
        if (!v && !pending) onCancel()
      }}
    >
      <AlertDialogContent className="max-h-[85vh] overflow-y-auto data-[size=default]:max-w-[calc(100vw-2rem)] data-[size=default]:sm:max-w-4xl">
        <AlertDialogHeader>
          <AlertDialogTitle>
            {preview.currentRevision === null
              ? "确认创建集群配置"
              : "确认保存配置变更"}
          </AlertDialogTitle>
          <AlertDialogDescription>
            请核对服务端规范化后的字段。保存时仍会检查权限和版本，冲突时需关闭编辑并刷新后重新预览。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <p className="text-sm">
          {preview.currentRevision === null
            ? "新建"
            : `当前 v${preview.currentRevision}`}{" "}
          → v{preview.nextRevision} · {preview.after.name}
        </p>
        <ConfigurationDiff fields={preview.fields} />
        <p className="text-xs text-muted-foreground">
          对比只包含平台连接字段；服务端
          endpoint、凭据、证书文件和连接检测结果不在快照中。
        </p>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>返回编辑</AlertDialogCancel>
          <AlertDialogAction
            disabled={pending}
            onClick={(e) => {
              e.preventDefault()
              onConfirm()
            }}
          >
            {pending ? "正在保存…" : "确认保存"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
