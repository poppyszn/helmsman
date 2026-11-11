import { Badge } from "@/components/ui/badge";
import type { Status } from "@/lib/api";

const tone = { healthy: "ok", degraded: "warn", down: "bad", unknown: "default" } as const;

// The word is always shown, so status never relies on colour alone.
export function StatusBadge({ status }: { status: Status }) {
  return <Badge tone={tone[status]}>{status}</Badge>;
}
