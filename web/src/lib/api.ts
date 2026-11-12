import { useQuery } from "@tanstack/react-query";

let csrfToken = "";

export class ApiError extends Error {
  status: number;
  body: unknown;
  constructor(status: number, message: string, body?: unknown) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

export async function api<T>(path: string, init?: RequestInit & { json?: unknown }): Promise<T> {
  const headers: Record<string, string> = {};
  let body: BodyInit | undefined;
  if (init?.json !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(init.json);
  }
  if (init?.method && init.method !== "GET") headers["X-CSRF-Token"] = csrfToken;
  const res = await fetch(path, { ...init, headers, body, credentials: "same-origin" });
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, (data as { error?: string }).error ?? `HTTP ${res.status}`, data);
  return data as T;
}

export type Me = {
  user: { id: number; login: string; name: string | null; avatar_url: string | null; is_admin: boolean };
  csrf_token: string;
  can: { admin: boolean; audit: boolean; agents: boolean; approve: boolean };
  display_tz: string;
};

export function useMe() {
  return useQuery<Me, ApiError>({
    queryKey: ["me"],
    queryFn: async () => {
      const me = await api<Me>("/api/me");
      csrfToken = me.csrf_token;
      return me;
    },
    retry: false,
    staleTime: 60_000,
  });
}

export type Status = "healthy" | "degraded" | "down" | "unknown";
export type Env = { env: string; tier: "nonprod" | "prod"; url: string; allow_seed: boolean; status: Status };
export type App = { id: string; name: string; repo: string; environments: Env[] };

export function formatTime(iso: string | null | undefined, tz: string): string {
  if (!iso) return "never";
  return new Intl.DateTimeFormat("en-GB", { dateStyle: "medium", timeStyle: "medium", timeZone: tz }).format(new Date(iso));
}

export type EnvStatus = {
  env: string;
  status: Status;
  reason: "" | "agent_offline" | "docker_unavailable" | "container_missing";
  agent: { online: boolean; last_seen: string | null; version: string; outdated: boolean; docker_ok: boolean | null; protocol_version: number };
  container: { state: string; health: string; image: string; revision: string; started_at: string | null; restart_count: number; updated_at: string } | null;
  last_restart: string | null;
  crash_loop: boolean;
  health: { configured: boolean; via: string; latest: { status: string; http_code: number; latency_ms: number; checked_at: string } | null };
  events: { at: string; agent_ts: string | null; event: string; detail: string | null }[];
  can_logs: boolean;
  can_restart: boolean;
  blocked: { logs: string; restart: string };
};

export type Agent = {
  id: string;
  name: string;
  version: string | null;
  hostname: string | null;
  protocol_version: number | null;
  docker_ok: boolean | null;
  last_seen_at: string | null;
  revoked_at: string | null;
  created_at: string;
  alert: string | null;
  alert_at: string | null;
  joined: boolean;
  join_pending: boolean;
  online: boolean;
  outdated: boolean;
};

export type RunStatus =
  | "pending_approval"
  | "rejected"
  | "dispatching"
  | "queued"
  | "in_progress"
  | "waiting"
  | "succeeded"
  | "failed"
  | "cancelled"
  | "unknown";

export type RunStep = {
  name: string;
  status: string;
  conclusion: string | null;
  number: number;
  started_at?: string | null;
  completed_at?: string | null;
};
export type RunJob = {
  id: number;
  name: string;
  status: string;
  conclusion: string | null;
  html_url: string;
  started_at?: string | null;
  completed_at?: string | null;
  steps: RunStep[] | null;
};

export type Run = {
  id: string;
  app: string;
  env: string;
  tier: "nonprod" | "prod";
  action: "migrate" | "seed";
  status: RunStatus;
  requested_by: string;
  approved_by: string | null;
  rejected_by: string | null;
  gh_run_url: string | null;
  workflow_url: string;
  ref: string | null;
  warning: string | null;
  error: string | null;
  jobs: RunJob[] | null;
  rerun_of: string | null;
  created_at: string;
  finished_at: string | null;
  can_approve: boolean;
  can_reject: boolean;
  can_cancel: boolean;
  can_rerun: boolean;
};

