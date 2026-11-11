import { CheckCircle2, Circle, Loader2, MinusCircle, XCircle } from "lucide-react";
import { cn } from "@/lib/utils";
import type { RunStatus } from "@/lib/api";

const map: Record<RunStatus, { label: string; dot: string; text: string }> = {
  pending_approval: { label: "needs approval", dot: "bg-amber-400 shadow-[0_0_8px_rgba(251,191,36,0.4)]", text: "text-amber-300" },
  dispatching: { label: "starting", dot: "bg-indigo-400 shadow-[0_0_8px_rgba(129,140,248,0.4)]", text: "text-indigo-300" },
  queued: { label: "queued", dot: "bg-indigo-400 shadow-[0_0_8px_rgba(129,140,248,0.4)]", text: "text-indigo-300" },
  in_progress: { label: "running", dot: "bg-indigo-400 shadow-[0_0_8px_rgba(129,140,248,0.4)]", text: "text-indigo-300" },
  waiting: { label: "waiting for GitHub approval", dot: "bg-amber-400 shadow-[0_0_8px_rgba(251,191,36,0.4)]", text: "text-amber-300" },
  succeeded: { label: "succeeded", dot: "bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.4)]", text: "text-zinc-200" },
  failed: { label: "failed", dot: "bg-rose-500 shadow-[0_0_8px_rgba(244,63,94,0.4)]", text: "text-rose-300" },
  cancelled: { label: "cancelled", dot: "bg-zinc-500", text: "text-zinc-400" },
  rejected: { label: "rejected", dot: "bg-zinc-500", text: "text-zinc-400" },
  unknown: { label: "unknown", dot: "bg-zinc-600", text: "text-zinc-400" },
};

export function RunStatusBadge({ status }: { status: RunStatus | string }) {
  const c = map[status as RunStatus] ?? map.unknown;
  return (
    <span className="inline-flex items-center gap-2 select-none font-mono text-xs font-medium whitespace-nowrap">
      <span className={cn("inline-block size-2 rounded-full shrink-0", c.dot)} />
      <span className={c.text}>{c.label}</span>
    </span>
  );
}

/** Icon for a step or job: what happened, not just a colour. */
export function StepIcon({ status, conclusion }: { status: string; conclusion: string | null }) {
  const cls = "size-3.5 shrink-0";
  if (status === "completed") {
    if (conclusion === "success" || conclusion === "skipped" || conclusion === "neutral")
      return <CheckCircle2 className={cn(cls, "text-emerald-400")} strokeWidth={2} />;
    if (conclusion === "cancelled") return <MinusCircle className={cn(cls, "text-zinc-500")} strokeWidth={2} />;
    return <XCircle className={cn(cls, "text-rose-400")} strokeWidth={2} />;
  }
  if (status === "in_progress") return <Loader2 className={cn(cls, "text-indigo-300 animate-spin")} strokeWidth={2} />;
  return <Circle className={cn(cls, "text-zinc-600")} strokeWidth={2} />;
}
