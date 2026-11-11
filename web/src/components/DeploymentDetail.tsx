import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, ExternalLink, FileText } from "lucide-react";
import { api, type RunJob } from "@/lib/api";
import { durationLabel, parseJobLog } from "@/lib/ghlog";
import { StepIcon } from "@/components/RunStatus";
import { cn } from "@/lib/utils";

type Props = { app: string; env: string; runId: number };

/** The jobs and steps of one deploy run, with each job's log on demand. */
export function DeploymentDetail({ app, env, runId }: Props) {
  const base = `/api/apps/${app}/envs/${env}/deployments/${runId}`;
  const jobs = useQuery<{ jobs: RunJob[] }>({
    queryKey: ["deployment-jobs", app, env, runId],
    queryFn: () => api(`${base}/jobs`),
    // Keep watching a deploy that is still going.
    refetchInterval: (q) => ((q.state.data?.jobs ?? []).some((j) => j.status !== "completed") ? 5_000 : false),
  });
  if (jobs.isLoading) return <div className="px-4 py-4 text-xs font-mono text-zinc-500">loading steps</div>;
  if (jobs.isError || !jobs.data) {
    return <div className="px-4 py-4 text-xs font-mono text-rose-300">could not load the steps of this deployment</div>;
  }
  if (jobs.data.jobs.length === 0) {
    return <div className="px-4 py-4 text-xs font-mono text-zinc-500">GitHub has not reported any jobs for this run yet</div>;
  }
  return (
    <div className="space-y-3 px-4 py-4">
      {jobs.data.jobs.map((j) => (
        <JobCard key={j.id} base={base} job={j} />
      ))}
    </div>
  );
}

function JobCard({ base, job }: { base: string; job: RunJob }) {
  const failed = job.status === "completed" && !["success", "skipped", "neutral", "cancelled"].includes(job.conclusion ?? "");
  const [showLog, setShowLog] = useState(false);
  const log = useQuery<{ content: string; truncated: boolean }>({
    queryKey: ["deployment-log", base, job.id],
    queryFn: () => api(`${base}/jobs/${job.id}/logs`),
    enabled: showLog,
    staleTime: job.status === "completed" ? Infinity : 0,
    retry: false,
  });
  return (
    <div className="rounded-lg bg-[#060608] p-3">
      <div className="flex flex-wrap items-center gap-2 text-sm font-mono font-semibold text-zinc-100">
        <StepIcon status={job.status} conclusion={job.conclusion} />
        <span>{job.name}</span>
        <span className="text-xs font-normal text-zinc-500">{durationLabel(job.started_at, job.completed_at, job.status === "in_progress")}</span>
        <button
          type="button"
          onClick={() => setShowLog((v) => !v)}
          className="ml-auto inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium text-sky-400 hover:bg-sky-400/10 hover:text-sky-300"
        >
          <FileText className="size-3.5" strokeWidth={1.5} />
          {showLog ? "Hide log" : failed ? "Show log (failed)" : "Show log"}
        </button>
        <a href={job.html_url} target="_blank" rel="noreferrer" className="text-zinc-500 hover:text-white" title="Open the job on GitHub">
          <ExternalLink className="size-3.5" strokeWidth={1.5} />
        </a>
      </div>
      <ol className="mt-3 space-y-1.5">
        {(job.steps ?? []).map((s) => (
          <li key={s.number} className={cn("flex items-center gap-2 text-xs font-mono", s.conclusion === "failure" ? "text-rose-300" : "text-zinc-300")}>
            <StepIcon status={s.status} conclusion={s.conclusion} />
            <span className="w-5 text-right text-zinc-600">{s.number}</span>
            <span>{s.name}</span>
            <span className="ml-auto text-zinc-500">{durationLabel(s.started_at, s.completed_at, s.status === "in_progress")}</span>
          </li>
        ))}
      </ol>
      {showLog && (
        <div className="mt-3">
          {log.isLoading ? (
            <div className="text-xs font-mono text-zinc-500">loading log</div>
          ) : log.isError || !log.data ? (
            <div className="text-xs font-mono text-rose-300">GitHub did not return this log. It may have expired, or the job has not started.</div>
          ) : (
            <JobLog text={log.data.content} truncated={log.data.truncated} />
          )}
        </div>
      )}
    </div>
  );
}

function JobLog({ text, truncated }: { text: string; truncated: boolean }) {
  const sections = parseJobLog(text);
  if (sections.length === 0) return <div className="text-xs font-mono text-zinc-500">the log is empty</div>;
  return (
    <div className="max-h-[32rem] overflow-auto rounded-lg bg-black p-3 font-mono text-xs leading-relaxed">
      {truncated && <div className="mb-2 text-amber-300">(earlier output was cut; showing the end of the log)</div>}
      {sections.map((sec, i) =>
        sec.title === null ? (
          <Lines key={i} lines={sec.lines} />
        ) : (
          <details key={i} open={sec.hasError} className="group my-1">
            <summary className={cn("flex cursor-pointer list-none items-center gap-1.5 py-0.5 select-none", sec.hasError ? "text-rose-300" : "text-zinc-200 hover:text-white")}>
              <ChevronRight className="size-3.5 group-open:hidden" strokeWidth={1.5} />
              <ChevronDown className="hidden size-3.5 group-open:block" strokeWidth={1.5} />
              {sec.title}
            </summary>
            <div className="ml-5">
              <Lines lines={sec.lines} />
            </div>
          </details>
        ),
      )}
    </div>
  );
}

function Lines({ lines }: { lines: { text: string; kind: "plain" | "error" | "warning" }[] }) {
  return (
    <>
      {lines.map((l, i) => (
        <div
          key={i}
          className={cn("whitespace-pre-wrap break-all", l.kind === "error" ? "text-rose-300" : l.kind === "warning" ? "text-amber-300" : "text-zinc-400")}
        >
          {l.text || " "}
        </div>
      ))}
    </>
  );
}
