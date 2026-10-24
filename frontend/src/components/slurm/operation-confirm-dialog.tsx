import { useState } from "react"
import { ShieldAlert } from "lucide-react"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export function OperationConfirmDialog({
  onConfirm,
}: {
  onConfirm: () => void
}) {
  const [confirmation, setConfirmation] = useState("")
  return (
    <AlertDialog onOpenChange={() => setConfirmation("")}>
      <AlertDialogTrigger asChild>
        <Button variant="destructive">
          <ShieldAlert />
          操作确认示例
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>确认演示操作</AlertDialogTitle>
          <AlertDialogDescription>
            这是操作确认组件的交互演示。确认后仅显示提示，不会调用 Slurm
            或修改任何资源。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-2">
          <Label htmlFor="operation-confirmation">请输入“演示”以继续</Label>
          <Input
            id="operation-confirmation"
            autoComplete="off"
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>返回</AlertDialogCancel>
          <AlertDialogAction
            disabled={confirmation !== "演示"}
            onClick={onConfirm}
          >
            确认演示
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
