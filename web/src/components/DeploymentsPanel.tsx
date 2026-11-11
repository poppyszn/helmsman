import { useQuery } from "@tanstack/react-query";
import { ExternalLink, TriangleAlert } from "lucide-react";
import { api, formatTime, type DeploymentsResponse } from "@/lib/api";
import { RunStatusBadge } from "@/components/RunStatus";
import { Table, Td, Th, Tr } from "@/components/ui/table";

export function DeploymentsPanel({ app, env, tz }: { app: string; env: string; tz: string }) {
  const q = useQuery<DeploymentsResponse>({
    queryKey: ["deployments", app, env],
    queryFn: () => api(`/api/apps/${app}/envs/${env}/deployments`),
    refetchInterval: 30_000,
  });
  if (q.isLoading) return <div className="py-12 text-center text-xs text-zinc-500 font-mono">loading</div>;
  const d = q.data;
  if (!d) return <div className="py-12 text-center text-xs text-rose-300 font-mono">could not load deployments</div>;
  if (!d.configured) {
    return <div className="rounded-xl bg-[#0a0a0d] py-12 text-center text-xs text-zinc-500 font-mono">this app has no deploy workflow in the catalog</div>;
  }
  return (
    <div className="space-y-4">
      {d.error && (
        <div className="flex items-start gap-2 rounded-lg bg-amber-500/10 p-3 text-xs text-amber-200 font-mono">
          <TriangleAlert className="size-3.5 mt-0.5 shrink-0" strokeWidth={1.5} />
          <span>{d.error}</span>
        </div>
      )}
      {d.mismatch && (
        <div className="flex items-start gap-2 rounded-lg bg-amber-500/10 p-3 text-xs text-amber-200 font-mono">
          <TriangleAlert className="size-3.5 mt-0.5 shrink-0" strokeWidth={1.5} />
          <span>
            Running revision <b>{d.running_revision.slice(0, 7)}</b> differs from the latest successful deploy <b>{d.latest_success_commit.slice(0, 7)}</b>.
          </span>
        </div>
      )}
      {d.deployments.length === 0 ? (
        <div className="rounded-xl bg-[#0a0a0d] py-12 text-center text-xs text-zinc-500 font-mono">no deploy runs yet</div>
      ) : (
        <Table>
          <thead>
            <Tr>
              <Th>Status</Th>
              <Th>Commit</Th>
              <Th>Actor</Th>
              <Th>Time</Th>
              <Th>
                <span className="sr-only">Link</span>
              </Th>
            </Tr>
          </thead>
          <tbody>
            {d.deployments.map((x) => (
              <Tr key={x.run_id}>
                <Td>
                  <RunStatusBadge status={x.status} />
                </Td>
                <Td className="font-mono text-xs text-zinc-300">
                  {x.commit.slice(0, 7)}
                  {x.matches_running && <span className="ml-2 rounded bg-emerald-500/15 px-1.5 py-0.5 text-xs text-emerald-300">running</span>}
                </Td>
                <Td className="font-mono text-xs text-zinc-300">{x.actor}</Td>
                <Td className="font-mono text-xs text-zinc-400">{formatTime(x.at, tz)}</Td>
                <Td>
                  <a href={x.url} target="_blank" rel="noreferrer" className="text-zinc-500 hover:text-white" title="Open on GitHub">
                    <ExternalLink className="size-3.5" strokeWidth={1.5} />
                  </a>
                </Td>
              </Tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}
