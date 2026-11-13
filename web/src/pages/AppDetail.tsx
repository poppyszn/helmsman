import { useState } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";
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
import { LogHistory } from "@/components/LogHistory";
import { RunsPanel } from "@/components/RunsPanel";
import { DeploymentsPanel } from "@/components/DeploymentsPanel";
import { TimelinePanel } from "@/components/TimelinePanel";
import { AppTitle } from "@/components/AppTitle";

type Tab = "logs" | "timeline" | "events" | "runs" | "deployments";
const TABS: Tab[] = ["logs", "timeline", "events", "runs", "deployments"];

const reasonText: Record<string, string> = {
  agent_offline: "The host agent on this server is offline.",
  docker_unavailable: "Docker is not reachable on the host.",
  container_missing: "No container matches this app's compose service.",
};

export default function AppDetail() {
  // The address says where you are: /apps/<app>/<environment>/<tab>, so a link or a reload lands on the same view.
  const { appId, env: envParam, tab: tabParam } = useParams();
  const navigate = useNavigate();

  const { data, isPending, error } = useQuery<App>({
    queryKey: ["app", appId],
    queryFn: () => api(`/api/apps/${appId}`),
    refetchInterval: 15_000,
  });
  // The same list the Apps page uses (shared cache), for the switcher between apps of one repository.
  const all = useQuery<App[]>({ queryKey: ["apps"], queryFn: () => api("/api/apps"), staleTime: 15_000 });

  if (isPending) {
    return (
      <div className="space-y-4" aria-busy="true" aria-label="Loading app">
        <div className="h-4 w-40 animate-pulse rounded bg-[#0a0a0d]" />
        <div className="h-8 w-64 animate-pulse rounded bg-[#0a0a0d]" />
        <div className="h-24 animate-pulse rounded-xl bg-[#0a0a0d]" />
        <div className="h-64 animate-pulse rounded-xl bg-[#0a0a0d]" />
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
        <Link to="/" className="inline-block text-xs text-sky-400 hover:text-sky-300 mt-3 underline">
          Back to apps
        </Link>
      </Card>
    );
  }

  const env = data.environments.find((e) => e.env === envParam) ?? data.environments[0];
  const tab = TABS.find((t) => t === tabParam) ?? "logs";
  // An address with an unknown environment or tab is corrected to the nearest real one.
  if ((envParam && env.env !== envParam) || (tabParam && tab !== tabParam)) {
    return <Navigate replace to={envParam && env.env === envParam && tabParam ? `/apps/${data.id}/${env.env}` : `/apps/${data.id}`} />;
  }

  return (
    <div className="space-y-6">
      {/* App Header: the repository above the title, which is also the switcher between its apps */}
      <div className="space-y-2">
        <p className="font-mono text-sm text-zinc-500">
          {data.repo.includes("/") ? (
            <>
              {data.repo.split("/")[0]}/<span className="text-zinc-300">{data.repo.split("/")[1]}</span>
            </>
          ) : (
            data.repo
          )}
        </p>
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
          <AppTitle current={data} apps={all.data ?? []} env={env.env} tab={tabParam} />
          {env.url && (
            <a
              href={env.url}
              target="_blank"
              rel="noreferrer"
              className="text-sm text-sky-400 hover:text-sky-300 transition-colors flex items-center gap-1.5 font-mono"
            >
              <span>{env.url.replace(/^https?:\/\//, "")}</span>
              <ExternalLink className="size-3.5" strokeWidth={1.5} />
            </a>
          )}
        </div>
      </div>

      {/* Quick links: the repository, then the shortcuts from the catalog */}
      <div className="flex flex-wrap items-center gap-2" aria-label="Quick links">
        {[{ name: "Repository", url: `https://github.com/${data.repo}` }, ...(env.links ?? [])].map((l) => (
          <a
            key={`${l.name}-${l.url}`}
            href={l.url}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1.5 rounded-md bg-[#141418] px-2.5 py-1.5 text-xs font-mono text-sky-400 transition-colors hover:bg-[#1a1a22] hover:text-sky-300"
          >
            {l.name}
            <ExternalLink className="size-3" strokeWidth={1.5} />
          </a>
        ))}
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
              onClick={() => navigate(`/apps/${data.id}/${e.env}`)}
              className={`flex items-center gap-2 transition-colors cursor-pointer select-none ${
                isActive
                  ? "text-white font-semibold underline decoration-2 underline-offset-8"
                  : "text-zinc-500 hover:text-zinc-300"
              }`}
            >
              <span>{e.env}</span>
              {e.tier === "prod" && e.env !== "prod" && <span className="text-xs text-zinc-500 font-semibold">prod</span>}
            </button>
          );
        })}
      </div>

      <EnvInspector app={data.id} env={env} tab={tab} onTab={(t) => navigate(`/apps/${data.id}/${env.env}/${t}`)} />
    </div>
  );
}

