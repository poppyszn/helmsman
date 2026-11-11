import { useDeferredValue, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Globe, RefreshCw, Search, Server } from "lucide-react";
import { api, ApiError, formatTime, type HistoryEntry, type HistoryResponse } from "@/lib/api";
import { AnsiText } from "@/components/AnsiText";
import { Badge, type BadgeTone } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const RANGES = [
  { id: "15m", label: "15 min" },
  { id: "1h", label: "1 hour" },
  { id: "6h", label: "6 hours" },
  { id: "24h", label: "24 hours" },
  { id: "7d", label: "7 days" },
];
const KINDS: { id: HistoryEntry["kind"]; label: string; hint: string }[] = [
  { id: "access", label: "access", hint: "nginx request lines" },
  { id: "error", label: "server errors", hint: "nginx error log" },
  { id: "app", label: "messages", hint: "application and browser console messages" },
  { id: "exception", label: "exceptions", hint: "uncaught browser errors" },
  { id: "event", label: "events", hint: "browser events, such as clicks and navigations" },
  { id: "measurement", label: "measurements", hint: "browser performance, such as web vitals" },
];
const LEVELS: HistoryEntry["level"][] = ["error", "warn", "info", "debug"];
const levelDot: Record<HistoryEntry["level"], string> = {
  error: "bg-rose-500",
  warn: "bg-amber-400",
  info: "bg-zinc-500",
  debug: "bg-zinc-700",
};
const kindTone: Record<HistoryEntry["kind"], BadgeTone> = {
  access: "default",
  error: "bad",
  app: "default",
  exception: "bad",
  event: "indigo",
  measurement: "cyan",
};

function Chip({ on, onClick, children, title, disabled }: { on: boolean; onClick: () => void; children: React.ReactNode; title?: string; disabled?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={title}
      aria-pressed={on}
      className={cn(
        "rounded-md px-2.5 py-1 text-xs font-mono transition-colors cursor-pointer select-none disabled:opacity-30 disabled:cursor-not-allowed",
        on ? "bg-[#202028] text-white" : "bg-[#121215] text-zinc-500 hover:text-zinc-300",
      )}
    >
      {children}
    </button>
  );
}

type Filters = { range: string; container: boolean; browser: boolean; kinds: string[]; levels: string[]; q: string };

function params(f: Filters, before?: string): string {
  const p = new URLSearchParams();
  p.set("range", f.range);
  const sources = [f.container && "container", f.browser && "browser"].filter(Boolean) as string[];
  if (sources.length) p.set("source", sources.join(","));
  if (f.kinds.length < KINDS.length) p.set("kind", f.kinds.join(","));
  if (f.levels.length < LEVELS.length) p.set("level", f.levels.join(","));
  if (f.q.trim()) p.set("q", f.q.trim());
  if (before) p.set("before", before);
  p.set("limit", "200");
  return p.toString();
}

