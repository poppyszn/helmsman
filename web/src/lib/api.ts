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
  can: { admin: boolean; audit: boolean };
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

export type Env = { env: string; tier: "nonprod" | "prod"; url: string; allow_seed: boolean };
export type App = { id: string; name: string; repo: string; environments: Env[] };

export function formatTime(iso: string | null | undefined, tz: string): string {
  if (!iso) return "never";
  return new Intl.DateTimeFormat("en-GB", { dateStyle: "medium", timeStyle: "medium", timeZone: tz }).format(new Date(iso));
}
