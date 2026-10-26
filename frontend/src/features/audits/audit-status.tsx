import { Badge } from "@/components/ui/badge"
import { statuses } from "./schema"
export function AuditStatus({ status }: { status: string }) {
  return (
    <Badge
      variant="outline"
      className={
        status === "failed"
          ? "border-destructive/30 text-destructive"
          : ["unknown", "accepted"].includes(status)
            ? "border-warning/30 text-warning"
            : ["succeeded", "confirmed"].includes(status)
              ? "border-success/30 text-success"
              : ""
      }
    >
      {statuses[status] || status}
    </Badge>
  )
}
