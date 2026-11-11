import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, formatTime, useMe, type Agent } from "@/lib/api";
import { Table, Td, Th } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ConfirmDialog } from "@/components/ConfirmDialog";

const field = "h-9 rounded-md border border-border bg-card px-2 text-sm";
type Created = { id: string; join_token: string; expires_at: string };

export default function Agents() {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const qc = useQueryClient();
  const agents = useQuery<Agent[]>({ queryKey: ["agents"], queryFn: () => api("/api/admin/agents"), refetchInterval: 10_000 });
  const [form, setForm] = useState({ id: "", name: "" });
  const [err, setErr] = useState<string | null>(null);
  const [token, setToken] = useState<Created | null>(null);
  const [revoke, setRevoke] = useState<Agent | null>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["agents"] });

  const create = useMutation({
    mutationFn: () => api<Created>("/api/admin/agents", { method: "POST", json: form }),
    onSuccess: (c) => { setErr(null); setToken(c); setForm({ id: "", name: "" }); refresh(); },
    onError: (e: Error) => setErr(e.message),
  });
  const reissue = useMutation({
    mutationFn: (id: string) => api<Created>(`/api/admin/agents/${id}/join-token`, { method: "POST" }),
    onSuccess: (c) => { setErr(null); setToken(c); refresh(); },
    onError: (e: Error) => setErr(e.message),
  });
  const doRevoke = useMutation({
    mutationFn: (id: string) => api(`/api/admin/agents/${id}/revoke`, { method: "POST" }),
    onSuccess: () => { setRevoke(null); refresh(); },
  });
  const ack = useMutation({
    mutationFn: (id: string) => api(`/api/admin/agents/${id}/ack-alert`, { method: "POST" }),
    onSuccess: refresh,
  });

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Agents</h1>
      <Card>
        <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); create.mutate(); }}>
          <label className="text-sm">Agent id<br />
            <input className={field} required pattern="[a-z0-9][a-z0-9-]*" title="lowercase letters, digits and dashes"
              value={form.id} onChange={(e) => setForm({ ...form, id: e.target.value })} placeholder="billing-host-1" />
          </label>
          <label className="text-sm">Name<br />
            <input className={`${field} w-64`} required maxLength={100} value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Billing staging server" />
          </label>
          <Button type="submit" disabled={create.isPending}>Create agent</Button>
        </form>
        <p className="mt-2 text-xs text-muted">The id is what catalog/apps.yaml refers to. Creating an agent shows a one-time join token, valid for 1 hour.</p>
        {err && <p role="alert" className="mt-2 text-sm text-red-600 dark:text-red-400">{err}</p>}
      </Card>

      {token && (
        <Card>
          <h2 className="font-medium">Join token for {token.id}</h2>
          <p className="mt-1 text-sm">Copy it now. It is shown once and expires {formatTime(token.expires_at, tz)}.</p>
          <pre className="mt-2 overflow-x-auto rounded-md border border-border bg-background p-2 text-sm" aria-label="Join token">{token.join_token}</pre>
          <p className="mt-2 text-sm text-muted">On the app server set <code>IDP_JOIN_TOKEN</code> in the agent's environment, then start it. See docs/agent-install.md.</p>
          <Button className="mt-2" variant="outline" size="sm" onClick={() => setToken(null)}>I have copied it</Button>
        </Card>
      )}

      <Table>
        <thead><tr><Th>Agent</Th><Th>State</Th><Th>Host</Th><Th>Version</Th><Th>Last seen</Th><Th /></tr></thead>
        <tbody>
          {agents.data?.map((a) => (
            <tr key={a.id}>
              <Td>
                <div className="font-medium">{a.id}</div>
                <div className="text-xs text-muted">{a.name}</div>
                {a.alert && (
                  <div role="alert" className="mt-1 text-xs text-red-600 dark:text-red-400">
                    {a.alert} ({formatTime(a.alert_at, tz)}){" "}
                    <button className="underline" onClick={() => ack.mutate(a.id)}>Dismiss</button>
                  </div>
                )}
              </Td>
              <Td>
                {a.revoked_at ? <Badge tone="bad">revoked</Badge>
                  : a.online ? <Badge tone="ok">online</Badge>
                  : a.joined ? <Badge>offline</Badge>
                  : <Badge tone="warn">{a.join_pending ? "waiting to join" : "token expired"}</Badge>}
                {a.docker_ok === false && <> <Badge tone="warn">docker unavailable</Badge></>}
              </Td>
              <Td>{a.hostname ?? ""}</Td>
              <Td>{a.version ?? ""} {a.outdated && <Badge tone="warn">outdated</Badge>}</Td>
              <Td>{formatTime(a.last_seen_at, tz)}</Td>
              <Td className="space-x-2 whitespace-nowrap">
                {!a.joined && !a.revoked_at && <Button size="sm" variant="outline" onClick={() => reissue.mutate(a.id)}>New join token</Button>}
                {!a.revoked_at && <Button size="sm" variant="outline" onClick={() => setRevoke(a)}>Revoke</Button>}
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>

      <ConfirmDialog
        open={!!revoke}
        title={`Revoke ${revoke?.id}?`}
        body={<p>The agent is disconnected now and its credential stops working. Environments that use it show as unknown until a new agent is set up.</p>}
        confirmLabel="Revoke"
        busy={doRevoke.isPending}
        onConfirm={() => revoke && doRevoke.mutate(revoke.id)}
        onCancel={() => setRevoke(null)}
      />
    </div>
  );
}
