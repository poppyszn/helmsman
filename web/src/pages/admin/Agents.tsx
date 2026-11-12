import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy } from "lucide-react";
import { api, formatTime, useMe, type Agent, type AgentInstall } from "@/lib/api";
import { AgentContainers } from "@/pages/admin/AgentContainers";
import { CopyBlock } from "@/components/CopyBlock";
import { Table, Td, Th, Tr } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { ConfirmDialog } from "@/components/ConfirmDialog";

type Created = { id: string; join_token: string; expires_at: string; install?: AgentInstall };

export default function Agents() {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const qc = useQueryClient();
  const agents = useQuery<Agent[]>({
    queryKey: ["agents"],
    queryFn: () => api("/api/admin/agents"),
    refetchInterval: 10_000,
  });

  const [form, setForm] = useState({ id: "", name: "" });
  const [err, setErr] = useState<string | null>(null);
  const [token, setToken] = useState<Created | null>(null);
  const [revoke, setRevoke] = useState<Agent | null>(null);
  const [copiedToken, setCopiedToken] = useState(false);
  const [hostOf, setHostOf] = useState<string | null>(null);

  const refresh = () => qc.invalidateQueries({ queryKey: ["agents"] });

  const create = useMutation({
    mutationFn: () => api<Created>("/api/admin/agents", { method: "POST", json: form }),
    onSuccess: (c) => {
      setErr(null);
      setToken(c);
      setForm({ id: "", name: "" });
      refresh();
    },
    onError: (e: Error) => setErr(e.message),
  });

  const reissue = useMutation({
    mutationFn: (id: string) => api<Created>(`/api/admin/agents/${id}/join-token`, { method: "POST" }),
    onSuccess: (c) => {
      setErr(null);
      setToken(c);
      refresh();
    },
    onError: (e: Error) => setErr(e.message),
  });

  const doRevoke = useMutation({
    mutationFn: (id: string) => api(`/api/admin/agents/${id}/revoke`, { method: "POST" }),
    onSuccess: () => {
      setRevoke(null);
      refresh();
    },
  });

  const ack = useMutation({
    mutationFn: (id: string) => api(`/api/admin/agents/${id}/ack-alert`, { method: "POST" }),
    onSuccess: refresh,
  });

  const copyJoinToken = (t: string) => {
    navigator.clipboard.writeText(t);
    setCopiedToken(true);
    setTimeout(() => setCopiedToken(false), 2000);
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white tracking-tight">
          Host Agents
        </h1>
      </div>

      {/* Register New Agent Card */}
      <Card className="p-5">
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <label className="text-xs font-mono font-medium text-zinc-400">
            Agent ID<br />
            <Input
              required
              pattern="[a-z0-9][a-z0-9-]*"
              value={form.id}
              onChange={(e) => setForm({ ...form, id: e.target.value })}
              placeholder="e.g. billing-host-1"
              className="w-52 mt-1 font-mono text-xs h-9"
            />
          </label>
          <label className="text-xs font-medium text-zinc-400">
            Display name<br />
            <Input
              required
              maxLength={100}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="Billing staging server"
              className="w-60 mt-1 text-xs h-9 font-sans"
            />
          </label>
          <Button type="submit" size="default" disabled={create.isPending}>
            {create.isPending ? "Creating..." : "Create agent"}
          </Button>
        </form>
        {err && (
          <p role="alert" className="mt-2.5 text-xs text-rose-400 font-mono">
            {err}
          </p>
        )}
      </Card>

      {/* Generated Token Alert */}
      {token && (
        <Card className="bg-[#121218] p-5">
          <div className="flex items-center justify-between text-xs">
            <span className="font-semibold text-white">Join token for {token.id}</span>
            <span className="font-mono text-zinc-400 text-xs">
              expires {formatTime(token.expires_at, tz)}
            </span>
          </div>
          <div className="mt-2.5 flex items-center gap-2 rounded-lg bg-black p-2.5 font-mono text-xs text-zinc-200">
            <span className="flex-1 break-all select-all font-mono-code">{token.join_token}</span>
            <Button
              size="sm"
              variant="outline"
              onClick={() => copyJoinToken(token.join_token)}
            >
              {copiedToken ? <Check className="size-3.5 text-emerald-400" /> : <Copy className="size-3.5" strokeWidth={1.5} />}
              <span>{copiedToken ? "Copied" : "Copy"}</span>
            </Button>
          </div>
          {token.install && (
            <div className="mt-5 space-y-4">
              <p className="text-sm text-zinc-400">
                To start the agent, make a folder on the app's host with these three files, then run the commands. The token is shown only now and works once, for an hour.
              </p>
              <CopyBlock title="agent.yaml" hint="Nothing is exposed until you add it, and the portal drafts the entries from what the agent finds." text={token.install.agent_yaml} />
              <CopyBlock title="compose.agent.yml" text={token.install.compose} />
              <CopyBlock title=".env" hint="chmod 600. Keep it out of git." text={token.install.env} />
              <CopyBlock title="Commands" text={token.install.commands} />
            </div>
          )}
          <div className="mt-3">
            <Button size="sm" variant="secondary" onClick={() => setToken(null)}>
              Dismiss
            </Button>
          </div>
        </Card>
      )}

      {/* Agents Table */}
      <Table>
        <thead>
          <Tr>
            <Th>Agent</Th>
            <Th>Status</Th>
            <Th>Hostname</Th>
            <Th>Version</Th>
            <Th>Last Seen</Th>
            <Th className="text-right">Actions</Th>
          </Tr>
        </thead>
        <tbody>
          {agents.data?.map((a) => (
            <Tr key={a.id}>
              <Td>
                <div className="font-mono text-sm font-semibold text-white">
                  {a.id}
                </div>
                <div className="text-xs text-zinc-400">{a.name}</div>
                {a.alert && (
                  <div
                    role="alert"
                    className="mt-1.5 flex items-center gap-2 text-xs text-rose-400 font-mono"
                  >
                    <span>{a.alert}</span>
                    <button
                      className="underline text-xs text-rose-300 hover:text-white"
                      onClick={() => ack.mutate(a.id)}
                    >
                      Dismiss
                    </button>
                  </div>
                )}
              </Td>
              <Td>
                <div className="flex flex-wrap items-center gap-1.5">
                  {a.revoked_at ? (
                    <Badge tone="bad">revoked</Badge>
                  ) : a.online ? (
                    <Badge tone="ok">online</Badge>
                  ) : a.joined ? (
                    <Badge tone="default">offline</Badge>
                  ) : (
                    <Badge tone="warn">
                      {a.join_pending ? "pending" : "expired"}
                    </Badge>
                  )}
                  {a.docker_ok === false && <Badge tone="warn">docker down</Badge>}
                </div>
              </Td>
              <Td className="font-mono text-xs text-zinc-300">{a.hostname || "-"}</Td>
              <Td className="font-mono text-xs text-zinc-300">
                {a.version ? `v${a.version}` : "-"}
                {a.outdated && <Badge tone="warn" className="ml-1.5">outdated</Badge>}
              </Td>
              <Td className="font-mono text-xs text-zinc-400">
                {formatTime(a.last_seen_at, tz)}
              </Td>
              <Td className="text-right space-x-2 whitespace-nowrap">
                {!a.joined && !a.revoked_at && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => reissue.mutate(a.id)}
                    disabled={reissue.isPending}
                  >
                    New token
                  </Button>
                )}
                {a.joined && !a.revoked_at && (
                  <Button size="sm" variant="outline" onClick={() => setHostOf(hostOf === a.id ? null : a.id)}>
                    {hostOf === a.id ? "Hide containers" : "Containers"}
                  </Button>
                )}
                {!a.revoked_at && (
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => setRevoke(a)}
                  >
                    Revoke
                  </Button>
                )}
              </Td>
            </Tr>
          ))}
        </tbody>
      </Table>

      {hostOf && <AgentContainers agentId={hostOf} />}

      <ConfirmDialog
        open={!!revoke}
        title={`Revoke agent ${revoke?.id}?`}
        body={
          <p>
            The agent connection will be closed and its credential invalidated.
          </p>
        }
        confirmLabel="Revoke Agent"
        busy={doRevoke.isPending}
        onConfirm={() => revoke && doRevoke.mutate(revoke.id)}
        onCancel={() => setRevoke(null)}
      />
    </div>
  );
}