function EnvInspector({ app, env, tab: activeTab, onTab: setActiveTab }: { app: string; env: Env; tab: Tab; onTab: (t: Tab) => void }) {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const qc = useQueryClient();
  const [logMode, setLogMode] = useState<"live" | "history">("live");
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

  const tabs: { id: Tab; label: string }[] = [
    { id: "logs", label: "logs" },
    { id: "timeline", label: "timeline" },
    { id: "events", label: `events (${s.events.length})` },
    { id: "runs", label: "runs" },
    { id: "deployments", label: "deployments" },
  ];

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
          <div className="flex flex-wrap items-baseline gap-x-2">
            {lastProbe ? (
              <>
                <span className="text-2xl font-bold font-mono text-white tracking-tight whitespace-nowrap">
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
          <div className="flex flex-wrap items-baseline gap-x-2">
            {c ? (
              <>
                <span className="text-2xl font-bold text-white tracking-tight capitalize">
                  {c.state}
                </span>
                <span className="text-xs font-mono text-zinc-400 whitespace-nowrap">
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
              <span className="text-xl font-medium text-zinc-500 font-mono">-</span>
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
          {tabs.map((t) => (
            <button
              key={t.id}
              onClick={() => setActiveTab(t.id)}
              className={`transition-colors cursor-pointer ${
                activeTab === t.id ? "text-white font-semibold" : "text-zinc-500 hover:text-zinc-300"
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>

        <div key={`${env.env}:${activeTab}`} className="animate-fade-in">
        {activeTab === "timeline" ? (
          <TimelinePanel app={app} env={env.env} tz={tz} />
        ) : activeTab === "runs" ? (
          <RunsPanel app={app} env={env.env} tier={env.tier} tz={tz} />
        ) : activeTab === "deployments" ? (
          <DeploymentsPanel app={app} env={env.env} tz={tz} />
        ) : activeTab === "logs" ? (
          <div className="space-y-3">
            <div className="flex w-fit rounded-lg bg-[#121215] p-1 text-xs font-mono" role="group" aria-label="Log mode">
              {(["live", "history"] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  aria-pressed={logMode === m}
                  onClick={() => setLogMode(m)}
                  className={`rounded-md px-3 py-1 cursor-pointer select-none transition-colors ${
                    logMode === m ? "bg-[#202028] text-white" : "text-zinc-400 hover:text-zinc-200"
                  }`}
                >
                  {m === "live" ? "live tail" : "history"}
                </button>
              ))}
            </div>
            {logMode === "live" ? (
              <LogViewer
                app={app}
                env={env.env}
                tz={tz}
                disabledReason={s.blocked.logs || (s.can_logs ? undefined : "You do not have permission to view logs for this environment.")}
              />
            ) : s.can_logs ? (
              <LogHistory app={app} env={env.env} tz={tz} />
            ) : (
              <p className="rounded-md bg-amber-500/10 p-2.5 text-xs text-amber-300 font-mono">You do not have permission to view logs for this environment.</p>
            )}
          </div>
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
                        {e.detail || "-"}
                      </Td>
                    </Tr>
                  ))}
                </tbody>
              </Table>
            )}
          </div>
        )}
        </div>
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
