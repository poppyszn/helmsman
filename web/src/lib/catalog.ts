import type { Env } from "@/lib/api";

type AppWithEnvs = { id: string; environments: Pick<Env, "env">[] };

/** Sorted unique environment slugs across every app, or one chosen app ("*" for all). */
export function catalogEnvNames(apps: AppWithEnvs[], appId: string): string[] {
  const scope = appId === "*" ? apps : apps.filter((a) => a.id === appId);
  return [...new Set(scope.flatMap((a) => a.environments.map((e) => e.env)))].sort();
}