export type RunsResponse = {
  runs: Run[];
  can_migrate: boolean;
  can_seed: boolean;
  seed_allowed: boolean;
  needs_approval: boolean;
  disabled_reason: string;
  repo: string;
};

export type Deployment = {
  run_id: number;
  title: string;
  commit: string;
  actor: string;
  at: string;
  status: "succeeded" | "failed" | "in_progress" | "queued" | "cancelled" | "waiting";
  url: string;
  matches_running?: boolean;
};

export type DeploymentsResponse = {
  configured: boolean;
  deployments: Deployment[];
  running_revision: string;
  latest_success_commit: string;
  mismatch: boolean;
  error?: string;
};

export type RepoCheck = {
  repo: string;
  installed: boolean;
  workflows: { file: string; role: string; exists: boolean; has_dispatch: boolean; error?: string }[] | null;
  problem: string;
  checked_at: string;
};

export const runActive = (s: RunStatus) => ["dispatching", "queued", "in_progress", "waiting"].includes(s);

export type HistoryEntry = {
  ts: string;
  source: "container" | "browser";
  kind: "access" | "error" | "app" | "exception" | "event" | "measurement";
  level: "error" | "warn" | "info" | "debug";
  text: string;
  session?: string;
  page?: string;
  version?: string;
};

export type HistoryResponse = {
  entries: HistoryEntry[];
  sources: { container: boolean; browser: boolean };
  kinds: { container: string[]; browser: string[] };
  truncated: boolean;
  next_before: string | null;
  retention: string;
};

/** An nginx access line in the default "combined" format, or a JSON access line. The same test Alloy applies when it labels history. */
const ACCESS_LINE = /^\S+ \S+ \S+ \[[^\]]+\] "[A-Z]+ [^"]*" \d{3} |^\{.*"(status|response_code)":\s*"?\d{3}"?.*"(request|uri|path)"/;
export const isAccessLine = (text: string) => ACCESS_LINE.test(text);

export type LokiStatus = {
  connected: boolean;
  enabled: boolean;
  url: string;
  user: string;
  has_password: boolean;
  source: "none" | "database" | "environment";
  updated_by?: string;
  updated_at?: string;
};

export type LokiTest = { reachable: boolean; error?: string; labels: string[] | null; has_logs: boolean; missing_labels?: string[] };

export type AgentContainer = {
  compose_project: string;
  service: string;
  image: string;
  state: string;
  exposed: boolean;
  /** "app/env" entries of the catalog that already use this service. */
  catalog: string[];
};
export type AgentInstall = { agent_yaml: string; compose: string; env: string; commands: string };

export type NotifyChannel = {
  id: number;
  name: string;
  kind: "email" | "webhook";
  to?: string[];
  url?: string;
  has_secret: boolean;
  events: string[];
  tier: "all" | "prod" | "nonprod";
  enabled: boolean;
  created_by?: string;
};
export type NotifyDelivery = {
  id: number;
  channel: string;
  event: string;
  title: string;
  status: "pending" | "sent" | "failed";
  attempts: number;
  last_error?: string;
  created_at: string;
  sent_at?: string;
};
export type SmtpStatus = {
  configured: boolean;
  host: string;
  port: number;
  security: "starttls" | "tls" | "none";
  user: string;
  from: string;
  has_password: boolean;
  updated_by?: string;
  updated_at?: string;
};
export type NotifyEventType = { id: string; label: string; default: boolean };
export type NotificationsResp = { smtp: SmtpStatus; channels: NotifyChannel[]; deliveries: NotifyDelivery[]; events: NotifyEventType[] };
