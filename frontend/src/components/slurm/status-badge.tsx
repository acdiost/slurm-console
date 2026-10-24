import { Badge } from "@/components/ui/badge"
import { resolveStatus } from "@/components/slurm/status"

export function SlurmStatusBadge({
  state,
  kind = "job",
}: {
  state: string
  kind?: "job" | "node"
}) {
  const status = resolveStatus(state, kind)
  return (
    <Badge
      variant="outline"
      data-tone={status.tone}
      className="status-badge gap-1.5 font-normal"
      title={status.label}
    >
      <span
        aria-hidden="true"
        className="size-1.5 shrink-0 rounded-full bg-current"
      />
      <span className="font-mono text-2xs">{status.code}</span>
      <span className="sr-only">{status.label}</span>
    </Badge>
  )
}
