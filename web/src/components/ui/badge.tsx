import { cn } from "@/lib/utils";

type Tone = "default" | "prod" | "ok" | "bad" | "warn";

export function Badge({ tone = "default", children }: { tone?: Tone; children: React.ReactNode }) {
  const tones: Record<Tone, string> = {
    default: "border-border text-muted",
    prod: "border-red-500/50 text-red-600 dark:text-red-400",
    ok: "border-green-500/50 text-green-700 dark:text-green-400",
    bad: "border-red-500/50 text-red-600 dark:text-red-400",
    warn: "border-amber-500/60 text-amber-700 dark:text-amber-400",
  };
  return <span className={cn("inline-block rounded border px-1.5 py-0.5 text-xs", tones[tone])}>{children}</span>;
}
