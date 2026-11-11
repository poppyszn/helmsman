import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, RefreshCw, XCircle } from "lucide-react";
import { api, formatTime, useMe, type RepoCheck } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Table, Td, Th, Tr } from "@/components/ui/table";

type Resp = { mode: string; api_url: string; repos: RepoCheck[] };

function Mark({ ok }: { ok: boolean }) {
  return ok ? (
    <CheckCircle2 className="size-4 text-emerald-400" strokeWidth={2} aria-label="yes" />
  ) : (
    <XCircle className="size-4 text-rose-400" strokeWidth={2} aria-label="no" />
  );
}

export default function GitHub() {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const qc = useQueryClient();
  const q = useQuery<Resp>({ queryKey: ["github"], queryFn: () => api("/api/admin/github"), refetchInterval: 30_000 });
  const check = useMutation({
    mutationFn: () => api<Resp>("/api/admin/github/check", { method: "POST", json: {} }),
    onSuccess: (d) => qc.setQueryData(["github"], d),
  });
  const repos = q.data?.repos ?? [];
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">GitHub</h1>
          <p className="mt-1 text-sm text-zinc-400">
            The GitHub App must be installed on each repository, and its migrate workflow needs a workflow_dispatch trigger. A repository that fails these checks has its actions disabled.
            {q.data?.api_url ? ` Runs are sent to ${q.data.api_url}.` : ""}
          </p>
        </div>
        <Button variant="secondary" onClick={() => check.mutate()} disabled={check.isPending}>
          <RefreshCw className={`size-3.5 ${check.isPending ? "animate-spin" : ""}`} strokeWidth={1.5} />
          Check now
        </Button>
      </div>
      {repos.length === 0 ? (
        <div className="rounded-xl bg-[#0a0a0d] py-16 text-center text-sm text-zinc-500">No repositories have been checked yet.</div>
      ) : (
        <Table>
          <thead>
            <Tr>
              <Th>Repository</Th>
              <Th>App installed</Th>
              <Th>Workflows</Th>
              <Th>Actions</Th>
              <Th>Checked</Th>
            </Tr>
          </thead>
          <tbody>
            {repos.map((r) => (
              <Tr key={r.repo}>
                <Td className="font-mono text-xs text-zinc-200">{r.repo}</Td>
                <Td>
                  <Mark ok={r.installed} />
                </Td>
                <Td>
                  <ul className="space-y-1">
                    {(r.workflows ?? []).map((w) => (
                      <li key={w.file} className="flex items-center gap-2 font-mono text-xs text-zinc-300">
                        <Mark ok={w.exists && (w.role !== "migrate" || w.has_dispatch)} />
                        {w.file}
                        <span className="text-zinc-500">{w.role}</span>
                      </li>
                    ))}
                  </ul>
                </Td>
                <Td className="text-xs font-mono">
                  {r.problem ? <span className="text-rose-300">disabled: {r.problem}</span> : <span className="text-emerald-300">enabled</span>}
                </Td>
                <Td className="font-mono text-xs text-zinc-400">{formatTime(r.checked_at, tz)}</Td>
              </Tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}
