import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { UserRound } from "lucide-react";
import { api, ApiError, formatTime, useMe, type AccessRequest, type App } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

type Role = { id: number; name: string; permissions: string[] };

const select = "mt-1.5 h-9 w-full rounded-md bg-black px-3 text-xs text-zinc-100 font-mono focus:outline-none cursor-pointer";

const roleHelp: Record<string, string> = {
  viewer: "Sees apps and their deployments. Cannot change anything.",
  developer: "Views logs, restarts, runs migrations and seeds on non-production.",
  operator: "Like a developer, without seeds.",
  approver: "Approves production runs, and sees apps and deployments.",
  admin: "Everything, including access and agents.",
};

function Grant({ req, roles, apps, onDone }: { req: AccessRequest; roles: Role[]; apps: App[]; onDone: () => void }) {
  const qc = useQueryClient();
  const [roleId, setRoleId] = useState(roles.find((r) => r.name === "viewer")?.id ?? roles[0]?.id ?? 0);
  const [app, setApp] = useState("*");
  const [env, setEnv] = useState("*");
  const [err, setErr] = useState<string | null>(null);
  const role = roles.find((r) => r.id === roleId);
  const envNames = [...new Set((app === "*" ? apps : apps.filter((a) => a.id === app)).flatMap((a) => a.environments.map((e) => e.env)))].sort();

  const grant = useMutation({
    mutationFn: () => api("/api/admin/bindings", { method: "POST", json: { subject_type: "user", subject: req.login, role_id: roleId, app_id: app, env } }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["access-requests"] });
      void qc.invalidateQueries({ queryKey: ["bindings"] });
      onDone();
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : "that did not work"),
  });

  return (
    <form
      aria-label={`Grant access to ${req.login}`}
      className="mt-3 grid gap-3.5 rounded-lg bg-[#101014] p-4 sm:grid-cols-3"
      onSubmit={(e) => {
        e.preventDefault();
        setErr(null);
        grant.mutate();
      }}
    >
      <label className="block text-xs font-mono text-zinc-400">
        Role
        <select aria-label="Role" className={select} value={roleId} onChange={(e) => setRoleId(Number(e.target.value))}>
          {roles.map((r) => (
            <option key={r.id} value={r.id} className="bg-black text-white">
              {r.name}
            </option>
          ))}
        </select>
      </label>
      <label className="block text-xs font-mono text-zinc-400">
        Apps
        <select
          aria-label="Apps"
          className={select}
          value={app}
          onChange={(e) => {
            setApp(e.target.value);
            setEnv("*");
          }}
        >
          <option value="*" className="bg-black text-white">All apps</option>
          {apps.map((a) => (
            <option key={a.id} value={a.id} className="bg-black text-white">
              {a.name}
            </option>
          ))}
        </select>
      </label>
      <label className="block text-xs font-mono text-zinc-400">
        Environments
        <select aria-label="Environments" className={select} value={env} onChange={(e) => setEnv(e.target.value)}>
          <option value="*" className="bg-black text-white">All non-production</option>
          {envNames.map((n) => (
            <option key={n} value={n} className="bg-black text-white">
              {n} only
            </option>
          ))}
        </select>
      </label>

      {role && (
        <div className="sm:col-span-3">
          <p className="text-sm text-zinc-300">{roleHelp[role.name] ?? "A custom role."}</p>
          <div className="mt-2 flex flex-wrap gap-1.5" aria-label="Permissions of this role">
            {role.permissions.map((p) => (
              <span key={p} className="rounded bg-[#141418] px-2 py-0.5 font-mono text-xs text-zinc-300">
                {p}
              </span>
            ))}
          </div>
          <p className="mt-2 text-xs text-zinc-500">Production is never included in "All non-production": give production access with a separate grant for that environment.</p>
        </div>
      )}
      {err && (
        <p role="alert" className="rounded-lg bg-rose-500/10 p-2.5 text-sm text-rose-300 sm:col-span-3">
          {err}
        </p>
      )}
      <div className="flex gap-2.5 sm:col-span-3">
        <Button type="submit" disabled={grant.isPending || !roleId}>
          Grant access
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/** People who signed in and have no role yet. They stay here, even after they sign out or switch account, until an admin acts. */
export function AccessRequests() {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const qc = useQueryClient();
  const q = useQuery<AccessRequest[]>({ queryKey: ["access-requests"], queryFn: () => api("/api/admin/access-requests"), refetchInterval: 15_000 });
  const roles = useQuery<Role[]>({ queryKey: ["roles"], queryFn: () => api("/api/admin/roles") });
  const apps = useQuery<App[]>({ queryKey: ["apps"], queryFn: () => api("/api/apps") });
  const [open, setOpen] = useState<number | null>(null);
  const dismiss = useMutation({
    mutationFn: (id: number) => api(`/api/admin/access-requests/${id}/dismiss`, { method: "POST" }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["access-requests"] }),
  });
  const list = q.data ?? [];

  return (
    <Card className="p-0" role="region" aria-label="Awaiting access">
      <div className="flex items-center gap-3 px-5 pt-4 pb-2">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-400">Awaiting access</h2>
        {list.length > 0 && <Badge tone="warn">{list.length}</Badge>}
      </div>
      {list.length === 0 ? (
        <p className="px-5 pb-5 text-sm text-zinc-500">No one is waiting. People who sign in without a role appear here for you to grant access.</p>
      ) : (
        <ul className="divide-y divide-zinc-900/60">
          {list.map((r) => (
            <li key={r.id} className="px-5 py-4" aria-label={`Request from ${r.login}`}>
              <div className="flex flex-wrap items-center gap-3">
                {r.avatar_url ? <img src={r.avatar_url} alt="" className="size-9 rounded-full" /> : <UserRound className="size-9 rounded-full bg-zinc-800 p-2 text-zinc-400" strokeWidth={1.5} />}
                <div className="min-w-0">
                  <div className="text-sm font-medium text-white">{r.name || r.login}</div>
                  <div className="font-mono text-xs text-zinc-500">
                    @{r.login} · first signed in {formatTime(r.first_seen_at, tz)}
                  </div>
                </div>
                <div className="ml-auto flex gap-2">
                  <Button size="sm" onClick={() => setOpen(open === r.id ? null : r.id)} disabled={!roles.data}>
                    Grant access
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => dismiss.mutate(r.id)} disabled={dismiss.isPending} title="Hide this request until they sign in again">
                    Dismiss
                  </Button>
                </div>
              </div>
              {r.orgs.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {r.orgs.map((o) => (
                    <span key={o.org} className="rounded bg-[#141418] px-2 py-0.5 text-xs text-zinc-300" title={o.teams.length ? `Teams: ${o.teams.join(", ")}` : "No teams"}>
                      {o.org}
                      {o.teams.length > 0 && <span className="text-zinc-500"> · {o.teams.map((t) => t.split("/").pop()).join(", ")}</span>}
                    </span>
                  ))}
                </div>
              )}
              {open === r.id && roles.data && <Grant req={r} roles={roles.data} apps={apps.data ?? []} onDone={() => setOpen(null)} />}
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
