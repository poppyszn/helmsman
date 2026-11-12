import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, Loader2, Mail, Webhook } from "lucide-react";
import { api, ApiError, formatTime, useMe, type NotificationsResp, type NotifyChannel, type NotifyEventType, type SmtpStatus } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { CopyBlock } from "@/components/CopyBlock";

const KEY = ["notifications"];
const scopeLabel = { all: "all environments", prod: "production only", nonprod: "non-production only" } as const;

const note = (e: unknown) => (e instanceof ApiError ? e.message : "that did not work");

function Select({ label, value, onChange, children }: { label: string; value: string; onChange: (v: string) => void; children: React.ReactNode }) {
  return (
    <label className="block text-xs font-mono text-zinc-400">
      {label}
      <select className="mt-1.5 h-9 w-full rounded-md bg-black px-3 text-xs text-zinc-100" value={value} onChange={(e) => onChange(e.target.value)}>
        {children}
      </select>
    </label>
  );
}

function SmtpCard({ smtp, hasEmailChannels }: { smtp: SmtpStatus; hasEmailChannels: boolean }) {
  const qc = useQueryClient();
  const [f, setF] = useState({ host: smtp.host, port: smtp.port ? String(smtp.port) : "", security: smtp.security || "starttls", user: smtp.user, from: smtp.from });
  const [password, setPassword] = useState("");
  const [changePw, setChangePw] = useState(false);
  const [to, setTo] = useState("");
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);

  const body = () => ({
    host: f.host,
    port: f.port ? Number(f.port) : 0,
    security: f.security,
    user: f.user,
    from: f.from,
    ...(changePw || !smtp.has_password ? { password } : {}),
  });
  const save = useMutation({
    mutationFn: () => api<{ smtp: SmtpStatus }>("/api/admin/notifications/smtp", { method: "PUT", json: body() }),
    onSuccess: () => {
      setMsg({ ok: true, text: "Saved. Send a test email to check it." });
      setPassword("");
      setChangePw(false);
      void qc.invalidateQueries({ queryKey: KEY });
    },
    onError: (e) => setMsg({ ok: false, text: note(e) }),
  });
  const test = useMutation({
    mutationFn: () => api<{ ok: boolean; error?: string }>("/api/admin/notifications/smtp/test", { method: "POST", json: { to } }),
    onSuccess: (r) => setMsg(r.ok ? { ok: true, text: `A test email was sent to ${to}.` } : { ok: false, text: r.error ?? "sending failed" }),
    onError: (e) => setMsg({ ok: false, text: note(e) }),
  });
  const remove = useMutation({
    mutationFn: () => api("/api/admin/notifications/smtp", { method: "DELETE" }),
    onSuccess: () => {
      setF({ host: "", port: "", security: "starttls", user: "", from: "" });
      setMsg({ ok: true, text: "The mail server settings were removed." });
      void qc.invalidateQueries({ queryKey: KEY });
    },
  });
  const busy = save.isPending || test.isPending || remove.isPending;

  return (
    <Card role="region" aria-label="Mail server (SMTP)" className="space-y-5 p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <Mail className="mt-0.5 size-5 text-zinc-400" strokeWidth={1.5} />
          <div>
            <h2 className="text-base font-semibold text-white">Mail server (SMTP)</h2>
            <p className="mt-1 max-w-2xl text-sm text-zinc-400">Needed only for email channels. Use any SMTP service: your provider's, a relay, or your own server. The password is stored encrypted and is never shown again.</p>
          </div>
        </div>
        {smtp.configured ? <Badge tone="ok">set up</Badge> : <Badge tone={hasEmailChannels ? "warn" : "default"}>{hasEmailChannels ? "needed by an email channel" : "not set up"}</Badge>}
      </div>
      <form
        className="grid gap-3.5 sm:grid-cols-2"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <label className="block text-xs font-mono text-zinc-400">
          Server
          <Input className="mt-1.5 bg-black" placeholder="smtp.example.com" value={f.host} onChange={(e) => setF({ ...f, host: e.target.value })} autoComplete="off" />
        </label>
        <div className="grid grid-cols-2 gap-3.5">
          <label className="block text-xs font-mono text-zinc-400">
            Port
            <Input className="mt-1.5 bg-black" placeholder="587" inputMode="numeric" value={f.port} onChange={(e) => setF({ ...f, port: e.target.value })} />
          </label>
          <Select label="Connection" value={f.security} onChange={(v) => setF({ ...f, security: v as SmtpStatus["security"] })}>
            <option value="starttls">STARTTLS (587)</option>
            <option value="tls">TLS (465)</option>
            <option value="none">None (25)</option>
          </Select>
        </div>
        <label className="block text-xs font-mono text-zinc-400">
          Username <span className="text-zinc-600">(if the server asks for a login)</span>
          <Input className="mt-1.5 bg-black" value={f.user} onChange={(e) => setF({ ...f, user: e.target.value })} autoComplete="off" />
        </label>
        <label className="block text-xs font-mono text-zinc-400">
          Password
          {smtp.has_password && !changePw ? (
            <div className="mt-1.5 flex h-9 items-center justify-between rounded-md bg-black px-3 text-xs text-zinc-500">
              saved
              <button type="button" onClick={() => setChangePw(true)} className="cursor-pointer text-sky-400 hover:text-sky-300">
                change
              </button>
            </div>
          ) : (
            <Input type="password" className="mt-1.5 bg-black" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
          )}
        </label>
        <label className="block text-xs font-mono text-zinc-400 sm:col-span-2">
          Send from
          <Input className="mt-1.5 bg-black" placeholder="Helmsman <helmsman@example.com>" value={f.from} onChange={(e) => setF({ ...f, from: e.target.value })} />
        </label>
        <div className="flex flex-wrap items-center gap-2.5 sm:col-span-2">
          <Button type="submit" disabled={busy || !f.host || !f.from}>
            {save.isPending && <Loader2 className="size-3.5 animate-spin" strokeWidth={2} />}
            Save
          </Button>
          {smtp.configured && (
            <>
              <Input className="w-64 bg-black" type="email" placeholder="send a test to you@example.com" aria-label="Send a test email to" value={to} onChange={(e) => setTo(e.target.value)} />
              <Button type="button" variant="secondary" disabled={busy || !to} onClick={() => test.mutate()}>
                {test.isPending && <Loader2 className="size-3.5 animate-spin" strokeWidth={2} />}
                Send test email
              </Button>
              <Button type="button" variant="ghost" disabled={busy} onClick={() => remove.mutate()}>
                Remove
              </Button>
            </>
          )}
        </div>
      </form>
      {msg && (
        <p role={msg.ok ? "status" : "alert"} className={`rounded-lg p-3 text-sm ${msg.ok ? "bg-emerald-500/10 text-emerald-300" : "bg-rose-500/10 text-rose-300"}`}>
          {msg.text}
        </p>
      )}
    </Card>
  );
}

