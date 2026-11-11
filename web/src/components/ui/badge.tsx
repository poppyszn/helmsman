import * as React from "react";
import { cn } from "@/lib/utils";

export type BadgeTone = "default" | "prod" | "ok" | "bad" | "warn" | "cyan" | "indigo";

export function Badge({
  tone = "default",
  className,
  children,
}: {
  tone?: BadgeTone;
  className?: string;
  children: React.ReactNode;
}) {
  const tones: Record<BadgeTone, string> = {
    default: "bg-zinc-800/80 text-zinc-300",
    prod: "bg-rose-500/15 text-rose-300 font-semibold",
    ok: "bg-emerald-500/15 text-emerald-300 font-medium",
    bad: "bg-rose-500/15 text-rose-300 font-medium",
    warn: "bg-amber-500/15 text-amber-300 font-medium",
    cyan: "bg-cyan-500/15 text-cyan-300 font-medium",
    indigo: "bg-indigo-500/15 text-indigo-300 font-medium",
  };

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-mono tracking-tight select-none",
        tones[tone],
        className
      )}
    >
      {children}
    </span>
  );
}
