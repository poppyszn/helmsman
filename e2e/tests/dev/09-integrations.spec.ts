import { expect, test, type Page } from "@playwright/test";
import { authFile } from "../support";

// The Integrations page: an admin connects Loki (log history) from the UI, with a guide. Runs after the
// history tests, and leaves things as it found them.
test.describe.configure({ mode: "serial" });

const LOKI = process.env.E2E_LOKI_URL ?? "http://loki:3100"; // as the Helmsman server reaches it

test.describe("as an admin", () => {
  test.use({ storageState: authFile("alice") });

  async function open(page: Page) {
    await page.goto("/admin/integrations");
    await expect(page.getByRole("heading", { name: "Integrations" })).toBeVisible();
  }

  test("the page explains log history and walks through setting it up", async ({ page }) => {
    await open(page);
    await expect(page.getByRole("heading", { name: "Log history (Loki)" })).toBeVisible();
    await expect(page.getByText("Nothing here is needed to run it.")).toBeVisible();
    await expect(page.getByText("How to set up log history")).toBeVisible();
    // The guide starts open when Loki is not connected and folded away when it is.
    if (!(await page.locator("details").evaluate((el) => (el as HTMLDetailsElement).open))) await page.getByText("How to set up log history").click();
    for (const step of ["Run Loki and Alloy", "Enter the address above", "Test connection", "Save and turn on", "Optional: browser logs for static apps"]) {
      await expect(page.getByText(step, { exact: true }).first()).toBeVisible();
    }
  });

  test("a wrong address is explained in plain words, and nothing is turned on", async ({ page }) => {
    await open(page);
    await page.getByPlaceholder("http://loki:3100").fill("http://127.0.0.1:1");
    await page.getByRole("button", { name: "Test connection" }).click();
    await expect(page.getByLabel("Connection test")).toContainText("Nothing is listening at that address");
    await page.getByRole("button", { name: /Save and turn on|Save changes/ }).click();
    await expect(page.getByRole("alert")).toContainText("not turned on");
  });

  test("an address that is not allowed is refused", async ({ page }) => {
    await open(page);
    for (const bad of ["ftp://loki:3100", "http://user:pw@loki:3100", "http://169.254.169.254/latest"]) {
      await page.getByPlaceholder("http://loki:3100").fill(bad);
      await page.getByRole("button", { name: "Test connection" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
    }
  });

  test("connect, use, turn off and remove, all from the page", async ({ page, request, baseURL }) => {
    const probe = await request.post(`${baseURL}/api/admin/integrations/loki/test`, {
      headers: { "X-CSRF-Token": ((await (await request.get("/api/me")).json()) as { csrf_token: string }).csrf_token },
      data: { url: LOKI },
    });
    test.skip(!(await probe.json()).reachable, "Loki is not running (use make up-logs)");

    await open(page);
    await page.getByPlaceholder("http://loki:3100").fill(LOKI);
    await page.getByRole("button", { name: "Test connection" }).click();
    await expect(page.getByLabel("Connection test")).toContainText("Loki answered");

    await page.getByRole("button", { name: /Save and turn on|Save changes/ }).click();
    await expect(page.getByRole("status")).toContainText("Log history is on");
    await expect(page.getByText("connected", { exact: true })).toBeVisible();

    // History now works for an environment.
    await page.goto("/apps/static-demo");
    await page.getByRole("button", { name: "history", exact: true }).click();
    await expect(page.getByRole("region", { name: "Log history" })).toBeVisible();

    // Turn it off: history says so, and points an admin to this page.
    await open(page);
    await page.getByRole("button", { name: "Turn off" }).click();
    await expect(page.getByRole("status")).toContainText("Log history is off");
    await expect(page.getByText("turned off", { exact: true })).toBeVisible();
    await page.goto("/apps/static-demo");
    await page.getByRole("button", { name: "history", exact: true }).click();
    await expect(page.getByText("Log history is not connected.")).toBeVisible();
    await page.getByRole("link", { name: "Connect Loki in Integrations" }).click();
    await expect(page).toHaveURL(/\/admin\/integrations/);

    // Turn it back on, then remove the saved settings (the server's environment, if any, applies again).
    await page.getByRole("button", { name: /Save and turn on|Save changes/ }).click();
    await expect(page.getByRole("status")).toContainText("Log history is on");
    await page.getByRole("button", { name: "Remove settings" }).click();
    await expect(page.getByRole("status")).toContainText("settings were removed");
  });

  test("changes are in the audit log, without any password", async ({ page }) => {
    const res = await page.request.get("/api/admin/audit?limit=100");
    const rows = (await res.json()) as { action: string; target: string; details: unknown }[];
    expect(rows.some((r) => r.action === "integration.loki.tested")).toBe(true);
    expect(JSON.stringify(rows.filter((r) => r.action.startsWith("integration.")))).not.toMatch(/password":"[^"]/);
  });
});

test.describe("as someone who is not an admin", () => {
  test.use({ storageState: authFile("bob") });

  test("there is no Integrations link, and the API refuses", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("link", { name: "Integrations", exact: true })).toHaveCount(0);
    for (const [method, path] of [["GET", "/api/admin/integrations"], ["PUT", "/api/admin/integrations/loki"]] as const) {
      const csrf = ((await (await page.request.get("/api/me")).json()) as { csrf_token: string }).csrf_token;
      const res = await page.request.fetch(path, { method, headers: { "X-CSRF-Token": csrf }, data: { url: "http://loki:3100" } });
      expect(res.status(), path).toBe(403);
    }
  });
});
