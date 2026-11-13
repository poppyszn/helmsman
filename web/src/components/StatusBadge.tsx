import { cn } from "@/lib/utils";
import type { Status } from "@/lib/api";

type StatusConfig = {
  label: string;
  dotColor: string;
  textColor: string;
};

const statusMap: Record<Status, StatusConfig> = {
  healthy: {
    label: "healthy",
    dotColor: "bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.4)]",
    textColor: "text-zinc-200",
  },
  degraded: {
    label: "degraded",
    dotColor: "bg-amber-400 shadow-[0_0_8px_rgba(251,191,36,0.4)]",
    textColor: "text-amber-300",
  },
  down: {
    label: "down",
    dotColor: "bg-rose-500 shadow-[0_0_8px_rgba(244,63,94,0.4)]",
    textColor: "text-rose-300",
  },
  unknown: {
    label: "unknown",
    dotColor: "bg-zinc-600",
    textColor: "text-zinc-400",
  },
};

export function StatusBadge({
  status,
  size = "default",
  showLabel = true,
}: {
  status: Status;
  size?: "default" | "sm";
  showLabel?: boolean;
}) {
  const config = statusMap[status] ?? statusMap.unknown;

  return (
    <span
      className={cn(
        "inline-flex items-center gap-2 select-none font-mono text-xs font-medium",
        size === "sm" ? "text-xs" : "text-xs"
      )}
    >
      <span className={cn("inline-block h-2 w-2 rounded-full shrink-0 transition-[background-color,box-shadow] duration-500", config.dotColor)} />
      {showLabel && <span className={config.textColor}>{config.label}</span>}
    </span>
  );
}
