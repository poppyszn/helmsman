import { expect, type APIRequestContext, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { createHmac } from "node:crypto";

export const AUTH_DIR = ".auth";
export const authFile = (user: string) => `${AUTH_DIR}/${user}.json`;

const FAKE_GITHUB = process.env.E2E_FAKE_GITHUB ?? "http://localhost:8090";

/** Signs in through the fake GitHub picker. Works on a page with no session. */
export async function loginAs(page: Page, user: string) {
  await page.goto("/login");
  await page.getByRole("link", { name: "Sign in with GitHub" }).click();
  await page.getByRole("link", { name: user, exact: true }).click();
}

export async function newSession(browser: Browser, user: string): Promise<BrowserContext> {
  return browser.newContext({ storageState: authFile(user) });
}

/** Scripts how the fake GitHub runs workflows. Fast by default so tests do not wait 16 s. */
export async function fakeScenario(request: APIRequestContext, scenario: Record<string, unknown> = {}) {
  const res = await request.put(`${FAKE_GITHUB}/_fake/scenario`, {
    data: { appear_ms: 300, queued_ms: 200, run_ms: 1500, ...scenario },
  });
  expect(res.status()).toBe(204);
}

/** Waits until no run is active in an environment, so tests do not collide on the one-run-per-env rule. */
export async function waitIdle(request: APIRequestContext, base: string, app: string, env: string) {
  await expect
    .poll(
      async () => {
        const res = await request.get(`${base}/api/apps/${app}/envs/${env}/runs`);
        const body = (await res.json()) as { runs: { status: string }[] };
        return body.runs.filter((r) => ["pending_approval", "dispatching", "queued", "in_progress", "waiting"].includes(r.status)).length;
      },
      { timeout: 60_000, message: `${app}/${env} still has an active run` },
    )
    .toBe(0);
}

/** The code an authenticator app would show now (RFC 6238, SHA-1, 6 digits, 30 s). */
export function totp(secretBase32: string, at = Date.now()): string {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  const clean = secretBase32.replace(/[\s=]/g, "").toUpperCase();
  let bits = "";
  for (const ch of clean) bits += alphabet.indexOf(ch).toString(2).padStart(5, "0");
  const bytes = Buffer.from(bits.match(/.{8}/g)!.map((b) => parseInt(b, 2)));
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)));
  const h = createHmac("sha1", bytes).update(counter).digest();
  const off = h[h.length - 1] & 0x0f;
  const code = (h.readUInt32BE(off) & 0x7fffffff) % 1_000_000;
  return code.toString().padStart(6, "0");
}

/**
 * Ends anything left active in an environment by an earlier, interrupted run, so a test starts clean.
 * It cancels running runs and withdraws requests that were waiting for approval.
 */
export async function clearActive(request: APIRequestContext, app: string, env: string) {
  const me = (await (await request.get("/api/me")).json()) as { csrf_token: string };
  const headers = { "X-CSRF-Token": me.csrf_token };
  const base = `/api/apps/${app}/envs/${env}`;
  const list = (await (await request.get(`${base}/runs`)).json()) as { runs: { id: string; status: string; can_cancel: boolean; can_reject: boolean }[] };
  for (const r of list.runs) {
    if (r.status === "pending_approval" && r.can_reject) await request.post(`${base}/runs/${r.id}/reject`, { headers, data: {} });
    else if (r.can_cancel) await request.post(`${base}/runs/${r.id}/cancel`, { headers, data: {} });
  }
}
