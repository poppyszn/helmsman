import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, CircleAlert, Info, Loader2, RefreshCw, XCircle } from "lucide-react";
import { api, formatTime, useMe } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

type Check = { id: string; group: string; title: string; status: "ok" | "warn" | "fail" | "info"; detail: string; fix?: string };
type Resp = { checks: Check[]; counts: Record<string, number>; generated_at: string };

const WS_FIX =
  "Live log tails and agents need WebSocket upgrades to reach Helmsman. In nginx, the location for /ws/ and /agent/ws must set Upgrade and Connection and repeat Host and the X-Forwarded headers (nginx drops the server-level proxy_set_header lines in a location that sets its own). docs/server-setup.md, step 6, has the exact block.";

/** Opens the check endpoint the way the log tail does. It answers "ok" once if upgrades and the origin check work. */
function probeWebSocket(): Promise<Check> {
  const base = { id: "websocket", group: "Server and HTTPS", title: "Live connections (WebSocket)" } as const;
  return new Promise((resolve) => {
    let done = false;
    const finish = (c: Omit<Check, "id" | "group" | "title">) => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      resolve({ ...base, ...c });
    };
    const url = `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws/doctor`;
    const ws = new WebSocket(url);
    const timer = setTimeout(() => {
      ws.close();
      finish({ status: "fail", detail: "No answer within 5 seconds.", fix: WS_FIX });
    }, 5000);
    ws.onmessage = (m) => {
      if (m.data === "ok") finish({ status: "ok", detail: "A WebSocket opened and answered through the proxy." });
    };
    ws.onerror = () => finish({ status: "fail", detail: "The browser could not open a WebSocket to Helmsman.", fix: WS_FIX });
    ws.onclose = () => finish({ status: "fail", detail: "The WebSocket closed before it answered.", fix: WS_FIX });
  });
}

function StatusIcon({ s }: { s: Check["status"] }) {
  const cls = "mt-0.5 size-4 shrink-0";
  if (s === "ok") return <CheckCircle2 className={cn(cls, "text-emerald-400")} strokeWidth={2} aria-label="ok" />;
  if (s === "fail") return <XCircle className={cn(cls, "text-rose-400")} strokeWidth={2} aria-label="failed" />;
  if (s === "warn") return <CircleAlert className={cn(cls, "text-amber-400")} strokeWidth={2} aria-label="warning" />;
  return <Info className={cn(cls, "text-zinc-500")} strokeWidth={2} aria-label="info" />;
}

export default function Doctor() {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const q = useQuery<Resp>({ queryKey: ["doctor"], queryFn: () => api("/api/admin/doctor"), refetchOnWindowFocus: false });
  const [ws, setWs] = useState<Check | null>(null);
  const [testing, setTesting] = useState(true);
  const [round, setRound] = useState(0);

  useEffect(() => {
    let live = true;
    void probeWebSocket().then((c) => {
      if (!live) return;
      setWs(c);
      setTesting(false);
    });
    return () => {
      live = false;
    };
  }, [round]);

  const again = () => {
    setTesting(true);
    setRound((r) => r + 1);
    void q.refetch();
  };

  if (q.isLoading) return <div className="py-12 text-center text-xs font-mono text-zinc-500">checking</div>;
  if (q.isError || !q.data) return <div className="py-12 text-center text-xs font-mono text-rose-300">could not run the checks</div>;

  const checks = q.data.checks.map((c) => (c.id === "websocket" && ws ? ws : c));
  const groups = [...new Set(checks.map((c) => c.group))];
  const n = (s: string) => checks.filter((c) => c.status === s).length;
  const problems = n("fail") + n("warn");

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Setup check</h1>
          <p className="mt-1 text-sm text-zinc-400">
            {problems === 0 ? "Everything checks out." : `${n("fail")} to fix, ${n("warn")} to look at.`} Checked {formatTime(q.data.generated_at, tz)}. Nothing here changes anything.
          </p>
        </div>
        <Button variant="secondary" onClick={again} disabled={q.isFetching || testing}>
          {q.isFetching || testing ? <Loader2 className="size-3.5 animate-spin" strokeWidth={2} /> : <RefreshCw className="size-3.5" strokeWidth={1.5} />}
          Check again
        </Button>
      </div>

      {groups.map((g) => (
        <Card key={g} className="space-y-1 p-0" aria-label={g}>
          <h2 className="px-5 pt-4 pb-2 text-xs font-semibold uppercase tracking-wider text-zinc-400">{g}</h2>
          <ul>
            {checks
              .filter((c) => c.group === g)
              .map((c) => (
                <li key={c.id} className="flex gap-3 border-t border-zinc-900/60 px-5 py-3 first:border-t-0" data-status={c.status}>
                  <StatusIcon s={c.status} />
                  <div className="min-w-0">
                    <div className="text-sm font-medium text-white">{c.title}</div>
                    <div className="mt-0.5 text-sm text-zinc-400 break-words">{c.detail}</div>
                    {c.fix && <div className="mt-1.5 rounded-md bg-[#141418] px-3 py-2 text-sm leading-relaxed text-zinc-300">{c.fix}</div>}
                  </div>
                </li>
              ))}
          </ul>
        </Card>
      ))}
    </div>
  );
}
