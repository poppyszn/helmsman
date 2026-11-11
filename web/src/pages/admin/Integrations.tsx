import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, CircleAlert, Loader2, PlugZap } from "lucide-react";
import { api, ApiError, formatTime, useMe, type LokiStatus, type LokiTest } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";

type Resp = { loki: LokiStatus };

function Step({ n, title, children }: { n: number; title: string; children: React.ReactNode }) {
  return (
    <li className="flex gap-3.5">
      <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full bg-[#202028] text-xs font-mono text-zinc-200">{n}</span>
      <div className="min-w-0 space-y-1.5">
        <div className="text-sm font-semibold text-white">{title}</div>
        <div className="text-sm text-zinc-400 leading-relaxed">{children}</div>
      </div>
    </li>
  );
}

const Code = ({ children }: { children: React.ReactNode }) => (
  <pre className="mt-1.5 overflow-x-auto rounded-lg bg-[#060608] p-3 text-xs font-mono text-zinc-300 whitespace-pre-wrap break-all">{children}</pre>
);

function TestResult({ t }: { t: LokiTest }) {
  const row = (ok: boolean, warn: boolean, text: React.ReactNode) => (
    <li className="flex items-start gap-2 text-sm">
      {ok ? (
        <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-emerald-400" strokeWidth={2} aria-label="ok" />
      ) : (
        <CircleAlert className={`mt-0.5 size-4 shrink-0 ${warn ? "text-amber-400" : "text-rose-400"}`} strokeWidth={2} aria-label={warn ? "note" : "failed"} />
      )}
      <span className="text-zinc-300">{text}</span>
    </li>
  );
  return (
    <ul className="space-y-2 rounded-lg bg-[#060608] p-3.5" aria-label="Connection test">
      {row(t.reachable, false, t.reachable ? "Loki answered." : t.error || "Loki could not be reached.")}
      {t.reachable &&
        row(
          t.has_logs,
          true,
          t.has_logs
            ? "Helmsman's logs are already in Loki."
            : "Loki is empty so far. That is fine: history fills in once Alloy is running (step 1).",
        )}
    </ul>
  );
}

export default function Integrations() {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const qc = useQueryClient();
  const q = useQuery<Resp>({ queryKey: ["integrations"], queryFn: () => api("/api/admin/integrations") });
  const st = q.data?.loki;

  const [url, setUrl] = useState<string | null>(null);
  const [user, setUser] = useState<string | null>(null);
  const [password, setPassword] = useState("");
  const [changePassword, setChangePassword] = useState(false);
  const [test, setTest] = useState<LokiTest | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  const urlV = url ?? st?.url ?? "";
  const userV = user ?? st?.user ?? "";
  const body = (enabled: boolean) => ({
    url: urlV,
    user: userV,
    enabled,
    // Leave the password out to keep the saved one. Send it only when the admin typed a new one.
    ...(changePassword || !st?.has_password ? { password } : {}),
  });

  const run = useMutation({
    mutationFn: () => api<LokiTest>("/api/admin/integrations/loki/test", { method: "POST", json: body(true) }),
    onMutate: () => {
      setErr(null);
      setDone(null);
    },
    onSuccess: setTest,
    onError: (e) => {
      setTest(null);
      setErr(e instanceof ApiError ? e.message : "the test failed");
    },
  });
  const save = useMutation({
    mutationFn: (enabled: boolean) => api<Resp & { test: LokiTest }>("/api/admin/integrations/loki", { method: "PUT", json: body(enabled) }),
    onMutate: () => {
      setErr(null);
      setDone(null);
    },
    onSuccess: (r, enabled) => {
      qc.setQueryData(["integrations"], { loki: r.loki });
      qc.invalidateQueries({ queryKey: ["log-history"] });
      setTest(r.test);
      setChangePassword(false);
      setPassword("");
      setDone(enabled ? "Log history is on. Open an environment's logs tab and choose history." : "Log history is off. Your settings are kept.");
    },
    onError: (e) => {
      if (e instanceof ApiError) {
        const b = e.body as { test?: LokiTest } | undefined;
        if (b?.test) setTest(b.test);
        setErr(e.message);
      } else setErr("could not save");
    },
  });
  const remove = useMutation({
    mutationFn: () => api<Resp>("/api/admin/integrations/loki", { method: "DELETE" }),
    onSuccess: (r) => {
      qc.setQueryData(["integrations"], r);
      qc.invalidateQueries({ queryKey: ["log-history"] });
      setUrl("");
      setUser("");
      setTest(null);
      setDone("The settings were removed.");
    },
  });

  const busy = run.isPending || save.isPending || remove.isPending;
  const connected = !!st?.connected;
  const badge = connected ? <Badge tone="ok">connected</Badge> : st?.url ? <Badge tone="default">turned off</Badge> : <Badge tone="default">not connected</Badge>;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-white">Integrations</h1>
        <p className="mt-1 text-sm text-zinc-400">Optional services that add to Helmsman. Nothing here is needed to run it.</p>
      </div>

      <Card className="space-y-5 bg-[#0a0a0d] p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex items-start gap-3">
            <PlugZap className="mt-0.5 size-5 text-zinc-400" strokeWidth={1.5} />
            <div>
              <h2 className="text-base font-semibold text-white">Log history (Loki)</h2>
              <p className="mt-1 max-w-2xl text-sm text-zinc-400">
                Keeps 14 days of container and browser logs, so you can look back after something broke. The live tail does not need it and keeps working either way.
              </p>
            </div>
          </div>
          {badge}
        </div>

        {st?.source === "environment" && (
          <p className="rounded-lg bg-[#141418] p-3 text-xs font-mono text-zinc-400">
            This connection comes from the server's environment (IDP_LOKI_URL). Saving here replaces it, and removing the saved settings brings it back.
          </p>
        )}
        {st?.updated_at && st.source === "database" && (
          <p className="text-xs font-mono text-zinc-500">
            Last changed by {st.updated_by || "an admin"} on {formatTime(st.updated_at, tz)}.
          </p>
        )}

        <form
          className="grid gap-3.5 sm:grid-cols-2"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate(true);
          }}
        >
          <label className="block text-xs font-mono text-zinc-400 sm:col-span-2">
            Loki address
            <Input className="mt-1.5 bg-black font-mono text-xs" placeholder="http://loki:3100" value={urlV} onChange={(e) => setUrl(e.target.value)} autoComplete="off" />
          </label>
          <label className="block text-xs font-mono text-zinc-400">
            Username <span className="text-zinc-600">(only if Loki asks for a login)</span>
            <Input className="mt-1.5 bg-black font-mono text-xs" value={userV} onChange={(e) => setUser(e.target.value)} autoComplete="off" />
          </label>
          <label className="block text-xs font-mono text-zinc-400">
            Password
            {st?.has_password && !changePassword ? (
              <div className="mt-1.5 flex h-9 items-center justify-between rounded-md bg-black px-3 text-xs text-zinc-500">
                saved
                <button type="button" onClick={() => setChangePassword(true)} className="text-zinc-300 hover:text-white cursor-pointer">
                  change
                </button>
              </div>
            ) : (
              <Input type="password" className="mt-1.5 bg-black font-mono text-xs" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
            )}
          </label>

          <div className="flex flex-wrap items-center gap-2.5 sm:col-span-2">
            <Button type="button" variant="secondary" onClick={() => run.mutate()} disabled={busy || !urlV}>
              {run.isPending && <Loader2 className="size-3.5 animate-spin" strokeWidth={2} />}
              Test connection
            </Button>
            <Button type="submit" disabled={busy || !urlV}>
              {connected ? "Save changes" : "Save and turn on"}
            </Button>
            {connected && (
              <Button type="button" variant="outline" onClick={() => save.mutate(false)} disabled={busy}>
                Turn off
              </Button>
            )}
            {st?.source === "database" && (
              <Button type="button" variant="ghost" onClick={() => remove.mutate()} disabled={busy}>
                Remove settings
              </Button>
            )}
          </div>
        </form>

        {err && (
          <p role="alert" className="rounded-lg bg-rose-500/10 p-3 text-sm text-rose-300">
            {err}
          </p>
        )}
        {test && <TestResult t={test} />}
        {done && (
          <p role="status" className="rounded-lg bg-emerald-500/10 p-3 text-sm text-emerald-300">
            {done}
          </p>
        )}
      </Card>

      <Card className="bg-[#0a0a0d] p-6">
        <details open={!connected}>
          <summary className="cursor-pointer text-base font-semibold text-white select-none">How to set up log history</summary>
          <ol className="mt-5 space-y-5">
            <Step n={1} title="Run Loki and Alloy">
              Loki stores the logs. Alloy reads your containers' output and sends it to Loki. On the Helmsman server, in <code className="font-mono text-zinc-300">deploy/server/.env</code>, add
              <code className="font-mono text-zinc-300"> logs</code> to <code className="font-mono text-zinc-300">COMPOSE_PROFILES</code> and fill in the log settings from
              <code className="font-mono text-zinc-300"> env.example</code>, then start them:
              <Code>{`docker compose --env-file .env up -d`}</Code>
              Already have a Loki? Skip this and use its address in step 2. Point your own Alloy at it with the files in <code className="font-mono text-zinc-300">deploy/alloy</code>.
            </Step>
            <Step n={2} title="Enter the address above">
              If Loki runs in the same Docker stack as Helmsman, that is <code className="font-mono text-zinc-300">http://loki:3100</code>. Otherwise use its address as the Helmsman server reaches it. Leave the login empty unless Loki
              sits behind one.
            </Step>
            <Step n={3} title="Test connection">
              You want a tick for "Loki answered". "Loki is empty so far" is fine on a new setup: it fills in as soon as Alloy sends its first lines.
            </Step>
            <Step n={4} title="Save and turn on">
              Every environment then has <strong className="text-zinc-200">history</strong> next to <strong className="text-zinc-200">live tail</strong> in its logs tab, for people with
              <code className="font-mono text-zinc-300"> logs.view</code>. No restart is needed, and you can turn it off here at any time.
            </Step>
            <Step n={5} title="Optional: browser logs for static apps">
              For a web app, add Grafana Faro to its pages and put its name in the catalog as <code className="font-mono text-zinc-300">logs.faro_app</code>. Browser errors and events then show up beside the nginx
              logs. The steps are in <code className="font-mono text-zinc-300">docs/logs.md</code>.
            </Step>
          </ol>
        </details>
      </Card>
    </div>
  );
}