type Draft = { id?: number; name: string; kind: "email" | "webhook"; to: string; url: string; secret: string; events: string[]; tier: NotifyChannel["tier"]; enabled: boolean };

const blank = (types: NotifyEventType[]): Draft => ({ name: "", kind: "email", to: "", url: "", secret: "", events: types.filter((t) => t.default).map((t) => t.id), tier: "all", enabled: true });

function ChannelForm({ types, draft, onDone }: { types: NotifyEventType[]; draft: Draft; onDone: (secret?: string) => void }) {
  const qc = useQueryClient();
  const [d, setD] = useState(draft);
  const [err, setErr] = useState<string | null>(null);
  const editing = d.id !== undefined;
  const save = useMutation({
    mutationFn: () => {
      const body = { name: d.name, kind: d.kind, to: d.to.split(/[\s,;]+/).filter(Boolean), url: d.url, secret: d.secret, events: d.events, tier: d.tier, enabled: d.enabled };
      return editing
        ? api<{ channel: NotifyChannel }>(`/api/admin/notifications/channels/${d.id}`, { method: "PUT", json: body })
        : api<{ channel: NotifyChannel; secret?: string }>("/api/admin/notifications/channels", { method: "POST", json: body });
    },
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: KEY });
      onDone((r as { secret?: string }).secret);
    },
    onError: (e) => setErr(note(e)),
  });
  const toggle = (id: string) => setD((x) => ({ ...x, events: x.events.includes(id) ? x.events.filter((e) => e !== id) : [...x.events, id] }));

  return (
    <form
      className="grid gap-3.5 rounded-lg bg-[#101014] p-5 sm:grid-cols-2"
      aria-label={editing ? "Edit channel" : "New channel"}
      onSubmit={(e) => {
        e.preventDefault();
        setErr(null);
        save.mutate();
      }}
    >
      <label className="block text-xs font-mono text-zinc-400">
        Name
        <Input className="mt-1.5 bg-black" placeholder="On-call email" value={d.name} onChange={(e) => setD({ ...d, name: e.target.value })} />
      </label>
      <Select label="Type" value={d.kind} onChange={(v) => !editing && setD({ ...d, kind: v as Draft["kind"] })}>
        <option value="email">Email</option>
        <option value="webhook">Webhook</option>
      </Select>
      {d.kind === "email" ? (
        <label className="block text-xs font-mono text-zinc-400 sm:col-span-2">
          Recipients <span className="text-zinc-600">(separate with commas or new lines, up to 20)</span>
          <Input className="mt-1.5 bg-black" placeholder="oncall@example.com, lead@example.com" value={d.to} onChange={(e) => setD({ ...d, to: e.target.value })} />
        </label>
      ) : (
        <>
          <label className="block text-xs font-mono text-zinc-400 sm:col-span-2">
            Webhook address
            <Input className="mt-1.5 bg-black" placeholder="https://hooks.example.com/helmsman" value={d.url} onChange={(e) => setD({ ...d, url: e.target.value })} />
          </label>
          <label className="block text-xs font-mono text-zinc-400 sm:col-span-2">
            Signing secret <span className="text-zinc-600">({editing ? "leave empty to keep the current one" : "leave empty to generate one"})</span>
            <Input className="mt-1.5 bg-black" type="password" autoComplete="new-password" value={d.secret} onChange={(e) => setD({ ...d, secret: e.target.value })} />
          </label>
        </>
      )}
      <Select label="Which environments" value={d.tier} onChange={(v) => setD({ ...d, tier: v as Draft["tier"] })}>
        <option value="all">All environments</option>
        <option value="prod">Production only</option>
        <option value="nonprod">Non-production only</option>
      </Select>
      <label className="flex items-end gap-2 pb-2 text-sm text-zinc-300">
        <input type="checkbox" className="size-4 accent-sky-400" checked={d.enabled} onChange={(e) => setD({ ...d, enabled: e.target.checked })} />
        Turned on
      </label>
      <fieldset className="sm:col-span-2">
        <legend className="mb-2 text-xs font-mono text-zinc-400">Tell this channel when</legend>
        <div className="grid gap-2 sm:grid-cols-2">
          {types.map((t) => (
            <label key={t.id} className="flex items-center gap-2 text-sm text-zinc-300">
              <input type="checkbox" className="size-4 accent-sky-400" checked={d.events.includes(t.id)} onChange={() => toggle(t.id)} />
              {t.label}
            </label>
          ))}
        </div>
      </fieldset>
      {err && (
        <p role="alert" className="rounded-lg bg-rose-500/10 p-3 text-sm text-rose-300 sm:col-span-2">
          {err}
        </p>
      )}
      <div className="flex gap-2.5 sm:col-span-2">
        <Button type="submit" disabled={save.isPending}>
          {save.isPending && <Loader2 className="size-3.5 animate-spin" strokeWidth={2} />}
          {editing ? "Save changes" : "Add channel"}
        </Button>
        <Button type="button" variant="ghost" onClick={() => onDone()}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

function ChannelRow({ c, types, onEdit }: { c: NotifyChannel; types: NotifyEventType[]; onEdit: () => void }) {
  const qc = useQueryClient();
  const [res, setRes] = useState<{ ok: boolean; text: string } | null>(null);
  const [del, setDel] = useState(false);
  const test = useMutation({
    mutationFn: () => api<{ ok: boolean; error?: string }>(`/api/admin/notifications/channels/${c.id}/test`, { method: "POST" }),
    onSuccess: (r) => setRes(r.ok ? { ok: true, text: "The test was delivered." } : { ok: false, text: r.error ?? "it failed" }),
    onError: (e) => setRes({ ok: false, text: note(e) }),
  });
  const remove = useMutation({
    mutationFn: () => api(`/api/admin/notifications/channels/${c.id}`, { method: "DELETE" }),
    onSuccess: () => {
      setDel(false);
      void qc.invalidateQueries({ queryKey: KEY });
    },
  });
  const label = (id: string) => types.find((t) => t.id === id)?.label ?? id;
  return (
    <li className="space-y-2.5 px-5 py-4" aria-label={`Channel ${c.name}`}>
      <div className="flex flex-wrap items-center gap-3">
        {c.kind === "email" ? <Mail className="size-4 text-zinc-400" strokeWidth={1.5} /> : <Webhook className="size-4 text-zinc-400" strokeWidth={1.5} />}
        <span className="text-sm font-semibold text-white">{c.name}</span>
        {!c.enabled && <Badge tone="default">off</Badge>}
        <span className="text-xs font-mono text-zinc-500">{scopeLabel[c.tier]}</span>
        <div className="ml-auto flex gap-2">
          <Button size="sm" variant="outline" onClick={() => test.mutate()} disabled={test.isPending}>
            {test.isPending && <Loader2 className="size-3.5 animate-spin" strokeWidth={2} />}
            Send test
          </Button>
          <Button size="sm" variant="outline" onClick={onEdit}>
            Edit
          </Button>
          <Button size="sm" variant="destructive" onClick={() => setDel(true)}>
            Delete
          </Button>
        </div>
      </div>
      <div className="break-all font-mono text-xs text-zinc-400">{c.kind === "email" ? (c.to ?? []).join(", ") : c.url}</div>
      <div className="flex flex-wrap gap-1.5">
        {c.events.map((e) => (
          <span key={e} className="rounded bg-[#141418] px-2 py-0.5 text-xs text-zinc-300">
            {label(e)}
          </span>
        ))}
      </div>
      {res && (
        <p role={res.ok ? "status" : "alert"} className={`rounded-lg p-2.5 text-sm ${res.ok ? "bg-emerald-500/10 text-emerald-300" : "bg-rose-500/10 text-rose-300"}`}>
          {res.text}
        </p>
      )}
      <ConfirmDialog
        open={del}
        title={`Delete ${c.name}?`}
        body={<p>It stops receiving notifications and its delivery history is removed.</p>}
        confirmLabel="Delete channel"
        busy={remove.isPending}
        onConfirm={() => remove.mutate()}
        onCancel={() => setDel(false)}
      />
    </li>
  );
}

export default function Notifications() {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const q = useQuery<NotificationsResp>({ queryKey: KEY, queryFn: () => api("/api/admin/notifications"), refetchInterval: 10_000 });
  const [form, setForm] = useState<Draft | null>(null);
  const [secret, setSecret] = useState<string | null>(null);

  if (q.isLoading) return <div className="py-12 text-center text-xs font-mono text-zinc-500">loading</div>;
  if (q.isError || !q.data) return <div className="py-12 text-center text-xs font-mono text-rose-300">could not load notifications</div>;
  const { smtp, channels, deliveries, events } = q.data;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Notifications</h1>
          <p className="mt-1 max-w-2xl text-sm text-zinc-400">
            Tell people when something needs them: a failed deployment or migration, an environment that went down, a crash loop, a production run waiting for approval, an agent that went offline. Notifications go to email recipients or a webhook, and
            never contain log text.
          </p>
        </div>
        <Button
          onClick={() => {
            setSecret(null);
            setForm(blank(events));
          }}
        >
          <Bell className="size-4" strokeWidth={1.5} />
          Add channel
        </Button>
      </div>

      {secret && (
        <Card className="space-y-3 bg-[#121218] p-5" role="status" aria-label="Signing secret">
          <CopyBlock title="Webhook signing secret" hint="Shown once. Your receiver checks the X-Helmsman-Signature header with it: sha256=HMAC-SHA256(secret, timestamp + '.' + body), timestamp from X-Helmsman-Timestamp." text={secret} />
          <Button size="sm" variant="secondary" onClick={() => setSecret(null)}>
            I have saved it
          </Button>
        </Card>
      )}

      {form && (
        <ChannelForm
          key={form.id ?? "new"}
          types={events}
          draft={form}
          onDone={(s) => {
            setForm(null);
            if (s) setSecret(s);
          }}
        />
      )}

      <Card className="p-0" aria-label="Channels">
        <h2 className="px-5 pt-4 pb-2 text-xs font-semibold uppercase tracking-wider text-zinc-400">Channels</h2>
        {channels.length === 0 ? (
          <p className="px-5 pb-5 text-sm text-zinc-500">No channel yet. Add one to start receiving notifications.</p>
        ) : (
          <ul className="divide-y divide-zinc-900/60">
            {channels.map((c) => (
              <ChannelRow
                key={c.id}
                c={c}
                types={events}
                onEdit={() => {
                  setSecret(null);
                  setForm({ id: c.id, name: c.name, kind: c.kind, to: (c.to ?? []).join(", "), url: c.url ?? "", secret: "", events: c.events, tier: c.tier, enabled: c.enabled });
                }}
              />
            ))}
          </ul>
        )}
      </Card>

      <SmtpCard key={smtp.host + smtp.port + smtp.from + smtp.user} smtp={smtp} hasEmailChannels={channels.some((c) => c.kind === "email")} />

      <Card className="p-0" aria-label="Recent deliveries">
        <h2 className="px-5 pt-4 pb-2 text-xs font-semibold uppercase tracking-wider text-zinc-400">Recent deliveries</h2>
        {deliveries.length === 0 ? (
          <p className="px-5 pb-5 text-sm text-zinc-500">Nothing has been sent yet. A failed send is retried up to four times.</p>
        ) : (
          <ul className="divide-y divide-zinc-900/60">
            {deliveries.map((d) => (
              <li key={d.id} className="flex flex-wrap items-center gap-3 px-5 py-2.5 text-sm">
                <Badge tone={d.status === "sent" ? "ok" : d.status === "failed" ? "bad" : "warn"}>{d.status}</Badge>
                <span className="min-w-0 flex-1 truncate text-zinc-200">{d.title}</span>
                <span className="font-mono text-xs text-zinc-500">{d.channel}</span>
                <span className="font-mono text-xs text-zinc-500">{formatTime(d.created_at, tz)}</span>
                {d.last_error && d.status !== "sent" && <span className="basis-full text-xs text-rose-300">{d.last_error}</span>}
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}
