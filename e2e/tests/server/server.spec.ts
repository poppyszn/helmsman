import { expect, test } from "@playwright/test";
import { totp } from "../support";

// A check of a real deployment. The first group needs nothing but the URL and never changes state.
// The second signs in with the break-glass admin, so it needs credentials in the environment:
//   E2E_BREAKGLASS_PASSWORD   the password from cmd/breakglass-init
//   E2E_BREAKGLASS_TOTP_SECRET   the base32 TOTP secret
// It deliberately never tries a wrong password: five failures lock break-glass for 15 minutes.
//   make e2e-server
const host = new URL(process.env.E2E_SERVER_URL ?? "https://portal.alphaskysolutions.com");

test.describe("the public surface", () => {
  test("health and readiness answer over HTTPS with a valid certificate", async ({ request }) => {
    const health = await request.get("/healthz");
    expect(health.status()).toBe(200);
    const ready = await request.get("/readyz");
    expect(ready.status()).toBe(200);
    expect(await ready.json()).toEqual({ status: "ready" });
  });

  test("plain HTTP is redirected to HTTPS", async ({ request }) => {
    const plain = process.env.E2E_HTTP_URL ?? `http://${host.hostname}/`;
    const res = await request.get(plain, { maxRedirects: 0 });
    expect([301, 302, 307, 308]).toContain(res.status());
    expect(res.headers()["location"]).toMatch(/^https:\/\//);
  });

  test("responses carry the security headers", async ({ request }) => {
    const res = await request.get("/healthz");
    const h = res.headers();
    expect(h["strict-transport-security"]).toBeTruthy();
    expect(h["content-security-policy"]).toContain("default-src 'self'");
    expect(h["x-content-type-options"]).toBe("nosniff");
  });

  test("the sign-in page offers GitHub and the redirect uses state and PKCE", async ({ page, request }) => {
    await page.goto("/login");
    await expect(page.getByRole("link", { name: "Sign in with GitHub" })).toBeVisible();
    const res = await request.get("/auth/github/login", { maxRedirects: 0 });
    expect(res.status()).toBe(302);
    const to = new URL(res.headers()["location"]);
    expect(to.hostname).toBe("github.com");
    expect(to.searchParams.get("client_id")).toBeTruthy();
    expect(to.searchParams.get("state")).toBeTruthy();
    expect(to.searchParams.get("code_challenge_method")).toBe("S256");
    expect(to.searchParams.get("redirect_uri")).toBe(`${host.origin}/auth/github/callback`);
  });

  test("the API is closed without a session", async ({ request }) => {
    for (const path of ["/api/me", "/api/apps", "/api/admin/users", "/api/admin/audit"]) {
      expect((await request.get(path)).status(), path).toBe(401);
    }
    const post = await request.post("/api/apps/x/envs/y/restart", { data: {} });
    expect(post.status()).toBe(401);
  });

  test("an agent cannot connect without a credential", async ({ request }) => {
    const res = await request.get("/agent/ws");
    expect([400, 401, 403]).toContain(res.status());
    const join = await request.post("/agent/join", { data: { join_token: "not-a-real-token", hostname: "e2e", version: "e2e" } });
    expect(join.status()).toBe(401);
  });

  test("a page on another site cannot open a log stream", async ({ request }) => {
    const res = await request.get("/ws/apps/x/envs/y/logs", { headers: { Origin: "https://evil.example" } });
    expect([401, 403]).toContain(res.status());
  });

  test("unknown API paths are 404, not the web app", async ({ request }) => {
    const res = await request.get("/api/does-not-exist");
    expect([401, 404]).toContain(res.status());
  });
});

const password = process.env.E2E_BREAKGLASS_PASSWORD;
const secret = process.env.E2E_BREAKGLASS_TOTP_SECRET;

test.describe("signed in as the break-glass admin", () => {
  test.skip(!password || !secret, "set E2E_BREAKGLASS_PASSWORD and E2E_BREAKGLASS_TOTP_SECRET to run these");
  test.describe.configure({ mode: "serial" });

  test("emergency sign-in works and lands on the applications", async ({ page }) => {
    await page.goto("/login");
    await page.getByRole("button", { name: "Emergency access" }).click();
    await page.getByLabel("Password").fill(password!);
    await page.getByLabel("Authenticator code").fill(totp(secret!));
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Applications" })).toBeVisible();
    await expect(page.getByText("breakglass", { exact: true })).toBeVisible();
    await page.context().storageState({ path: ".auth/server-admin.json" });
  });

  test.describe("with that session", () => {
    test.use({ storageState: ".auth/server-admin.json" });

    test("the admin pages are available", async ({ page }) => {
      for (const [path, heading] of [
        ["/admin/users", "Users"],
        ["/admin/bindings", "Access & Roles"],
        ["/admin/catalog", "Catalog"],
        ["/admin/agents", "Host Agents"],
        ["/admin/github", "GitHub"],
        ["/admin/audit", "Audit Log"],
      ] as const) {
        await page.goto(path);
        await expect(page.getByRole("heading", { name: heading }), path).toBeVisible();
      }
    });

    test("the catalog loaded without an error", async ({ page }) => {
      await page.goto("/admin/catalog");
      await expect(page.getByRole("alert")).toHaveCount(0);
    });

    test("every agent is free of alerts, and every repository has actions enabled or a stated reason", async ({ page }) => {
      await page.goto("/admin/agents");
      await expect(page.getByRole("alert")).toHaveCount(0);
      await page.goto("/admin/github");
      for (const row of await page.getByRole("row").all()) {
        const text = (await row.innerText()).replace(/\s+/g, " ");
        if (/\/.+ (enabled|disabled)/.test(text)) expect(text, "repository row").toMatch(/enabled|disabled: .+/);
      }
    });

    test("the sign-in was written to the audit log", async ({ page }) => {
      await page.goto("/admin/audit");
      const row = page.getByRole("row").filter({ hasText: "auth.breakglass" }).first();
      await expect(row).toContainText("ok");
      await expect(row).toContainText("breakglass");
    });

    test("the audit log cannot be edited through the API", async ({ page }) => {
      const csrf = ((await (await page.request.get("/api/me")).json()) as { csrf_token: string }).csrf_token;
      const res = await page.request.delete("/api/admin/audit", { headers: { "X-CSRF-Token": csrf } });
      expect([404, 405]).toContain(res.status());
    });

    test("signing out ends the session", async ({ page }) => {
      await page.goto("/");
      await page.getByRole("button", { name: "Sign out" }).click();
      await expect(page).toHaveURL(/\/login/);
      expect((await page.request.get("/api/me")).status()).toBe(401);
    });
  });
});
