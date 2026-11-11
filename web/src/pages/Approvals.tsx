import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Check, X } from "lucide-react";
import { api, ApiError, formatTime, useMe, type Run } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { RunStatusBadge } from "@/components/RunStatus";
import { useState } from "react";

export default function Approvals() {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const qc = useQueryClient();
  const [err, setErr] = useState<string | null>(null);
  const q = useQuery<{ runs: Run[] }>({ queryKey: ["approvals"], queryFn: () => api("/api/approvals"), refetchInterval: 10_000 });
  const decide = useMutation({
    mutationFn: ({ r, verb }: { r: Run; verb: "approve" | "reject" }) =>
      api(`/api/apps/${r.app}/envs/${r.env}/runs/${r.id}/${verb}`, { method: "POST", json: {} }),
    onError: (e) => setErr(e instanceof ApiError ? e.message : "request failed"),
    onSuccess: () => setErr(null),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ["approvals"] });
      qc.invalidateQueries({ queryKey: ["runs"] });
    },
  });
  const runs = q.data?.runs ?? [];
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-white">Approvals</h1>
        <p className="mt-1 text-sm text-zinc-400">Production migrations wait here until a second person accepts them. You cannot approve your own request.</p>
      </div>
      {err && <p role="alert" className="rounded-lg bg-rose-500/10 p-3 text-xs text-rose-300 font-mono">{err}</p>}
      {runs.length === 0 ? (
        <div className="rounded-xl bg-[#0a0a0d] py-16 text-center text-sm text-zinc-500">Nothing is waiting for approval.</div>
      ) : (
        <div className="space-y-2">
          {runs.map((r) => (
            <div key={r.id} className="flex flex-wrap items-center gap-x-5 gap-y-2 rounded-xl bg-[#0a0a0d] px-4 py-3.5">
              <Link to={`/apps/${r.app}`} className="font-mono text-sm font-semibold text-sky-400 hover:text-sky-300 hover:underline">
                {r.app}/{r.env}
              </Link>
              <span className="font-mono text-sm text-zinc-200">{r.action}</span>
              <RunStatusBadge status={r.status} />
              <span className="text-xs font-mono text-zinc-400">
                requested by {r.requested_by} · {formatTime(r.created_at, tz)}
              </span>
              <div className="ml-auto flex items-center gap-2">
                {r.can_approve && (
                  <Button size="sm" onClick={() => decide.mutate({ r, verb: "approve" })} disabled={decide.isPending}>
                    <Check className="size-3.5" strokeWidth={2} />
                    Approve
                  </Button>
                )}
                <Button size="sm" variant="ghost" onClick={() => decide.mutate({ r, verb: "reject" })} disabled={decide.isPending}>
                  <X className="size-3.5" strokeWidth={2} />
                  {r.can_approve ? "Reject" : "Withdraw"}
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
