import type { Env, Status } from "@/lib/api";

// The worst status wins when several environments are rolled up into one dot.
const worse: Record<Status, number> = { down: 3, degraded: 2, unknown: 1, healthy: 0 };

export function worstStatus(envs: Pick<Env, "status">[]): Status {
  return envs.reduce<Status>((w, e) => (worse[e.status] > worse[w] ? e.status : w), "healthy");
}
