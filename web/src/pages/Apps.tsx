import * as React from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Pencil, Plus, Search, Trash2, Wrench } from "lucide-react";
import { api, ApiError, useMe, type App, type CustomApp } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/StatusBadge";
import { Input } from "@/components/ui/input";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { cn } from "@/lib/utils";

const ORG_KEY = "helmsman.org";
const remembered = () => {
  try {
    return localStorage.getItem(ORG_KEY) ?? "";
  } catch {
    return "";
  }
};

function Chip({ on, onClick, children, title }: { on: boolean; onClick: () => void; children: React.ReactNode; title?: string }) {
  return (
    <button
      type="button"
      aria-pressed={on}
      title={title}
      onClick={onClick}
      className={cn("rounded-md px-2.5 py-1.5 text-xs font-mono transition-colors cursor-pointer select-none", on ? "bg-[#202028] text-white" : "bg-[#141418] text-zinc-400 hover:text-zinc-200")}
    >
      {children}
    </button>
  );
}

export default function Apps() {
  const { data: me } = useMe();
  const [params, setParams] = useSearchParams();
  const [filter, setFilter] = React.useState("");
  const tierFilter = (params.get("tier") ?? "all") as "all" | "prod" | "nonprod";
  const tags = params.getAll("tag");

  const { data = [], isPending, error } = useQuery<App[]>({
    queryKey: ["apps"],
    queryFn: () => api("/api/apps"),
    refetchInterval: 15_000,
  });
  const tools = useQuery<CustomApp[]>({ queryKey: ["custom-apps"], queryFn: () => api("/api/custom-apps") });

  const orgs = React.useMemo(() => [...new Set(data.map((a) => a.org))].sort(), [data]);
  // The organization comes from the address, else from the last one chosen on this browser, when it still exists.
  const orgParam = params.get("org");
  const org = orgParam ?? (orgs.length > 1 && orgs.includes(remembered()) ? remembered() : "");

  const update = (mut: (p: URLSearchParams) => void) => {
    const next = new URLSearchParams(params);
    mut(next);
    setParams(next, { replace: true });
  };
  const chooseOrg = (o: string) => {
    try {
      localStorage.setItem(ORG_KEY, o);
    } catch {
      /* a private window: the choice just is not remembered */
    }
    update((p) => (o ? p.set("org", o) : p.set("org", "")));
  };
  const toggleTag = (t: string) =>
    update((p) => {
      const cur = p.getAll("tag");
      p.delete("tag");
      (cur.includes(t) ? cur.filter((x) => x !== t) : [...cur, t]).forEach((x) => p.append("tag", x));
    });

  if (isPending) {
    return <div className="py-20 text-xs text-zinc-500 font-mono">loading apps...</div>;
  }

  if (error) {
    return (
      <Card className="bg-[#0a0a0d]">
        <h3 className="text-sm font-semibold text-amber-400">Access Restricted</h3>
        <p className="text-xs text-zinc-400 mt-1">You do not have permission to view any applications in the catalog.</p>
      </Card>
    );
  }

  // An app has a tag when it carries it or has an environment of that name (so prod and develop work as filters).
  const hasTag = (app: App, t: string) => app.tags.includes(t) || app.environments.some((e) => e.env === t);
  const inOrg = data.filter((a) => !org || a.org === org);
  const q = filter.toLowerCase();
  const filteredApps = inOrg.filter((app) => {
    const matchesQuery = app.name.toLowerCase().includes(q) || app.repo.toLowerCase().includes(q) || app.environments.some((e) => e.env.toLowerCase().includes(q));
    if (!matchesQuery) return false;
    if (tierFilter !== "all" && !app.environments.some((e) => e.tier === tierFilter)) return false;
    return tags.every((t) => hasTag(app, t));
  });

  // The tags to offer: the apps' own tags first, then environment names, each with how many apps carry it.
  const counts = new Map<string, number>();
  for (const a of inOrg) for (const t of new Set([...a.tags, ...a.environments.map((e) => e.env)])) counts.set(t, (counts.get(t) ?? 0) + 1);
  const ownTags = new Set(inOrg.flatMap((a) => a.tags));
  const tagList = [...counts.keys()].sort((a, b) => Number(ownTags.has(b)) - Number(ownTags.has(a)) || a.localeCompare(b));

  const shownTools = (tools.data ?? []).filter((t) => tags.every((x) => t.tags.includes(x)));

  return (
    <div className="space-y-6">
      {/* Header & Filter Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <h1 className="text-2xl font-bold text-white tracking-tight">Applications</h1>

        <div className="flex flex-wrap items-center gap-3">
          {orgs.length > 1 && (
            <label className="flex items-center gap-2 text-xs font-mono text-zinc-400">
              Organization
              <select
                aria-label="Organization"
                value={org}
                onChange={(e) => chooseOrg(e.target.value)}
                className="h-9 rounded-md bg-[#121215] px-3 text-xs text-zinc-100 font-mono focus:outline-none cursor-pointer"
              >
                <option value="" className="bg-black text-white">All organizations</option>
                {orgs.map((o) => (
                  <option key={o} value={o} className="bg-black text-white">
                    {o}
                  </option>
                ))}
              </select>
            </label>
          )}
          <div className="relative w-60">
            <Search className="absolute left-3 top-2.5 size-3.5 text-zinc-500" strokeWidth={1.5} />
            <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Search apps or repos..." className="pl-8 h-9 text-xs bg-[#121215]" />
          </div>

          <div className="flex rounded-lg bg-[#121215] p-1 text-xs font-mono">
            {(["all", "prod", "nonprod"] as const).map((tier) => (
              <button
                key={tier}
                onClick={() => update((p) => (tier === "all" ? p.delete("tier") : p.set("tier", tier)))}
                className={`rounded-md px-3 py-1 text-xs capitalize transition-colors cursor-pointer select-none font-medium ${
                  tierFilter === tier ? "bg-[#202028] text-white shadow-sm" : "text-zinc-400 hover:text-zinc-200"
                }`}
              >
                {tier}
              </button>
            ))}
          </div>
        </div>
      </div>

      {tagList.length > 0 && (
        <div role="group" aria-label="Filter by tag" className="flex flex-wrap items-center gap-1.5">
          <span className="mr-1 text-xs font-mono text-zinc-500">tags</span>
          {tagList.map((t) => (
            <Chip key={t} on={tags.includes(t)} onClick={() => toggleTag(t)} title={ownTags.has(t) ? "A tag from the catalog" : "An environment name"}>
              {t} <span className="text-zinc-500">{counts.get(t)}</span>
            </Chip>
          ))}
          {tags.length > 0 && (
            <button type="button" onClick={() => update((p) => p.delete("tag"))} className="ml-1 text-xs text-sky-400 hover:text-sky-300 cursor-pointer">
              clear
            </button>
          )}
        </div>
      )}

      {filteredApps.length === 0 ? (
        <div className="py-16 text-center text-sm text-zinc-500 font-mono">{filter || tags.length || org ? "no matching apps" : "no apps declared in catalog"}</div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          {filteredApps.map((app) => (
            <Link key={app.id} to={`/apps/${app.id}`} className="block focus:outline-none group">
              <Card className="hover:bg-[#111116] transition-all duration-150 p-5">
                <div className="flex items-baseline justify-between gap-3">
                  <h2 className="text-base font-semibold text-white tracking-tight group-hover:text-zinc-100">{app.name}</h2>
                  <span className="text-xs text-zinc-400 font-mono">{app.repo}</span>
                </div>
                {app.tags.length > 0 && (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {app.tags.map((t) => (
                      <span key={t} className="rounded bg-[#141418] px-1.5 py-0.5 text-xs font-mono text-zinc-400">
                        {t}
                      </span>
                    ))}
                  </div>
                )}

                <div className="mt-4 flex flex-wrap gap-2.5">
                  {app.environments.map((e) => (
                    <div key={e.env} className="flex items-center gap-2 rounded-lg bg-[#141418] px-2.5 py-1.5 text-xs transition-colors hover:bg-[#1a1a22]">
                      <span className="font-mono text-xs font-medium text-zinc-200">{e.env}</span>
                      <Badge tone={e.tier === "prod" ? "prod" : "default"}>{e.tier}</Badge>
                      <StatusBadge status={e.status} size="sm" showLabel={false} />
                    </div>
                  ))}
                </div>
              </Card>
            </Link>
          ))}
        </div>
      )}

      <Tools tools={shownTools} total={tools.data?.length ?? 0} admin={!!me?.can.admin} />
    </div>
  );
}

type ToolDraft = { id?: number; name: string; url: string; description: string; tags: string };

/** External tools kept in one place: Grafana, Sentry, the wiki. Links only; an admin manages them. */
function Tools({ tools, total, admin }: { tools: CustomApp[]; total: number; admin: boolean }) {
  const qc = useQueryClient();
  const [form, setForm] = React.useState<ToolDraft | null>(null);
  const [err, setErr] = React.useState<string | null>(null);
  const [del, setDel] = React.useState<CustomApp | null>(null);

  const save = useMutation({
    mutationFn: (d: ToolDraft) => {
      const body = { name: d.name, url: d.url, description: d.description, tags: d.tags.split(/[\s,]+/).filter(Boolean) };
      return d.id === undefined ? api("/api/admin/custom-apps", { method: "POST", json: body }) : api(`/api/admin/custom-apps/${d.id}`, { method: "PUT", json: body });
    },
    onSuccess: () => {
      setForm(null);
      setErr(null);
      void qc.invalidateQueries({ queryKey: ["custom-apps"] });
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : "that did not work"),
  });
  const remove = useMutation({
    mutationFn: (id: number) => api(`/api/admin/custom-apps/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      setDel(null);
      void qc.invalidateQueries({ queryKey: ["custom-apps"] });
    },
  });

  if (total === 0 && !admin) return null;
  return (
    <section aria-label="Tools" className="space-y-3 pt-2">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <Wrench className="size-4 text-zinc-500" strokeWidth={1.5} />
          <h2 className="text-sm font-semibold uppercase tracking-wider text-zinc-400">Tools</h2>
          <span className="text-xs text-zinc-500">links to the other tools your team uses</span>
        </div>
        {admin && (
          <Button size="sm" variant="outline" onClick={() => { setErr(null); setForm({ name: "", url: "", description: "", tags: "" }); }}>
            <Plus className="size-3.5" strokeWidth={1.5} />
            Add tool
          </Button>
        )}
      </div>

      {form && (
        <form
          aria-label={form.id === undefined ? "New tool" : "Edit tool"}
          className="grid gap-3.5 rounded-xl bg-[#0a0a0d] p-5 sm:grid-cols-2"
          onSubmit={(e) => {
            e.preventDefault();
            setErr(null);
            save.mutate(form);
          }}
        >
          <label className="block text-xs font-mono text-zinc-400">
            Name
            <Input className="mt-1.5 bg-black" placeholder="Grafana" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </label>
          <label className="block text-xs font-mono text-zinc-400">
            Address
            <Input className="mt-1.5 bg-black" placeholder="https://grafana.example.com" value={form.url} onChange={(e) => setForm({ ...form, url: e.target.value })} />
          </label>
          <label className="block text-xs font-mono text-zinc-400 sm:col-span-2">
            Description <span className="text-zinc-600">(optional)</span>
            <Input className="mt-1.5 bg-black" placeholder="Dashboards and alerts" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </label>
          <label className="block text-xs font-mono text-zinc-400 sm:col-span-2">
            Tags <span className="text-zinc-600">(optional, separated by commas: they appear in the tag filter)</span>
            <Input className="mt-1.5 bg-black" placeholder="monitoring, prod" value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} />
          </label>
          {err && (
            <p role="alert" className="rounded-lg bg-rose-500/10 p-3 text-sm text-rose-300 sm:col-span-2">
              {err}
            </p>
          )}
          <div className="flex gap-2.5 sm:col-span-2">
            <Button type="submit" disabled={save.isPending}>
              {form.id === undefined ? "Add tool" : "Save changes"}
            </Button>
            <Button type="button" variant="ghost" onClick={() => setForm(null)}>
              Cancel
            </Button>
          </div>
        </form>
      )}

      {tools.length === 0 ? (
        <p className="rounded-xl bg-[#0a0a0d] p-5 text-sm text-zinc-500">{total === 0 ? "No tools yet. Add the links your team keeps reaching for, so they live in one place." : "No tools match these tags."}</p>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {tools.map((t) => (
            <div key={t.id} className="group relative rounded-xl bg-[#0a0a0d] p-4 transition-colors hover:bg-[#111116]" aria-label={`Tool ${t.name}`}>
              <a href={t.url} target="_blank" rel="noopener noreferrer" className="block focus:outline-none">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-semibold text-sky-400 group-hover:text-sky-300">{t.name}</span>
                  <ExternalLink className="size-3.5 shrink-0 text-zinc-500" strokeWidth={1.5} />
                </div>
                {t.description && <p className="mt-1 text-sm text-zinc-400">{t.description}</p>}
                <p className="mt-1 truncate font-mono text-xs text-zinc-600">{new URL(t.url).host}</p>
                {t.tags.length > 0 && (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {t.tags.map((x) => (
                      <span key={x} className="rounded bg-[#141418] px-1.5 py-0.5 text-xs font-mono text-zinc-400">
                        {x}
                      </span>
                    ))}
                  </div>
                )}
              </a>
              {admin && (
                <div className="absolute right-2 top-9 flex gap-1 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
                  <button
                    type="button"
                    title="Edit"
                    aria-label={`Edit ${t.name}`}
                    onClick={() => { setErr(null); setForm({ id: t.id, name: t.name, url: t.url, description: t.description, tags: t.tags.join(", ") }); }}
                    className="rounded p-1.5 text-zinc-400 hover:bg-zinc-900 hover:text-white"
                  >
                    <Pencil className="size-3.5" strokeWidth={1.5} />
                  </button>
                  <button type="button" title="Delete" aria-label={`Delete ${t.name}`} onClick={() => setDel(t)} className="rounded p-1.5 text-zinc-400 hover:bg-zinc-900 hover:text-rose-300">
                    <Trash2 className="size-3.5" strokeWidth={1.5} />
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={!!del}
        title={`Delete ${del?.name}?`}
        body={<p>The link is removed from the Apps page. The tool itself is not touched.</p>}
        confirmLabel="Delete tool"
        busy={remove.isPending}
        onConfirm={() => del && remove.mutate(del.id)}
        onCancel={() => setDel(null)}
      />
    </section>
  );
}
