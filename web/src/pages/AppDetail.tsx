import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  RotateCw,
  ExternalLink,
  Copy,
  Check,
} from "lucide-react";
import { api, formatTime, useMe, type App, type Env, type EnvStatus } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, Td, Th, Tr } from "@/components/ui/table";
import { StatusBadge } from "@/components/StatusBadge";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { LogViewer } from "@/components/LogViewer";

const reasonText: Record<string, string> = {
  agent_offline: "The host agent on this server is offline.",
  docker_unavailable: "Docker is not reachable on the host.",
  container_missing: "No container matches this app's compose service.",
};

export default function AppDetail() {
  const { appId } = useParams();
  const [selected, setSelected] = useState<string | null>(null);

  const { data, isPending, error } = useQuery<App>({
    queryKey: ["app", appId],
    queryFn: () => api(`/api/apps/${appId}`),
    refetchInterval: 15_000,
  });

  if (isPending) {
    return (
      <div className="py-20 text-xs text-zinc-500 font-mono">
        loading...
      </div>
    );
  }

  if (error || !data) {
    return (
      <Card className="bg-[#0a0a0d]">
        <h2 className="text-sm font-semibold text-amber-400">Not Found</h2>
        <p className="text-xs text-zinc-400 mt-1">
          App not found or access restricted.
        </p>
        <Link to="/" className="inline-block text-xs text-zinc-300 hover:text-white mt-3 underline">
          Back to apps
        </Link>
      </Card>
    );
  }

  const env = data.environments.find((e) => e.env === selected) ?? data.environments[0];

  return (
    <div className="space-y-6">
      {/* App Header */}
      <div className="flex flex-col sm:flex-row sm:items-baseline justify-between gap-3">
        <div className="flex items-baseline gap-3.5 flex-wrap">
          <h1 className="text-2xl font-bold text-white tracking-tight">
            {data.name}
          </h1>
          <span className="text-sm text-zinc-400 font-mono">{data.repo}</span>
          {env.url && (
            <a
              href={env.url}
              target="_blank"
              rel="noreferrer"
              className="text-sm text-zinc-300 hover:text-white transition-colors flex items-center gap-1.5 font-mono"
            >
              <span>{env.url.replace(/^https?:\/\//, "")}</span>
              <ExternalLink className="size-3.5 text-zinc-500" strokeWidth={1.5} />
            </a>
          )}
        </div>
      </div>

      {/* Environment Switcher Tabs */}
      <div
        role="tablist"
        aria-label="Environments"
        className="flex items-center gap-6 border-b border-zinc-900 pb-3 text-sm font-mono"
      >
        {data.environments.map((e) => {
          const isActive = e.env === env.env;
          return (
            <button
              key={e.env}
              role="tab"
              aria-selected={isActive}
              onClick={() => setSelected(e.env)}
              className={`flex items-center gap-2 transition-colors cursor-pointer select-none ${
                isActive
                  ? "text-white font-semibold"
                  : "text-zinc-500 hover:text-zinc-300"
              }`}
            >
              {isActive && (
                <span className="size-2 rounded-full bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.5)]" />
              )}
              <span>{e.env}</span>
              {e.tier === "prod" && <span className="text-[11px] text-zinc-500 font-semibold">prod</span>}
            </button>
          );
        })}
      </div>

      <EnvInspector app={data.id} env={env} />
    </div>
  );
}

function EnvInspector({ app, env }: { app: string; env: Env }) {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const qc = useQueryClient();
  const [activeTab, setActiveTab] = useState<"logs" | "events">("logs");
  const [copiedSha, setCopiedSha] = useState(false);

  const q = useQuery<EnvStatus>({
    queryKey: ["status", app, env.env],
    queryFn: () => api(`/api/apps/${app}/envs/${env.env}/status`),
    refetchInterval: 10_000,
  });

  const [confirm, setConfirm] = useState(false);
  const [done, setDone] = useState<string | null>(null);

  const restart = useMutation({
    mutationFn: () =>
      api<{ message: string }>(`/api/apps/${app}/envs/${env.env}/restart`, { method: "POST" }),
    onSuccess: () => {
      setConfirm(false);
      setDone("Restart requested.");
      qc.invalidateQueries({ queryKey: ["status", app, env.env] });
      setTimeout(() => setDone(null), 4000);
    },
  });

  if (q.isPending) {
    return (
      <div className="py-12 text-zinc-500 text-xs font-mono">
        querying status...
      </div>
    );
  }

  if (q.error || !q.data) {
    return (
      <div className="text-rose-400 text-xs font-mono py-4">
        Status unavailable.
      </div>
    );
  }

  const s = q.data;
  const c = s.container;
  const lastProbe = s.health.latest;

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopiedSha(true);
    setTimeout(() => setCopiedSha(false), 2000);
  };

  return (
    <div className="space-y-6">
      {/* Top Action & Reason Line */}
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <StatusBadge status={s.status} />
          {s.crash_loop && (
            <Badge tone="bad">crash loop</Badge>
          )}
          {s.agent.outdated && <Badge tone="warn">agent outdated</Badge>}
        </div>

        <Button
          variant={env.tier === "prod" ? "destructive" : "default"}
          size="default"
          onClick={() => setConfirm(true)}
          disabled={!s.can_restart}
          title={s.blocked.restart || "Restart container"}
        >
          <RotateCw className={`size-3.5 ${restart.isPending ? "animate-spin" : ""}`} strokeWidth={1.5} />
          <span>Restart container</span>
        </Button>
      </div>

      {s.reason && (
        <div className="rounded-lg bg-amber-500/10 p-3 text-xs text-amber-200 font-mono">
          {reasonText[s.reason]} {s.reason === "agent_offline" && <>Last seen {formatTime(s.agent.last_seen, tz)}.</>}
        </div>
      )}

      {done && (
        <div className="rounded-lg bg-emerald-500/10 p-2.5 text-xs text-emerald-300 font-mono">
          {done}
        </div>
      )}

      {/* Standout Telemetry Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <div className="rounded-xl bg-[#0a0a0d] p-4.5 space-y-1.5 transition-colors hover:bg-[#101014]">
          <div className="text-xs font-mono font-medium text-zinc-400 uppercase tracking-wider">Health probe</div>
          <div className="flex items-baseline gap-2">
            {lastProbe ? (
              <>
                <span className="text-2xl font-bold font-mono text-white tracking-tight">
                  {lastProbe.latency_ms} ms
                </span>
                <span className="text-xs font-mono text-emerald-400 font-medium">
                  HTTP {lastProbe.http_code}
                </span>
              </>
            ) : (
              <span className="text-xl font-medium text-zinc-500 font-mono">no probe</span>
            )}
          </div>
        </div>

        <div className="rounded-xl bg-[#0a0a0d] p-4.5 space-y-1.5 transition-colors hover:bg-[#101014]">
          <div className="text-xs font-mono font-medium text-zinc-400 uppercase tracking-wider">Container</div>
          <div className="flex items-baseline gap-2">
            {c ? (
              <>
                <span className="text-2xl font-bold text-white tracking-tight capitalize">
                  {c.state}
                </span>
                <span className="text-xs font-mono text-zinc-400">
                  ({c.restart_count} restart{c.restart_count === 1 ? "" : "s"})
                </span>
              </>
            ) : (
              <span className="text-xl font-medium text-zinc-500 font-mono">none</span>
            )}
          </div>
        </div>

        <div className="rounded-xl bg-[#0a0a0d] p-4.5 space-y-1.5 transition-colors hover:bg-[#101014]">
          <div className="text-xs font-mono font-medium text-zinc-400 uppercase tracking-wider">Revision</div>
          <div className="flex items-center gap-2">
            {c?.revision ? (
              <>
                <span className="text-2xl font-bold font-mono text-white tracking-tight">
                  {c.revision.slice(0, 7)}
                </span>
                <button
                  onClick={() => copyToClipboard(c.revision)}
                  className="rounded p-1 text-zinc-500 hover:text-white transition-colors"
                  title="Copy SHA"
                >
                  {copiedSha ? <Check className="size-3.5 text-emerald-400" /> : <Copy className="size-3.5" strokeWidth={1.5} />}
                </button>
              </>
            ) : (
              <span className="text-xl font-medium text-zinc-500 font-mono">—</span>
            )}
          </div>
        </div>

        <div className="rounded-xl bg-[#0a0a0d] p-4.5 space-y-1.5 transition-colors hover:bg-[#101014]">
          <div className="text-xs font-mono font-medium text-zinc-400 uppercase tracking-wider">Host agent</div>
          <div className="flex items-baseline gap-2">
            {s.agent.online ? (
              <>
                <span className="text-2xl font-bold font-mono text-white tracking-tight">online</span>
                <span className="text-xs font-mono text-zinc-400">v{s.agent.version}</span>
              </>
            ) : (
              <span className="text-2xl font-bold font-mono text-zinc-500 tracking-tight">offline</span>
            )}
          </div>
        </div>
      </div>

      {/* Activity Lower Stage (Logs & Events) */}
      <div className="space-y-3 pt-2">
        <div className="flex items-center gap-6 border-b border-zinc-900 pb-2.5 text-sm font-mono">
          <button
            onClick={() => setActiveTab("logs")}
            className={`transition-colors cursor-pointer ${
              activeTab === "logs"
                ? "text-white font-semibold"
                : "text-zinc-500 hover:text-zinc-300"
            }`}
          >
            logs
          </button>
          <button
            onClick={() => setActiveTab("events")}
            className={`transition-colors cursor-pointer ${
              activeTab === "events"
                ? "text-white font-semibold"
                : "text-zinc-500 hover:text-zinc-300"
            }`}
          >
            events ({s.events.length})
          </button>
        </div>

        {activeTab === "logs" ? (
          <LogViewer
            app={app}
            env={env.env}
            tz={tz}
            disabledReason={s.blocked.logs || undefined}
          />
        ) : (
          <div>
            {s.events.length === 0 ? (
              <div className="py-12 text-center text-xs text-zinc-500 font-mono">
                no events recorded
              </div>
            ) : (
              <Table>
                <thead>
                  <Tr>
                    <Th>Time</Th>
                    <Th>Event</Th>
                    <Th>Detail</Th>
                  </Tr>
                </thead>
                <tbody>
                  {s.events.map((e, i) => (
                    <Tr key={i}>
                      <Td className="font-mono text-xs text-zinc-400">
                        {formatTime(e.at, tz)}
                      </Td>
                      <Td>
                        <Badge
                          tone={
                            e.event === "die" || e.event === "oom"
                              ? "bad"
                              : e.event === "start" || e.event === "restart"
                              ? "ok"
                              : "default"
                          }
                        >
                          {e.event}
                        </Badge>
                      </Td>
                      <Td className="font-mono text-xs text-zinc-200">
                        {e.detail || "—"}
                      </Td>
                    </Tr>
                  ))}
                </tbody>
              </Table>
            )}
          </div>
        )}
      </div>

      {/* Confirm Dialog */}
      <ConfirmDialog
        open={confirm}
        title={`Restart ${app}/${env.env}?`}
        body={
          env.tier === "prod" ? (
            <span className="text-rose-400">
              This is a production environment. Active connections may be disrupted.
            </span>
          ) : (
            `The container running ${app} on ${env.env} will be restarted.`
          )
        }
        confirmLabel="Restart container"
        requireText={env.tier === "prod" ? env.env : undefined}
        busy={restart.isPending}
        error={restart.error ? (restart.error as Error).message : null}
        onConfirm={() => restart.mutate()}
        onCancel={() => setConfirm(false)}
      />
    </div>
  );
}
