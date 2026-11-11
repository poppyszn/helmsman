import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Database, ExternalLink, Play, RotateCw, Sprout, Square, TriangleAlert } from "lucide-react";
import { api, ApiError, formatTime, runActive, type Run, type RunsResponse } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { RunStatusBadge, StepIcon } from "@/components/RunStatus";

type Command = "migrate" | "seed";

export function RunsPanel({ app, env, tier, tz }: { app: string; env: string; tier: "nonprod" | "prod"; tz: string }) {
  const qc = useQueryClient();
  const base = `/api/apps/${app}/envs/${env}`;
  const key = ["runs", app, env];
  const [confirm, setConfirm] = useState<Command | null>(null);
  const [error, setError] = useState<string | null>(null);

  const q = useQuery<RunsResponse>({
    queryKey: key,
    queryFn: () => api(`${base}/runs`),
    refetchInterval: (query) => (query.state.data?.runs.some((r) => runActive(r.status)) ? 3000 : 15000),
  });

  const start = useMutation({
    mutationFn: (c: Command) => api<{ run: Run; deduped: boolean }>(`${base}/actions/${c}`, { method: "POST", json: {} }),
    onSuccess: () => {
      setConfirm(null);
      setError(null);
    },
    onError: (e) => setError(e instanceof ApiError ? e.message : "request failed"),
    onSettled: () => qc.invalidateQueries({ queryKey: key }),
  });

  if (q.isLoading) return <div className="py-12 text-center text-xs text-zinc-500 font-mono">loading</div>;
  if (!q.data) return <div className="py-12 text-center text-xs text-rose-300 font-mono">could not load runs</div>;
  const d = q.data;
  const blockedSeed = !d.seed_allowed ? `Seeding is not allowed in ${env}` : undefined;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2.5">
        <Button variant="secondary" disabled={!d.can_migrate} onClick={() => setConfirm("migrate")} title={d.disabled_reason || (d.can_migrate ? "Run the migrate workflow" : "You need action.migrate on this environment")}>
          <Database className="size-3.5" strokeWidth={1.5} />
          Run migrations
        </Button>
        <Button variant="secondary" disabled={!d.can_seed} onClick={() => setConfirm("seed")} title={blockedSeed || d.disabled_reason || (d.can_seed ? "Run the seed" : "You need action.seed on this environment")}>
          <Sprout className="size-3.5" strokeWidth={1.5} />
          Run seed
        </Button>
        <a href={`https://github.com/${d.repo}/actions`} target="_blank" rel="noreferrer" className="ml-auto inline-flex items-center gap-1.5 text-xs font-mono text-zinc-400 hover:text-white">
          {d.repo}
          <ExternalLink className="size-3.5" strokeWidth={1.5} />
        </a>
      </div>

      {d.disabled_reason && (
        <div className="flex items-start gap-2 rounded-lg bg-amber-500/10 p-3 text-xs text-amber-200 font-mono">
          <TriangleAlert className="size-3.5 mt-0.5 shrink-0" strokeWidth={1.5} />
          <span>Actions are disabled for this repository: {d.disabled_reason}</span>
        </div>
      )}
      {d.needs_approval && (
        <p className="text-xs font-mono text-zinc-400">Runs in {env} start only after a second person with approval rights accepts them.</p>
      )}

      {d.runs.length === 0 ? (
        <div className="rounded-xl bg-[#0a0a0d] py-12 text-center text-xs text-zinc-500 font-mono">no runs yet</div>
      ) : (
        <div className="space-y-2">
          {d.runs.map((r) => (
            <RunRow key={r.id} run={r} base={base} tz={tz} onChanged={() => qc.invalidateQueries({ queryKey: key })} />
          ))}
        </div>
      )}

      <ConfirmDialog
        open={confirm !== null}
        title={confirm === "seed" ? `Seed ${env}?` : `Run migrations on ${env}?`}
        body={
          d.needs_approval
            ? "This creates a request. Nothing runs until a second person approves it."
            : `This starts the ${confirm ?? "migrate"} workflow on ${d.repo}. Only one run can be active in an environment at a time.`
        }
        confirmLabel={d.needs_approval ? "Request approval" : "Start run"}
        requireText={tier === "prod" ? env : undefined}
        busy={start.isPending}
        error={error}
        onConfirm={() => confirm && start.mutate(confirm)}
        onCancel={() => {
          setConfirm(null);
          setError(null);
        }}
      />
    </div>
  );
}

