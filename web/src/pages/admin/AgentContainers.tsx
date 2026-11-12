import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type AgentContainer } from "@/lib/api";
import { allowYaml, catalogYaml } from "@/lib/agentSetup";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { CopyBlock } from "@/components/CopyBlock";

const key = (c: AgentContainer) => `${c.compose_project}/${c.service}`;

/** What is running on an agent's host: what the catalog uses, what the agent exposes, what it only found. */
export function AgentContainers({ agentId }: { agentId: string }) {
  const q = useQuery<{ containers: AgentContainer[] }>({
    queryKey: ["agent-containers", agentId],
    queryFn: () => api(`/api/admin/agents/${agentId}/containers`),
    refetchInterval: 10_000,
  });
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [repo, setRepo] = useState("");
  const [env, setEnv] = useState("develop");
  const [tier, setTier] = useState<"nonprod" | "prod">("nonprod");

  const all = q.data?.containers ?? [];
  const todo = all.filter((c) => c.catalog.length === 0);
  const chosen = useMemo(() => todo.filter((c) => picked.has(key(c))), [todo, picked]);

  if (q.isLoading) return <div className="px-1 py-3 text-xs font-mono text-zinc-500">loading</div>;

  const toggle = (c: AgentContainer) =>
    setPicked((p) => {
      const n = new Set(p);
      if (n.has(key(c))) n.delete(key(c));
      else n.add(key(c));
      return n;
    });

  const status = (c: AgentContainer) =>
    c.catalog.length > 0 ? <Badge tone="ok">in the catalog: {c.catalog.join(", ")}</Badge> : c.exposed ? <Badge tone="warn">exposed, not in the catalog</Badge> : <Badge tone="default">found, not exposed</Badge>;

  return (
    <Card className="space-y-4 bg-[#0a0a0d] p-5" aria-label={`Containers on ${agentId}`}>
      <div>
        <h2 className="text-base font-semibold text-white">Containers on {agentId}</h2>
        <p className="mt-1 text-sm text-zinc-400">
          Services found on this host. Pick the ones to add to Helmsman and it drafts the two files to change. Services that are not exposed appear here only while the agent has <code className="font-mono text-zinc-300">discovery: true</code>
          , and only their names, image and state are reported.
        </p>
      </div>

      {all.length === 0 ? (
        <p className="rounded-lg bg-[#141418] p-3 text-sm text-zinc-400">
          Nothing reported yet. Check that the agent is online. To see services it does not expose yet, set <code className="font-mono text-zinc-300">discovery: true</code> in its agent.yaml and recreate it.
        </p>
      ) : (
        <ul className="divide-y divide-zinc-900/60 rounded-lg bg-[#060608]">
          {all.map((c) => (
            <li key={key(c)} className="flex flex-wrap items-center gap-3 px-3 py-2.5">
              <input
                type="checkbox"
                aria-label={`Select ${key(c)}`}
                disabled={c.catalog.length > 0}
                checked={picked.has(key(c))}
                onChange={() => toggle(c)}
                className="size-4 accent-sky-400"
              />
              <span className="font-mono text-sm text-white">{key(c)}</span>
              <span className="min-w-0 truncate font-mono text-xs text-zinc-500">{c.image}</span>
              <span className="font-mono text-xs text-zinc-400">{c.state}</span>
              <span className="ml-auto">{status(c)}</span>
            </li>
          ))}
        </ul>
      )}

      {chosen.length > 0 && (
        <div className="space-y-4 rounded-lg bg-[#101014] p-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <label className="block text-xs font-mono text-zinc-400">
              Repository
              <Input className="mt-1.5 bg-black" placeholder="my-org/my-app" value={repo} onChange={(e) => setRepo(e.target.value)} />
            </label>
            <label className="block text-xs font-mono text-zinc-400">
              Environment name
              <Input className="mt-1.5 bg-black" value={env} onChange={(e) => setEnv(e.target.value)} />
            </label>
            <label className="block text-xs font-mono text-zinc-400">
              Tier
              <select
                className="mt-1.5 h-9 w-full rounded-md bg-black px-3 text-xs text-zinc-100"
                value={tier}
                onChange={(e) => setTier(e.target.value as "nonprod" | "prod")}
              >
                <option value="nonprod">nonprod</option>
                <option value="prod">prod</option>
              </select>
            </label>
          </div>
          <CopyBlock title="1. On the host: add to agent.yaml" hint="Then recreate the agent: docker compose -f compose.agent.yml --env-file .env up -d --force-recreate" text={allowYaml(chosen.filter((c) => !c.exposed))} />
          <CopyBlock
            title="2. In the catalog: add under apps:"
            hint="deploy/server/apps.yaml. Replace the CHANGE-ME addresses. The server picks the file up within 30 seconds."
            text={catalogYaml({ agent: agentId, repo, env, tier, items: chosen })}
          />
        </div>
      )}
    </Card>
  );
}