export function LogHistory({ app, env, tz }: { app: string; env: string; tz: string }) {
  const [range, setRange] = useState("1h");
  const [container, setContainer] = useState(true);
  const [browser, setBrowser] = useState(true);
  // Access lines are the bulk of a static site's output, so they start hidden; one click shows them.
  const [kinds, setKinds] = useState<string[]>(KINDS.filter((k) => k.id !== "access").map((k) => k.id));
  const [levels, setLevels] = useState<string[]>([...LEVELS]);
  const [q, setQ] = useState("");
  const deferredQ = useDeferredValue(q);
  const [auto, setAuto] = useState(true);
  const [older, setOlder] = useState<HistoryEntry[]>([]);
  // undefined: use the first page's cursor. null: nothing older is left.
  const [cursor, setCursor] = useState<string | null | undefined>(undefined);
  const [olderBusy, setOlderBusy] = useState(false);
  const [olderErr, setOlderErr] = useState<string | null>(null);

  const filters: Filters = { range, container, browser, kinds, levels, q: deferredQ };
  const base = `/api/apps/${app}/envs/${env}/logs/history`;
  const resetOlder = () => {
    setOlder([]);
    setCursor(undefined);
    setOlderErr(null);
  };
  const toggle = (list: string[], v: string, set: (l: string[]) => void) => {
    resetOlder();
    set(list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  };

  const key = ["log-history", app, env, filters];
  // The first answer says whether this app has a Faro name; until then assume it does.
  const [browserKnown, setBrowserKnown] = useState(true);
  const sourcesOn = container || (browser && browserKnown);
  const q1 = useQuery<HistoryResponse, ApiError>({
    queryKey: key,
    queryFn: async () => {
      const res = await api<HistoryResponse>(`${base}?${params(filters)}`);
      setBrowserKnown(res.sources.browser);
      return res;
    },
    enabled: sourcesOn,
    refetchInterval: auto ? 10_000 : false,
    retry: false,
  });

  if (q1.error?.status === 503) {
    return (
      <div className="rounded-xl bg-[#0a0a0d] p-8 text-center">
        <p className="text-sm text-zinc-300">Log history is not set up on this server.</p>
        <p className="mt-1.5 text-xs font-mono text-zinc-500">Live logs still work. See docs/logs.md to add Loki and Alloy.</p>
      </div>
    );
  }

  const d = sourcesOn ? q1.data : undefined;
  const rows = [...(d?.entries ?? []), ...older];
  const nextCursor = cursor === undefined ? (d?.truncated ? d.next_before : null) : cursor;
  const canLoadOlder = !!nextCursor;

  const loadOlder = async () => {
    if (!nextCursor) return;
    setOlderBusy(true);
    setOlderErr(null);
    try {
      const more = await api<HistoryResponse>(`${base}?${params(filters, nextCursor)}`);
      setOlder((o) => [...o, ...more.entries]);
      setCursor(more.truncated ? more.next_before : null);
    } catch (e) {
      setOlderErr(e instanceof ApiError ? e.message : "could not load older lines");
    } finally {
      setOlderBusy(false);
    }
  };

  const browserAvailable = browserKnown;

  return (
    <section aria-label="Log history" className="space-y-3">
      <div className="space-y-2.5">
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex rounded-lg bg-[#121215] p-1 text-xs font-mono" role="group" aria-label="Time range">
            {RANGES.map((r) => (
              <button
                key={r.id}
                type="button"
                aria-pressed={range === r.id}
                onClick={() => {
                  resetOlder();
                  setRange(r.id);
                }}
                className={cn(
                  "rounded-md px-2.5 py-1 cursor-pointer select-none transition-colors",
                  range === r.id ? "bg-[#202028] text-white" : "text-zinc-400 hover:text-zinc-200",
                )}
              >
                {r.label}
              </button>
            ))}
          </div>
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 size-3.5 text-zinc-500" strokeWidth={1.5} />
            <input
              className="h-9 w-56 rounded-md bg-[#121215] pl-8 pr-2.5 text-xs text-zinc-200 placeholder:text-zinc-500 focus:outline-none font-mono"
              value={q}
              onChange={(e) => {
                resetOlder();
                setQ(e.target.value);
              }}
              placeholder="search..."
              aria-label="Search history"
            />
          </div>
          <label className="flex items-center gap-2 px-1 text-xs text-zinc-400 cursor-pointer select-none font-mono">
            <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} className="rounded size-3.5 accent-white cursor-pointer" />
            auto refresh
          </label>
          <Button size="sm" variant="outline" onClick={() => q1.refetch()} disabled={q1.isFetching} title="Refresh now">
            <RefreshCw className={cn("size-3.5", q1.isFetching && "animate-spin")} strokeWidth={1.5} />
            refresh
          </Button>
        </div>

        <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
          <div className="flex items-center gap-1.5" role="group" aria-label="Source">
            <span className="text-xs font-mono text-zinc-500 mr-1">source</span>
            <Chip on={container} onClick={() => { resetOlder(); setContainer(!container); }} title="Output of the container, read by Alloy">
              <Server className="inline size-3.5 mr-1 -mt-0.5" strokeWidth={1.5} />
              container
            </Chip>
            <Chip
              on={browser && browserAvailable}
              disabled={!browserAvailable}
              onClick={() => { resetOlder(); setBrowser(!browser); }}
              title={browserAvailable ? "Errors, events and console messages from the browser (Grafana Faro)" : "This app has no logs.faro_app in the catalog"}
            >
              <Globe className="inline size-3.5 mr-1 -mt-0.5" strokeWidth={1.5} />
              browser
            </Chip>
          </div>
          <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Kind">
            <span className="text-xs font-mono text-zinc-500 mr-1">type</span>
            {KINDS.map((k) => (
              <Chip key={k.id} on={kinds.includes(k.id)} onClick={() => toggle(kinds, k.id, setKinds)} title={k.hint}>
                {k.label}
              </Chip>
            ))}
          </div>
          <div className="flex items-center gap-1.5" role="group" aria-label="Level">
            <span className="text-xs font-mono text-zinc-500 mr-1">level</span>
            {LEVELS.map((l) => (
              <Chip key={l} on={levels.includes(l)} onClick={() => toggle(levels, l, setLevels)}>
                <span className={cn("inline-block size-2 rounded-full mr-1.5", levelDot[l as HistoryEntry["level"]])} />
                {l}
              </Chip>
            ))}
          </div>
        </div>
      </div>

      {q1.error && q1.error.status !== 503 && (
        <p role="alert" className="rounded-md bg-rose-500/10 p-2.5 text-xs text-rose-300 font-mono">
          {q1.error.message}
        </p>
      )}

      <div className="rounded-xl bg-[#060608] overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 text-xs font-mono text-zinc-400 bg-[#0c0c0f]">
          <span>
            {app}/{env} history
          </span>
          <span>{q1.isLoading ? "loading" : `${rows.length} lines${d?.truncated && !older.length ? "+" : ""}`}</span>
        </div>
        <div className="max-h-[520px] overflow-auto" aria-label="History output">
          {!sourcesOn ? (
            <div className="py-16 text-center text-xs font-mono text-zinc-500">choose a source to see its history</div>
          ) : !q1.isLoading && rows.length === 0 ? (
            <div className="py-16 text-center text-xs font-mono text-zinc-500">
              no lines match these filters in the last {RANGES.find((r) => r.id === range)?.label}
              {!kinds.includes("access") && " (access lines are hidden)"}
            </div>
          ) : (
            <ul>
              {rows.map((r, i) => (
                <li key={`${r.ts}-${i}`} data-kind={r.kind} data-source={r.source} className="flex gap-3 px-4 py-1.5 text-[13px] hover:bg-[#0c0c10]">
                  <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", levelDot[r.level])} title={r.level} />
                  <span className="w-44 shrink-0 font-mono text-xs text-zinc-500 pt-0.5 whitespace-nowrap">{formatTime(r.ts, tz)}</span>
                  <span className="w-[8.5rem] shrink-0 flex items-start gap-1.5 pt-0.5">
                    {r.source === "browser" ? (
                      <Globe className="size-3.5 mt-0.5 text-zinc-500" strokeWidth={1.5} aria-label="browser" />
                    ) : (
                      <Server className="size-3.5 mt-0.5 text-zinc-500" strokeWidth={1.5} aria-label="container" />
                    )}
                    <Badge tone={kindTone[r.kind]}>{r.kind}</Badge>
                  </span>
                  <span className="min-w-0 flex-1 font-mono-code text-zinc-200 whitespace-pre-wrap break-words">
                    {r.source === "container" ? <AnsiText text={r.text} /> : r.text}
                    {r.source === "browser" && (r.page || r.version || r.session) && (
                      <span className="block text-xs text-zinc-500 mt-0.5">
                        {[r.page, r.version && `v${r.version}`, r.session && `session ${r.session.slice(0, 8)}`].filter(Boolean).join("  ")}
                      </span>
                    )}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      <div className="flex items-center justify-between text-xs font-mono text-zinc-500">
        <span>History is kept for {d?.retention ?? "14 days"}.</span>
        {olderErr && <span className="text-rose-300">{olderErr}</span>}
        {canLoadOlder && (
          <Button size="sm" variant="outline" onClick={loadOlder} disabled={olderBusy}>
            {olderBusy ? "loading..." : "load older"}
          </Button>
        )}
      </div>
    </section>
  );
}
