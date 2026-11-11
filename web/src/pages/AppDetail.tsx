import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, formatTime, useMe, type App, type Env, type EnvStatus } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, Td, Th } from "@/components/ui/table";
import { StatusBadge } from "@/components/StatusBadge";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { LogViewer } from "@/components/LogViewer";

const reasonText: Record<string, string> = {
  agent_offline: "The agent on this environment's server is offline, so the real state is unknown.",
  docker_unavailable: "The agent is connected but cannot reach Docker on the server.",
  container_missing: "The agent is connected, but no container matches this app's compose project and service.",
};

export default function AppDetail() {
  const { appId } = useParams();
  const [selected, setSelected] = useState<string | null>(null);
  const { data, isPending, error } = useQuery<App>({ queryKey: ["app", appId], queryFn: () => api(`/api/apps/${appId}`), refetchInterval: 15_000 });
  if (isPending) return <p className="text-sm text-muted">Loading</p>;
  if (error) {
    return (
      <Card>
        <p className="text-sm">App not found, or you do not have access to it.</p>
        <Link to="/" className="text-sm text-primary underline">Back to apps</Link>
      </Card>
    );
  }
  const env = data.environments.find((e) => e.env === selected) ?? data.environments[0];
  return (
    <div className="space-y-4">
      <div>
        <Link to="/" className="text-sm text-muted hover:text-foreground">Apps</Link>
        <h1 className="text-xl font-semibold">{data.name}</h1>
        <p className="text-sm text-muted">{data.repo}</p>
      </div>
      <div role="tablist" aria-label="Environments" className="flex flex-wrap gap-2">
        {data.environments.map((e) => (
          <button key={e.env} role="tab" aria-selected={e.env === env.env} onClick={() => setSelected(e.env)}
            className={`flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm ${e.env === env.env ? "border-primary bg-card" : "border-border text-muted hover:text-foreground"}`}>
            {e.env} <StatusBadge status={e.status} />
          </button>
        ))}
      </div>
      <EnvPanel app={data.id} env={env} />
    </div>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs text-muted">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}

function EnvPanel({ app, env }: { app: string; env: Env }) {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const qc = useQueryClient();
  const q = useQuery<EnvStatus>({
    queryKey: ["status", app, env.env],
    queryFn: () => api(`/api/apps/${app}/envs/${env.env}/status`),
    refetchInterval: 10_000,
  });
  const [confirm, setConfirm] = useState(false);
  const [done, setDone] = useState<string | null>(null);
  const restart = useMutation({
    mutationFn: () => api<{ message: string }>(`/api/apps/${app}/envs/${env.env}/restart`, { method: "POST" }),
    onSuccess: () => {
      setConfirm(false);
      setDone("Restart requested and completed.");
      qc.invalidateQueries({ queryKey: ["status", app, env.env] });
    },
  });
  if (q.isPending) return <p className="text-sm text-muted">Loading status</p>;
  if (q.error) return <Card><p className="text-sm">Could not load the status for this environment.</p></Card>;
  const s = q.data;
  const c = s.container;
  const lastProbe = s.health.latest;
  return (
    <div className="space-y-4">
      <Card>
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <h2 className="text-lg font-semibold">{env.env}</h2>
          <Badge tone={env.tier === "prod" ? "prod" : "default"}>{env.tier}</Badge>
          <StatusBadge status={s.status} />
          {s.crash_loop && <Badge tone="bad">crash loop</Badge>}
          {s.agent.outdated && <Badge tone="warn">agent outdated</Badge>}
          <a className="ml-auto text-sm text-primary underline" href={env.url} target="_blank" rel="noreferrer">{env.url}</a>
        </div>
        {s.reason && (
          <p role="status" className="mb-3 rounded-md border border-amber-500/50 p-2 text-sm">
            {reasonText[s.reason]} {s.reason === "agent_offline" && <>Last seen {formatTime(s.agent.last_seen, tz)}.</>}
          </p>
        )}
        {s.crash_loop && (
          <p role="alert" className="mb-3 rounded-md border border-red-500/50 p-2 text-sm">
            This container has exited three or more times in the last 10 minutes. Check the logs before restarting again.
          </p>
        )}
        <dl className="grid gap-3 sm:grid-cols-3">
          <Fact label="Container">{c ? <>{c.state}{c.health && <span className="text-muted"> ({c.health})</span>}</> : "unknown"}</Fact>
          <Fact label="Deployed revision">{c?.revision ? <code title={c.revision}>{c.revision.slice(0, 12)}</code> : <span className="text-muted">not labelled</span>}</Fact>
          <Fact label="Image">{c?.image ? <span className="break-all">{c.image}</span> : "unknown"}</Fact>
          <Fact label="Last started">{c?.started_at ? formatTime(c.started_at, tz) : "unknown"}</Fact>
          <Fact label="Restart count">{c ? c.restart_count : "unknown"}</Fact>
          <Fact label="Health check">
            {!s.health.configured ? <span className="text-muted">none configured</span> : lastProbe
              ? <>{lastProbe.status} ({lastProbe.http_code || "no response"}, {lastProbe.latency_ms} ms) <span className="text-muted">{formatTime(lastProbe.checked_at, tz)}</span></>
              : <span className="text-muted">waiting for the first check</span>}
          </Fact>
          <Fact label="Agent">
            {s.agent.online ? <Badge tone="ok">online</Badge> : <Badge>offline</Badge>}{" "}
            <span className="text-muted">{s.agent.version}</span>
          </Fact>
          <Fact label="Agent last seen">{formatTime(s.agent.last_seen, tz)}</Fact>
          <Fact label="Seeds">{env.allow_seed ? "allowed" : "blocked"}</Fact>
        </dl>
        <div className="mt-4 flex flex-wrap items-center gap-3">
          {s.can_restart && (
            <Button variant="outline" onClick={() => { setDone(null); restart.reset(); setConfirm(true); }} disabled={!!s.blocked.restart}>
              Restart
            </Button>
          )}
          {s.blocked.restart && <span className="text-sm text-muted">{s.blocked.restart}</span>}
          {done && <span role="status" className="text-sm text-green-700 dark:text-green-400">{done}</span>}
        </div>
      </Card>

      {s.can_logs && (
        <Card>
          <h3 className="mb-2 font-medium">Logs</h3>
          <LogViewer key={`${app}/${env.env}`} app={app} env={env.env} tz={tz} disabledReason={s.blocked.logs} />
        </Card>
      )}

      <div>
        <h3 className="mb-2 font-medium">Restart history (14 days)</h3>
        {s.events.length === 0 ? (
          <p className="text-sm text-muted">No container events recorded yet.</p>
        ) : (
          <Table>
            <thead><tr><Th>Received</Th><Th>Event</Th><Th>Detail</Th><Th>Agent clock</Th></tr></thead>
            <tbody>
              {s.events.map((e, i) => (
                <tr key={i}>
                  <Td>{formatTime(e.at, tz)}</Td>
                  <Td>{e.event}</Td>
                  <Td>{e.detail ?? ""}</Td>
                  <Td className="text-muted">{formatTime(e.agent_ts, tz)}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </div>

      <ConfirmDialog
        open={confirm}
        title={`Restart ${app} (${env.env})?`}
        body={<>
          <p>The container is stopped and started again. In-flight requests may fail.</p>
          {env.tier === "prod" && <p className="mt-2 font-medium text-red-600 dark:text-red-400">This is a production environment.</p>}
          <p className="mt-2 text-muted">This action is recorded in the audit log.</p>
        </>}
        confirmLabel="Restart"
        requireText={env.tier === "prod" ? env.env : undefined}
        busy={restart.isPending}
        error={restart.error ? (restart.error as Error).message : null}
        onConfirm={() => restart.mutate()}
        onCancel={() => setConfirm(false)}
      />
    </div>
  );
}
