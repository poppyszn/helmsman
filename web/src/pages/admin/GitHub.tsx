import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, ExternalLink, RefreshCw, XCircle } from "lucide-react";
import { api, formatTime, useMe, type RepoCheck } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Table, Td, Th, Tr } from "@/components/ui/table";

type OrgInfo = { org: string; allowed: boolean; members: string[]; apps: number; policy_url?: string };
type Resp = { mode: string; api_url: string; repos: RepoCheck[]; orgs: OrgInfo[]; authorize_url?: string };

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
  const orgs = q.data?.orgs ?? [];
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
      <section aria-label="Organizations" className="space-y-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold text-white">Organizations</h2>
            <p className="mt-1 text-sm text-zinc-400">
              People only see apps from organizations they belong to. An organization is connected once someone from it has signed in; until then, an owner or member has to grant the OAuth App access to it.
            </p>
          </div>
          {q.data?.authorize_url && (
            <a href={q.data.authorize_url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1.5 text-sm text-zinc-300 hover:text-white">
              Grant access on GitHub
              <ExternalLink className="size-3.5" strokeWidth={1.5} />
            </a>
          )}
        </div>
        <ul className="grid gap-2 sm:grid-cols-2">
          {orgs.map((o) => (
            <li key={o.org} aria-label={`Organization ${o.org}`} className="rounded-xl bg-[#0a0a0d] px-4 py-3">
              <div className="flex items-center justify-between gap-3">
                <span className="font-mono text-sm text-zinc-100">{o.org}</span>
                <span className="flex items-center gap-1.5 text-sm text-zinc-400">
                  <Mark ok={o.allowed && o.members.length > 0} />
                  {!o.allowed ? "not allowed to sign in" : o.members.length > 0 ? "connected" : "no one has signed in yet"}
                </span>
              </div>
              <div className="mt-1 text-sm text-zinc-400">
                {o.members.length} {o.members.length === 1 ? "person" : "people"} signed in, {o.apps} {o.apps === 1 ? "app" : "apps"}
                {o.members.length > 0 && <span className="text-zinc-500">: {o.members.join(", ")}</span>}
              </div>
              {o.policy_url && o.allowed && o.members.length === 0 && (
                <a href={o.policy_url} target="_blank" rel="noopener noreferrer" className="mt-1.5 inline-flex items-center gap-1.5 text-sm text-zinc-300 hover:text-white">
                  Approve the OAuth App for {o.org}
                  <ExternalLink className="size-3.5" strokeWidth={1.5} />
                </a>
              )}
            </li>
          ))}
        </ul>
      </section>
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
