import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, CircleAlert, Loader2, Mail } from "lucide-react";
import { api, ApiError, formatTime, useMe, type LokiStatus, type MailpitStatus, type MailpitTest } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";

type Resp = { loki: LokiStatus; mailpit?: MailpitStatus };

/** Connects a Mailpit, so the Mail page can show the messages the development environments send. */
export function MailpitCard() {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const qc = useQueryClient();
  const q = useQuery<Resp>({ queryKey: ["integrations"], queryFn: () => api("/api/admin/integrations") });
  const st = q.data?.mailpit;

  const [url, setUrl] = useState<string | null>(null);
  const [user, setUser] = useState<string | null>(null);
  const [password, setPassword] = useState("");
  const [changePassword, setChangePassword] = useState(false);
  const [test, setTest] = useState<MailpitTest | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  const urlV = url ?? st?.url ?? "";
  const userV = user ?? st?.user ?? "";
  const body = (enabled: boolean) => ({
    url: urlV,
    user: userV,
    enabled,
    ...(changePassword || !st?.has_password ? { password } : {}),
  });
  const merge = (mailpit: MailpitStatus) => qc.setQueryData<Resp>(["integrations"], (old) => (old ? { ...old, mailpit } : old));

  const run = useMutation({
    mutationFn: () => api<MailpitTest>("/api/admin/integrations/mailpit/test", { method: "POST", json: body(true) }),
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
    mutationFn: (enabled: boolean) => api<{ mailpit: MailpitStatus; test: MailpitTest }>("/api/admin/integrations/mailpit", { method: "PUT", json: body(enabled) }),
    onMutate: () => {
      setErr(null);
      setDone(null);
    },
    onSuccess: (r, enabled) => {
      merge(r.mailpit);
      qc.invalidateQueries({ queryKey: ["mail"] });
      setTest(r.test);
      setChangePassword(false);
      setPassword("");
      setDone(enabled ? "Mail is on. People with the mail.view permission now see Mail in the sidebar." : "Mail is off. Your settings are kept.");
    },
    onError: (e) => {
      if (e instanceof ApiError) {
        const b = e.body as { test?: MailpitTest } | undefined;
        if (b?.test) setTest(b.test);
        setErr(e.message);
      } else setErr("could not save");
    },
  });
  const remove = useMutation({
    mutationFn: () => api<{ mailpit: MailpitStatus }>("/api/admin/integrations/mailpit", { method: "DELETE" }),
    onSuccess: (r) => {
      merge(r.mailpit);
      qc.invalidateQueries({ queryKey: ["mail"] });
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
    <Card role="region" aria-label="Mail (Mailpit)" className="space-y-5 bg-[#0a0a0d] p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <Mail className="mt-0.5 size-5 text-zinc-400" strokeWidth={1.5} />
          <div>
            <h2 className="text-base font-semibold text-white">Mail (Mailpit)</h2>
            <p className="mt-1 max-w-2xl text-sm text-zinc-400">
              Shows the emails your development environments send, so developers can read them here instead of opening Mailpit. Read-only: Helmsman never deletes or sends mail. Point your apps' SMTP at Mailpit as you
              do today, and enter Mailpit's web address here.
            </p>
          </div>
        </div>
        {badge}
      </div>

      {st?.source === "environment" && (
        <p className="rounded-lg bg-[#141418] p-3 text-xs font-mono text-zinc-400">
          This connection comes from the server's environment (IDP_MAILPIT_URL). Saving here replaces it, and removing the saved settings brings it back.
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
          Mailpit address
          <Input className="mt-1.5 bg-black font-mono text-xs" placeholder="http://mailpit:8025" value={urlV} onChange={(e) => setUrl(e.target.value)} autoComplete="off" />
        </label>
        <label className="block text-xs font-mono text-zinc-400">
          Username <span className="text-zinc-600">(only if Mailpit asks for a login)</span>
          <Input className="mt-1.5 bg-black font-mono text-xs" value={userV} onChange={(e) => setUser(e.target.value)} autoComplete="off" />
        </label>
        <label className="block text-xs font-mono text-zinc-400">
          Password
          {st?.has_password && !changePassword ? (
            <div className="mt-1.5 flex h-9 items-center justify-between rounded-md bg-black px-3 text-xs text-zinc-500">
              saved
              <button type="button" onClick={() => setChangePassword(true)} className="cursor-pointer text-sky-400 hover:text-sky-300">
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
      {test && (
        <ul className="space-y-2 rounded-lg bg-[#060608] p-3.5" aria-label="Mailpit check result">
          <li className="flex items-start gap-2 text-sm">
            {test.reachable ? (
              <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-emerald-400" strokeWidth={2} aria-label="ok" />
            ) : (
              <CircleAlert className="mt-0.5 size-4 shrink-0 text-rose-400" strokeWidth={2} aria-label="failed" />
            )}
            <span className="text-zinc-300">
              {test.reachable ? `Mailpit ${test.version ?? ""} answered, with ${test.total} ${test.total === 1 ? "message" : "messages"} so far.` : test.error || "Mailpit could not be reached."}
            </span>
          </li>
        </ul>
      )}
      {done && (
        <p role="status" className="rounded-lg bg-emerald-500/10 p-3 text-sm text-emerald-300">
          {done}
        </p>
      )}
    </Card>
  );
}
