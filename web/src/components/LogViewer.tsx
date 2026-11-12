import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Play,
  Square,
  Pause,
  Download,
  Trash2,
  Search,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { AnsiText } from "@/components/AnsiText";
import { LogBody, rowTint } from "@/components/LogBody";
import { formatTime, isAccessLine } from "@/lib/api";
import { levelOf, levelRank, parseLogLine, type Level, type Parsed } from "@/lib/logline";

type Line = { ts: string; stream: string; text: string; marker?: boolean };
type Conn = "idle" | "connecting" | "live" | "closed";

const MAX_LINES = 5000;

type Shown = { line: Line; parsed: Parsed };

const reasons: Record<string, string> = {
  container_not_found: "No container found for this service.",
  docker_unavailable: "Docker is not reachable on the app server.",
  logs_unavailable: "Logs are not available through Docker for this container.",
  not_allowed: "The agent on that server is not configured to expose these logs.",
  too_many_streams: "Too many log viewers are open on this agent. Close one and retry.",
  access_revoked: "Your access to these logs ended.",
  agent_disconnected: "The agent disconnected.",
  ended: "The stream ended.",
};

export function LogViewer({
  app,
  env,
  tz,
  disabledReason,
}: {
  app: string;
  env: string;
  tz: string;
  disabledReason?: string;
}) {
  const [lines, setLines] = useState<Line[]>([]);
  const [conn, setConn] = useState<Conn>("idle");
  const [note, setNote] = useState("");
  const [paused, setPaused] = useState(false);
  const [filter, setFilter] = useState("");
  // A static web container writes one nginx access line per request, which buries everything else.
  const [hideAccess, setHideAccess] = useState(false);
  const [tail, setTail] = useState(200);
  const [follow, setFollow] = useState(true);
  // Structured layout (levels, JSON fields, access lines) is on by default; raw shows the text exactly as written.
  const [raw, setRaw] = useState(false);
  const [minLevel, setMinLevel] = useState<"all" | Level>("all");
  const [heldCount, setHeldCount] = useState(0);
  const wsRef = useRef<WebSocket | null>(null);
  const pausedRef = useRef(false);
  const held = useRef<Line[]>([]);
  const box = useRef<HTMLDivElement>(null);

  const append = useCallback((incoming: Line[]) => {
    setLines((prev) => {
      const next = prev.concat(incoming);
      return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
    });
  }, []);

  const stop = useCallback(() => {
    wsRef.current?.close();
    wsRef.current = null;
  }, []);

  const start = useCallback(() => {
    stop();
    setLines([]);
    held.current = [];
    setHeldCount(0);
    setNote("");
    setConn("connecting");
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(
      `${proto}://${location.host}/ws/apps/${encodeURIComponent(app)}/envs/${encodeURIComponent(env)}/logs?tail=${tail}`
    );
    wsRef.current = ws;
    ws.onopen = () => setConn("live");
    ws.onmessage = (ev) => {
      const m = JSON.parse(ev.data) as {
        type: string;
        lines?: Line[];
        reason?: string;
        message?: string;
      };
      if (m.type === "lines" && m.lines) {
        if (pausedRef.current) {
          held.current.push(...m.lines);
          setHeldCount(held.current.length);
        } else {
          append(m.lines);
        }
      } else if (m.type === "closed") {
        setNote(m.message || reasons[m.reason ?? ""] || "The stream closed.");
        setConn("closed");
      }
    };
    ws.onclose = () => {
      setConn((c) => (c === "closed" ? c : "closed"));
      setNote((n) => n || "Disconnected.");
    };
  }, [app, env, tail, append, stop]);

  useEffect(() => stop, [stop]);

  const togglePause = () => {
    const next = !paused;
    pausedRef.current = next;
    setPaused(next);
    if (!next && held.current.length) {
      append(held.current);
      held.current = [];
      setHeldCount(0);
    }
  };

  const shown = useMemo<Shown[]>(() => {
    const f = filter.trim().toLowerCase();
    const visible = hideAccess ? lines.filter((l) => !isAccessLine(l.text)) : lines;
    const matched = f ? visible.filter((l) => l.text.toLowerCase().includes(f)) : visible;
    const all = matched.map((line) => ({ line, parsed: parseLogLine(line.text) }));
    if (minLevel === "all") return all;
    // Keep lines at or above the level. A line with no level of its own (a stack trace line, a continuation)
    // stays when the line before it was kept, so an error keeps its trace.
    const min = levelRank(minLevel);
    const out: Shown[] = [];
    let keep = false;
    for (const s of all) {
      const lv = levelOf(s.parsed);
      if (lv) keep = levelRank(lv) >= min;
      else if (s.parsed.kind !== "trace") keep = false;
      if (keep) out.push(s);
    }
    return out;
  }, [lines, filter, hideAccess, minLevel]);

  useEffect(() => {
    if (follow && !paused && box.current) {
      box.current.scrollTop = box.current.scrollHeight;
    }
  }, [shown, follow, paused]);

  const download = () => {
    const body = shown.map(({ line: l }) => `${l.ts} ${l.stream} ${l.text}`).join("\n") + "\n";
    const url = URL.createObjectURL(new Blob([body], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${app}-${env}-logs.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const active = conn === "live" || conn === "connecting";

  return (
    <section aria-label="Logs" className="space-y-2.5">
      {/* Precision Controls Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-3 text-xs font-mono py-1">
        <div className="flex flex-wrap items-center gap-2.5">
          {active ? (
            <Button
              size="sm"
              variant="destructive"
              onClick={() => {
                stop();
                setConn("closed");
                setNote("Stopped.");
              }}
            >
              <Square className="size-3.5" strokeWidth={1.5} /> stop
            </Button>
          ) : (
            <Button
              size="sm"
              variant="default"
              onClick={start}
              disabled={!!disabledReason}
            >
              <Play className="size-3.5" strokeWidth={1.5} /> {conn === "idle" ? "tail logs" : "reconnect"}
            </Button>
          )}

          <div className="flex items-center gap-1.5 rounded-md bg-[#121215] px-2.5 py-1.5 text-xs text-zinc-400">
            <span>tail</span>
            <select
              className="bg-transparent text-zinc-200 focus:outline-none cursor-pointer font-mono"
              value={tail}
              disabled={active}
              onChange={(e) => setTail(Number(e.target.value))}
            >
              {[0, 50, 200, 500, 1000, 2000].map((n) => (
                <option key={n} value={n} className="bg-black text-white">
                  {n === 0 ? "live" : `${n}`}
                </option>
              ))}
            </select>
          </div>

          <Button
            size="sm"
            variant="outline"
            onClick={togglePause}
            disabled={!active}
          >
            {paused ? (
              <>
                <Play className="size-3.5 text-emerald-400" strokeWidth={1.5} />
                <span>resume{heldCount ? ` (${heldCount})` : ""}</span>
              </>
            ) : (
              <>
                <Pause className="size-3.5" strokeWidth={1.5} />
                <span>pause</span>
              </>
            )}
          </Button>

          <label className="flex items-center gap-2 px-1 text-xs text-zinc-400 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={follow}
              onChange={(e) => setFollow(e.target.checked)}
              className="rounded size-3.5 accent-white cursor-pointer"
            />
            follow
          </label>

          <label className="flex items-center gap-2 px-1 text-xs text-zinc-400 cursor-pointer select-none" title="Hide nginx request lines, so errors and messages stand out">
            <input
              type="checkbox"
              checked={hideAccess}
              onChange={(e) => setHideAccess(e.target.checked)}
              className="rounded size-3.5 accent-white cursor-pointer"
            />
            hide access lines
          </label>

          <label className="flex items-center gap-2 px-1 text-xs text-zinc-400 cursor-pointer select-none" title="Show lines exactly as the app wrote them">
            <input type="checkbox" checked={raw} onChange={(e) => setRaw(e.target.checked)} className="rounded size-3.5 accent-white cursor-pointer" />
            raw
          </label>

          <div className="flex items-center gap-1.5 rounded-md bg-[#121215] px-2.5 py-1.5 text-xs text-zinc-400" title="Show only lines at this level or above">
            <span>level</span>
            <select
              aria-label="Minimum level"
              className="bg-transparent text-zinc-200 focus:outline-none cursor-pointer font-mono"
              value={minLevel}
              onChange={(e) => setMinLevel(e.target.value as "all" | Level)}
            >
              <option value="all" className="bg-black text-white">all</option>
              <option value="info" className="bg-black text-white">info and above</option>
              <option value="warn" className="bg-black text-white">warnings and errors</option>
              <option value="error" className="bg-black text-white">errors only</option>
            </select>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 size-3.5 text-zinc-500" strokeWidth={1.5} />
            <input
              className="h-8 w-44 rounded-md bg-[#121215] pl-8 pr-2.5 text-xs text-zinc-200 placeholder:text-zinc-500 focus:outline-none font-mono"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="filter..."
            />
          </div>

          <button
            onClick={download}
            disabled={shown.length === 0}
            className="text-zinc-400 hover:text-white p-1.5 rounded-md hover:bg-zinc-900 disabled:opacity-30 transition-colors"
            title="Download visible logs"
          >
            <Download className="size-4" strokeWidth={1.5} />
          </button>

          <button
            onClick={() => {
              setLines([]);
              held.current = [];
            }}
            disabled={lines.length === 0}
            className="text-zinc-400 hover:text-white p-1.5 rounded-md hover:bg-zinc-900 disabled:opacity-30 transition-colors"
            title="Clear viewport"
          >
            <Trash2 className="size-4" strokeWidth={1.5} />
          </button>
        </div>
      </div>

      {disabledReason && (
        <p className="rounded-md bg-amber-500/10 p-2.5 text-xs text-amber-300 font-mono">
          {disabledReason}
        </p>
      )}

      {note && (
        <p className="rounded-md bg-[#121215] p-2.5 text-xs text-zinc-400 font-mono" role="status">
          {note}
        </p>
      )}

      {/* Terminal Viewport */}
      <div className="rounded-xl bg-[#060608] overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 text-xs font-mono text-zinc-400 bg-[#0c0c0f] border-b border-zinc-900/80">
          <span>{app}/{env}</span>
          <span>{shown.length} lines</span>
        </div>

        <div
          ref={box}
          className="h-[420px] overflow-auto p-4 font-mono-code text-[13px] text-zinc-200 leading-relaxed select-text"
          tabIndex={0}
          aria-label="Log output"
        >
          {shown.length === 0 ? (
            <div className="flex h-full items-center justify-center text-zinc-500 text-xs font-mono">
              {conn === "idle" ? "idle" : "no logs recorded"}
            </div>
          ) : (
            shown.map(({ line: l, parsed }, i) => (
              <div key={i} className={cn("flex items-start gap-3 rounded px-1 py-0.5", raw ? (l.marker ? "text-amber-300 font-semibold" : "text-zinc-200") : l.marker ? "text-amber-300 font-semibold" : rowTint(parsed))}>
                <span className="select-none text-zinc-600 text-xs w-8 shrink-0 text-right font-mono pt-0.5">{i + 1}</span>
                <span className="select-none text-zinc-500 text-xs shrink-0 font-mono pt-0.5">{formatTime(l.ts, tz).split(", ").pop()}</span>
                <span className="min-w-0 flex-1">
                  {raw || l.marker ? (
                    <span className="whitespace-pre-wrap break-all">
                      <AnsiText text={l.text} />
                    </span>
                  ) : (
                    <LogBody raw={l.text} parsed={parsed} />
                  )}
                </span>
              </div>
            ))
          )}
        </div>
      </div>
    </section>
  );
}
