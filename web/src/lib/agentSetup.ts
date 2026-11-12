import type { AgentContainer } from "@/lib/api";

// Drafts the two files an admin edits to expose services the agent discovered: the allow entries for the
// agent's agent.yaml, and the apps.yaml entries for the catalog. Pure text, nothing is sent anywhere.

const slug = (s: string) =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "") || "app";

const title = (s: string) => s.replace(/[-_]+/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());

export type SetupInput = { agent: string; repo: string; env: string; tier: "nonprod" | "prod"; items: AgentContainer[] };

/** The `allow:` entries to add to the agent's agent.yaml, one per compose project. */
export function allowYaml(items: AgentContainer[]): string {
  const byProject = new Map<string, string[]>();
  for (const c of items) byProject.set(c.compose_project, [...(byProject.get(c.compose_project) ?? []), c.service]);
  if (byProject.size === 0) return "# every selected service is already exposed by this agent\n";
  let out = "allow:\n";
  for (const [project, services] of byProject) {
    out += `  - compose_project: ${project}\n    services: [${services.join(", ")}]\n    actions: [logs, restart]\n`;
  }
  return out;
}

/** The catalog entries: the catalog tracks one container per environment, so one app entry per service. */
export function catalogYaml(i: SetupInput): string {
  let out = "";
  for (const c of i.items) {
    const id = slug(c.service);
    out +=
      `  - id: ${id}\n` +
      `    name: ${title(c.service)}\n` +
      `    repo: ${i.repo || "OWNER/REPO"}\n` +
      `    environments:\n` +
      `      - env: ${i.env || "develop"}\n` +
      `        tier: ${i.tier}\n` +
      `        agent: ${i.agent}\n` +
      `        container: { compose_project: ${c.compose_project}, service: ${c.service} }\n` +
      `        url: https://CHANGE-ME.example.com                 # where people open the app\n` +
      `        health: { url: https://CHANGE-ME.example.com/health, via: server }   # via: agent for a private address\n` +
      `        workflows: { migrate: migrate.yml }                # a workflow in the repo, see docs/server-setup.md\n` +
      `        github_environment: ${i.env || "develop"}\n\n`;
  }
  return out;
}
