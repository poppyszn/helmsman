import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { AnsiText } from "@/components/AnsiText";
import { formatTime } from "@/lib/api";

type Line = { ts: string; stream: string; text: string; marker?: boolean };
type Conn = "idle" | "connecting" | "live" | "closed";

const MAX_LINES = 5000;

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

export function LogViewer({ app, env, tz, disabledReason }: { app: string; env: string; tz: string; disabledReason?: string }) {
  const [lines, setLines] = useState<Line[]>([]);
  const [conn, setConn] = useState<Conn>("idle");
  const [note, setNote] = useState("");
  const [paused, setPaused] = useState(false);
  const [filter, setFilter] = useState("");
  const [tail, setTail] = useState(200);
  const [follow, setFollow] = useState(true);
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
    const ws = new WebSocket(`${proto}://${location.host}/ws/apps/${encodeURIComponent(app)}/envs/${encodeURIComponent(env)}/logs?tail=${tail}`);
    wsRef.current = ws;
    ws.onopen = () => setConn("live");
    ws.onmessage = (ev) => {
      const m = JSON.parse(ev.data) as { type: string; lines?: Line[]; reason?: string; message?: string };
      if (m.type === "lines" && m.lines) {
        if (pausedRef.current) { held.current.push(...m.lines); setHeldCount(held.current.length); }
        else append(m.lines);
      } else if (m.type === "closed") {
        setNote(m.message || reasons[m.reason ?? ""] || "The stream closed.");
        setConn("closed");
      }
    };
    ws.onclose = () => { setConn((c) => (c === "closed" ? c : "closed")); setNote((n) => n || "Disconnected."); };
  }, [app, env, tail, append, stop]);

  useEffect(() => stop, [stop]);

  const togglePause = () => {
    const next = !paused;
    pausedRef.current = next;
    setPaused(next);
    if (!next && held.current.length) { append(held.current); held.current = []; setHeldCount(0); }
  };

  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return f ? lines.filter((l) => l.text.toLowerCase().includes(f)) : lines;
  }, [lines, filter]);

  useEffect(() => {
    if (follow && !paused && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [shown, follow, paused]);

  const download = () => {
    const body = shown.map((l) => `${l.ts} ${l.stream} ${l.text}`).join("\n") + "\n";
    const url = URL.createObjectURL(new Blob([body], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${app}-${env}-logs.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const active = conn === "live" || conn === "connecting";
  return (
    <section aria-label="Logs" className="space-y-2">
      <div className="flex flex-wrap items-end gap-2">
        {active ? (
          <Button size="sm" variant="outline" onClick={() => { stop(); setConn("closed"); setNote("Stopped."); }}>Stop</Button>
        ) : (
          <Button size="sm" onClick={start} disabled={!!disabledReason}>{conn === "idle" ? "Start tailing" : "Reconnect"}</Button>
        )}
        <label className="text-xs">History<br />
          <select className="h-8 rounded-md border border-border bg-card px-2 text-sm" value={tail} disabled={active}
            onChange={(e) => setTail(Number(e.target.value))}>
            {[0, 50, 200, 500, 1000, 2000].map((n) => <option key={n} value={n}>{n === 0 ? "live only" : `${n} lines`}</option>)}
          </select>
        </label>
        <Button size="sm" variant="outline" onClick={togglePause} disabled={!active}>
          {paused ? `Resume${heldCount ? ` (${heldCount} held)` : ""}` : "Pause"}
        </Button>
        <label className="flex h-8 items-center gap-1 text-sm">
          <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> Follow
        </label>
        <label className="text-xs">Filter (in view)<br />
          <input className="h-8 w-48 rounded-md border border-border bg-card px-2 text-sm" value={filter}
            onChange={(e) => setFilter(e.target.value)} placeholder="text to match" />
        </label>
        <Button size="sm" variant="outline" onClick={download} disabled={shown.length === 0}>Download</Button>
        <Button size="sm" variant="outline" onClick={() => { setLines([]); held.current = []; }} disabled={lines.length === 0}>Clear</Button>
        <span className="ml-auto text-xs text-muted" role="status">
          {conn === "live" && !paused && "Live"}{conn === "live" && paused && "Paused"}{conn === "connecting" && "Connecting"}
          {conn === "closed" && "Not connected"}
        </span>
      </div>
      {disabledReason && <p className="text-sm text-muted">{disabledReason}</p>}
      {note && <p className="text-sm" role="status">{note}</p>}
      <p className="text-xs text-muted">History shown is since the container started. Older logs are not kept here.</p>
      <div ref={box} className="h-96 overflow-auto rounded-md border border-border bg-neutral-950 p-2 font-mono text-xs text-neutral-200"
        tabIndex={0} aria-label="Log output">
        {shown.length === 0 && <p className="text-neutral-500">{conn === "idle" ? "Not tailing." : "No lines yet."}</p>}
        {shown.map((l, i) => (
          <div key={i} className={`whitespace-pre-wrap break-all ${l.marker ? "italic text-amber-400" : l.stream === "stderr" ? "text-red-300" : ""}`}>
            <span className="mr-2 select-none text-neutral-500" title={l.ts}>{formatTime(l.ts, tz).split(", ").pop()}</span>
            <AnsiText text={l.text} />
          </div>
        ))}
      </div>
    </section>
  );
}
