import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Box, Database, ExternalLink, HeartPulse, RotateCw, Rocket, TriangleAlert, type LucideIcon } from "lucide-react";
import { api, formatTime, type TimelineEntry, type TimelineResp } from "@/lib/api";
import { cn } from "@/lib/utils";

const kinds: { id: TimelineEntry["kind"] | "all"; label: string }[] = [
  { id: "all", label: "everything" },
  { id: "deploy", label: "deploys" },
  { id: "run", label: "migrations and seeds" },
  { id: "restart", label: "restarts" },
  { id: "container", label: "container" },
  { id: "health", label: "health" },
];

const icons: Record<TimelineEntry["kind"], LucideIcon> = { deploy: Rocket, run: Database, restart: RotateCw, container: Box, health: HeartPulse };
const dot = { ok: "bg-emerald-400", info: "bg-zinc-500", warn: "bg-amber-400", bad: "bg-rose-500" } as const;

/** What happened to this environment, newest first: deployments, runs, restarts, container and health changes. */
export function TimelinePanel({ app, env, tz }: { app: string; env: string; tz: string }) {
  const q = useQuery<TimelineResp>({ queryKey: ["timeline", app, env], queryFn: () => api(`/api/apps/${app}/envs/${env}/timeline`), refetchInterval: 15_000 });
  const [kind, setKind] = useState<(typeof kinds)[number]["id"]>("all");

  if (q.isLoading) return <div className="py-12 text-center text-xs font-mono text-zinc-500">loading</div>;
  if (q.isError || !q.data) return <div className="py-12 text-center text-xs font-mono text-rose-300">could not load the timeline</div>;

  const all = q.data.entries;
  const shown = kind === "all" ? all : all.filter((e) => e.kind === kind);
  // Group by calendar day, in the display time zone.
  const days: { day: string; items: TimelineEntry[] }[] = [];
  for (const e of shown) {
    const day = formatTime(e.at, tz).split(",")[0];
    const last = days[days.length - 1];
    if (last && last.day === day) last.items.push(e);
    else days.push({ day, items: [e] });
  }

  return (
    <div className="space-y-4">
      {q.data.notes.map((n) => (
        <div key={n} className="flex items-start gap-2 rounded-lg bg-amber-500/10 p-3 text-xs text-amber-200 font-mono">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0" strokeWidth={1.5} />
          <span>{n}</span>
        </div>
      ))}

      <div role="group" aria-label="Filter the timeline" className="flex flex-wrap gap-1.5">
        {kinds.map((k) => (
          <button
            key={k.id}
            type="button"
            aria-pressed={kind === k.id}
            onClick={() => setKind(k.id)}
            className={cn("rounded-md px-2.5 py-1.5 text-xs font-mono transition-colors", kind === k.id ? "bg-[#202028] text-white" : "bg-[#141418] text-zinc-400 hover:text-zinc-200")}
          >
            {k.label}
          </button>
        ))}
      </div>

      {shown.length === 0 ? (
        <div className="rounded-xl bg-[#0a0a0d] py-12 text-center text-xs font-mono text-zinc-500">nothing in the last 14 days</div>
      ) : (
        <div className="space-y-5">
          {days.map((d) => (
            <section key={d.day} aria-label={d.day}>
              <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider text-zinc-500">{d.day}</h3>
              <ol className="rounded-xl bg-[#0a0a0d]">
                {d.items.map((e, i) => {
                  const Icon = icons[e.kind];
                  return (
                    <li key={`${e.at}-${e.kind}-${e.title}-${i}`} className="flex items-start gap-3 border-t border-zinc-900/60 px-4 py-3 first:border-t-0" data-kind={e.kind} data-level={e.level}>
                      <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", dot[e.level])} aria-label={e.level} />
                      <Icon className="mt-0.5 size-4 shrink-0 text-zinc-500" strokeWidth={1.5} aria-hidden />
                      <div className="min-w-0 flex-1">
                        <div className="text-sm text-zinc-100">
                          {e.title}
                          {e.actor && <span className="text-zinc-500"> · {e.actor}</span>}
                        </div>
                        {e.detail && <div className="mt-0.5 break-words text-xs font-mono text-zinc-500">{e.detail}</div>}
                      </div>
                      <span className="shrink-0 text-xs font-mono text-zinc-500">{formatTime(e.at, tz).split(", ").slice(1).join(", ") || formatTime(e.at, tz)}</span>
                      {e.url && (
                        <a href={e.url} target="_blank" rel="noreferrer" className="shrink-0 text-sky-400 hover:text-sky-300" title="Open on GitHub">
                          <ExternalLink className="size-3.5" strokeWidth={1.5} />
                        </a>
                      )}
                    </li>
                  );
                })}
              </ol>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
