import { cn } from "@/lib/utils";

export function Badge({ tone = "default", children }: { tone?: "default" | "prod" | "ok" | "bad"; children: React.ReactNode }) {
  const tones = {
    default: "border-border text-muted",
    prod: "border-red-500/50 text-red-600 dark:text-red-400",
    ok: "border-green-500/50 text-green-700 dark:text-green-400",
    bad: "border-red-500/50 text-red-600 dark:text-red-400",
  };
  return <span className={cn("inline-block rounded border px-1.5 py-0.5 text-xs", tones[tone])}>{children}</span>;
}