function RunRow({ run, base, tz, onChanged }: { run: Run; base: string; tz: string; onChanged: () => void }) {
  const [open, setOpen] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const op = useMutation({
    mutationFn: (verb: "approve" | "reject" | "cancel" | "rerun") => api(`${base}/runs/${run.id}/${verb}`, { method: "POST", json: {} }),
    onError: (e) => setErr(e instanceof ApiError ? e.message : "request failed"),
    onSuccess: () => setErr(null),
    onSettled: onChanged,
  });
  const finished = !runActive(run.status) && run.status !== "pending_approval";
  const logs = useQuery<{ logs: { job_id: number; job_name: string; content: string; truncated: boolean }[] }>({
    queryKey: ["run-logs", run.id],
    queryFn: () => api(`${base}/runs/${run.id}/logs`),
    enabled: open && finished && !!run.gh_run_url,
  });

  return (
    <div className="rounded-xl bg-[#0a0a0d]">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
        <button onClick={() => setOpen(!open)} className="flex items-center gap-3 text-left cursor-pointer min-w-0" aria-expanded={open}>
          {open ? <ChevronDown className="size-4 text-zinc-500" strokeWidth={1.5} /> : <ChevronRight className="size-4 text-zinc-500" strokeWidth={1.5} />}
          <span className="font-mono text-sm font-semibold text-white">{run.action}</span>
          <RunStatusBadge status={run.status} />
        </button>
        <span className="text-xs font-mono text-zinc-400">
          {run.requested_by}
          {run.approved_by ? ` approved by ${run.approved_by}` : ""} · {formatTime(run.created_at, tz)}
        </span>
        {run.ref && <span className="rounded bg-[#141418] px-2 py-0.5 text-xs font-mono text-zinc-300">{run.ref}</span>}
        <div className="ml-auto flex items-center gap-2">
          {run.can_approve && (
            <Button size="sm" onClick={() => op.mutate("approve")} disabled={op.isPending}>
              <Play className="size-3.5" strokeWidth={2} />
              Approve
            </Button>
          )}
          {run.can_reject && (
            <Button size="sm" variant="ghost" onClick={() => op.mutate("reject")} disabled={op.isPending}>
              {run.can_approve ? "Reject" : "Withdraw"}
            </Button>
          )}
          {run.can_cancel && (
            <Button size="sm" variant="destructive" onClick={() => op.mutate("cancel")} disabled={op.isPending}>
              <Square className="size-3.5" strokeWidth={2} />
              Cancel
            </Button>
          )}
          {run.can_rerun && (
            <Button size="sm" variant="outline" onClick={() => op.mutate("rerun")} disabled={op.isPending}>
              <RotateCw className="size-3.5" strokeWidth={1.5} />
              Rerun
            </Button>
          )}
        </div>
      </div>

      {err && <p role="alert" className="mx-4 mb-3 rounded-lg bg-rose-500/10 p-2.5 text-xs text-rose-300 font-mono">{err}</p>}

      {open && (
        <div className="space-y-3 px-4 pb-4">
          {run.warning && (
            <div className="flex items-start gap-2 rounded-lg bg-amber-500/10 p-3 text-xs text-amber-200 font-mono">
              <TriangleAlert className="size-3.5 mt-0.5 shrink-0" strokeWidth={1.5} />
              <span>{run.warning}</span>
            </div>
          )}
          {run.error && <p className="rounded-lg bg-rose-500/10 p-3 text-xs text-rose-300 font-mono">{run.error}</p>}
          {run.status === "unknown" && (
            <a href={run.workflow_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 text-xs font-mono text-indigo-300 hover:text-white">
              Open the workflow on GitHub
              <ExternalLink className="size-3.5" strokeWidth={1.5} />
            </a>
          )}
          {(run.jobs ?? []).map((j) => (
            <div key={j.id} className="rounded-lg bg-[#060608] p-3">
              <div className="flex items-center gap-2 text-xs font-mono font-semibold text-zinc-200">
                <StepIcon status={j.status} conclusion={j.conclusion} />
                {j.name}
                <a href={j.html_url} target="_blank" rel="noreferrer" className="ml-auto text-zinc-500 hover:text-white" title="Open the job on GitHub">
                  <ExternalLink className="size-3.5" strokeWidth={1.5} />
                </a>
              </div>
              <ul className="mt-2 space-y-1.5">
                {(j.steps ?? []).map((s) => (
                  <li key={s.number} className="flex items-center gap-2 text-xs font-mono text-zinc-400">
                    <StepIcon status={s.status} conclusion={s.conclusion} />
                    {s.name}
                  </li>
                ))}
              </ul>
            </div>
          ))}
          {finished && run.gh_run_url && (
            <div>
              <div className="mb-1.5 text-xs font-mono font-medium uppercase tracking-wider text-zinc-400">Log</div>
              {logs.isLoading ? (
                <div className="text-xs font-mono text-zinc-500">loading</div>
              ) : (logs.data?.logs ?? []).length === 0 ? (
                <div className="text-xs font-mono text-zinc-500">no log was captured</div>
              ) : (
                logs.data!.logs.map((l) => (
                  <pre key={l.job_id} className="max-h-80 overflow-auto rounded-lg bg-[#060608] p-3 text-xs font-mono leading-relaxed text-zinc-300 whitespace-pre-wrap break-all">
                    {l.truncated ? "(earlier output was cut)\n" : ""}
                    {l.content}
                  </pre>
                ))
              )}
            </div>
          )}
          {run.gh_run_url && (
            <a href={run.gh_run_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 text-xs font-mono text-zinc-400 hover:text-white">
              View run on GitHub
              <ExternalLink className="size-3.5" strokeWidth={1.5} />
            </a>
          )}
        </div>
      )}
    </div>
  );
}
