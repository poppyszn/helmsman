import { expect, test, type Page } from "@playwright/test";
import { authFile } from "../support";

// Log history from Loki: nginx container logs and browser (Faro) telemetry for a static web app, side by
// side and filterable. Needs the dev stack started with `make up-logs`; the tests skip themselves if
// history is not set up.
const FARO = process.env.E2E_FARO_URL ?? "http://localhost:12347";
const STATIC = process.env.E2E_STATIC_URL ?? "http://localhost:9003";

test.use({ storageState: authFile("alice") });
test.describe.configure({ mode: "serial" });

const marker = `e2e-${Date.now().toString(36)}`;

test.beforeAll(async ({ playwright, baseURL }) => {
  const api = await playwright.request.newContext({ baseURL, storageState: authFile("alice") });
  const res = await api.get("/api/apps/static-demo/envs/staging/logs/history");
  test.skip(res.status() === 503, "log history is not set up (run make up-logs)");
  await api.dispose();

  // Real traffic: nginx access and error lines, and browser telemetry in Faro's payload format.
  const raw = await playwright.request.newContext();
  for (let i = 0; i < 3; i++) await raw.get(`${STATIC}/?${marker}=${i}`);
  await raw.get(`${STATIC}/${marker}-missing.js`); // an access 404 and an nginx error line
  const ts = new Date().toISOString();
  const meta = {
    sdk: { name: "e2e", version: "0" },
    app: { name: "static-demo", version: "9.9.9", environment: "staging" },
    session: { id: "e2esession0001" },
    page: { url: `http://localhost:9003/checkout` },
  };
  const post = await raw.post(`${FARO}/collect`, {
    headers: { Origin: "http://localhost:9003" },
    data: {
      meta,
      logs: [
        { message: `${marker} console message`, level: "info", timestamp: ts },
        { message: `${marker} slow request`, level: "warn", timestamp: ts },
      ],
      exceptions: [{ type: "TypeError", value: `${marker} cannot read x`, timestamp: ts }],
      events: [{ name: `${marker}_click`, attributes: { id: "pay" }, timestamp: ts }],
      measurements: [{ type: "web-vitals", values: { lcp: 1234.5 }, timestamp: ts }],
    },
  });
  expect(post.status()).toBe(202);
  await raw.dispose();
});

async function openHistory(page: Page) {
  await page.goto("/apps/static-demo");
  await page.getByRole("button", { name: "history", exact: true }).click();
  await expect(page.getByRole("region", { name: "Log history" })).toBeVisible();
}

const history = (page: Page) => page.getByRole("region", { name: "Log history" });
const rows = (page: Page) => history(page).getByRole("listitem");

// Loki takes a few seconds to index what Alloy sends, so look again until it shows up.
async function eventually(page: Page, check: () => Promise<void>) {
  await expect(async () => {
    await history(page).getByRole("button", { name: "refresh" }).click();
    await check();
  }).toPass({ timeout: 60_000, intervals: [2_000] });
}

test("container logs and browser telemetry appear together, newest first", async ({ page }) => {
  await openHistory(page);
  await history(page).getByPlaceholder("search...").fill(marker);
  await eventually(page, async () => {
    await expect(rows(page).filter({ hasText: `${marker} console message` })).toHaveCount(1);
  });
  // Browser lines are rendered for people, with where they came from.
  const exception = rows(page).filter({ hasText: "cannot read x" });
  await expect(exception).toContainText("TypeError: ");
  await expect(exception).toContainText("/checkout");
  await expect(exception).toContainText("v9.9.9");
  await expect(exception).toContainText("session e2esessi");
  await expect(exception).toHaveAttribute("data-source", "browser");
  await expect(exception).toHaveAttribute("data-kind", "exception");
  await expect(rows(page).filter({ hasText: `${marker}_click` })).toContainText("id=pay");
});

test("access lines are hidden by default and one click brings them back", async ({ page }) => {
  await openHistory(page);
  await history(page).getByPlaceholder("search...").fill(marker);
  await eventually(page, async () => {
    await expect(rows(page).filter({ hasText: `${marker} console message` })).toHaveCount(1);
  });
  await expect(history(page).locator('[data-kind="access"]')).toHaveCount(0);
  await history(page).getByRole("button", { name: "access" }).click();
  await eventually(page, async () => {
    await expect(history(page).locator('[data-kind="access"]').first()).toContainText("GET /");
  });
  await expect(history(page).locator('[data-kind="access"]').first()).toHaveAttribute("data-source", "container");
});

test("a 404 shows as a warning access line, and the nginx error line is its own type", async ({ page }) => {
  await openHistory(page);
  await history(page).getByRole("button", { name: "access" }).click();
  await history(page).getByPlaceholder("search...").fill(`${marker}-missing.js`);
  await eventually(page, async () => {
    await expect(history(page).locator('[data-kind="access"]').filter({ hasText: "404" })).toHaveCount(1);
    await expect(history(page).locator('[data-kind="error"]').filter({ hasText: "failed (2: No such file" })).toHaveCount(1);
  });
});

test("the source filter separates the container from the browser", async ({ page }) => {
  await openHistory(page);
  await history(page).getByPlaceholder("search...").fill(marker);
  await eventually(page, async () => {
    await expect(rows(page).filter({ hasText: `${marker} console message` })).toHaveCount(1);
  });
  await history(page).getByRole("button", { name: "container" }).click(); // turn the container off
  await eventually(page, async () => {
    await expect(history(page).locator('[data-source="container"]')).toHaveCount(0);
    await expect(history(page).locator('[data-source="browser"]').first()).toBeVisible();
  });
  await history(page).getByRole("button", { name: "container" }).click();
  await history(page).getByRole("button", { name: "browser" }).click(); // browser off
  await history(page).getByRole("button", { name: "access" }).click();
  await history(page).getByPlaceholder("search...").fill(`${marker}-missing.js`);
  await eventually(page, async () => {
    await expect(history(page).locator('[data-source="browser"]')).toHaveCount(0);
    await expect(history(page).locator('[data-source="container"]').first()).toBeVisible();
  });
});

test("type and level filters narrow the list", async ({ page }) => {
  await openHistory(page);
  await history(page).getByPlaceholder("search...").fill(marker);
  await eventually(page, async () => {
    await expect(rows(page).filter({ hasText: `${marker} console message` })).toHaveCount(1);
  });
  // Only exceptions.
  for (const t of ["server errors", "messages", "events", "measurements"]) await history(page).getByRole("button", { name: t }).click();
  await eventually(page, async () => {
    await expect(rows(page)).toHaveCount(1);
    await expect(rows(page).first()).toHaveAttribute("data-kind", "exception");
  });
  // Back to messages only, and only warnings.
  await history(page).getByRole("button", { name: "exceptions" }).click();
  await history(page).getByRole("button", { name: "messages" }).click();
  for (const l of ["error", "info", "debug"]) await history(page).getByRole("group", { name: "Level" }).getByRole("button", { name: l }).click();
  await eventually(page, async () => {
    await expect(rows(page)).toHaveCount(1);
    await expect(rows(page).first()).toContainText(`${marker} slow request`);
  });
});

test("the time range and the empty state", async ({ page }) => {
  await openHistory(page);
  await history(page).getByPlaceholder("search...").fill("zz-nothing-will-match-this-zz");
  await expect(history(page).getByText(/no lines match these filters/)).toBeVisible();
  await history(page).getByRole("button", { name: "7 days" }).click();
  await expect(history(page).getByRole("button", { name: "7 days" })).toHaveAttribute("aria-pressed", "true");
  await expect(history(page).getByText(/no lines match these filters in the last 7 days/)).toBeVisible();
});

test("the live tail can hide nginx access lines too", async ({ page, request }) => {
  await page.goto("/apps/static-demo");
  const logs = page.getByRole("region", { name: "Logs" });
  await logs.getByRole("button", { name: "tail logs" }).click();
  await expect(logs.getByRole("button", { name: "stop" })).toBeVisible();
  await request.get(`${STATIC}/?live=${marker}`);
  await expect(logs.getByText(new RegExp(`GET /\\?live=${marker}`)).first()).toBeVisible();
  await logs.getByLabel("hide access lines").check();
  await expect(logs.getByText(new RegExp(`GET /\\?live=${marker}`))).toHaveCount(0);
  await logs.getByLabel("hide access lines").uncheck();
  await expect(logs.getByText(new RegExp(`GET /\\?live=${marker}`)).first()).toBeVisible();
});

test("history is audited without the search text", async ({ page }) => {
  await openHistory(page);
  await history(page).getByPlaceholder("search...").fill("card-4111-do-not-store");
  await expect(history(page).getByText(/no lines match/)).toBeVisible();
  const res = await page.request.get("/api/admin/audit?limit=50");
  const entries = (await res.json()) as { action: string; target: string }[];
  expect(entries.some((e) => e.action === "logs.history" && e.target === "static-demo/staging")).toBe(true);
  expect(JSON.stringify(entries)).not.toContain("card-4111-do-not-store");
});

test("the real Grafana Faro SDK, running in a browser, reports errors and events that show up here", async ({ page, request }) => {
  const bundle = "https://unpkg.com/@grafana/faro-web-sdk@2.14.0/dist/bundle/faro-web-sdk.iife.js";
  const reachable = await request.get(bundle).then((r) => r.ok(), () => false);
  test.skip(!reachable, "the Faro SDK could not be downloaded from unpkg");
  const sdkMarker = `sdk-${marker}`;

  // The static demo page, served by its nginx container, gets the SDK exactly as the docs describe.
  await page.goto(STATIC);
  await page.addScriptTag({ url: bundle });
  await page.evaluate(
    ({ faro, m }) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const sdk = (window as any).GrafanaFaroWebSdk;
      const f = sdk.initializeFaro({ url: `${faro}/collect`, app: { name: "static-demo", version: "3.1.4", environment: "staging" }, batching: { enabled: false } });
      f.api.pushLog([`${m} real sdk log`], { level: sdk.LogLevel.WARN });
      f.api.pushError(new Error(`${m} real sdk error`));
      f.api.pushEvent(`${m}_real_event`, { id: "x1" });
    },
    { faro: FARO, m: sdkMarker },
  );

  await openHistory(page);
  await history(page).getByPlaceholder("search...").fill(sdkMarker);
  await eventually(page, async () => {
    await expect(rows(page).filter({ hasText: "real sdk log" })).toHaveCount(1);
    await expect(history(page).locator('[data-kind="exception"]').filter({ hasText: "real sdk error" })).toHaveCount(1);
    // The SDK may also echo the event as a console message, so look for the event itself.
    await expect(history(page).locator('[data-kind="event"]').filter({ hasText: "_real_event" })).toHaveCount(1);
  });
  const log = rows(page).filter({ hasText: "real sdk log" });
  await expect(log).toHaveAttribute("data-kind", "app");
  await expect(log).toHaveAttribute("data-source", "browser");
  await expect(log.locator('span[title="warn"]')).toBeVisible();
  const err = history(page).locator('[data-kind="exception"]').filter({ hasText: "real sdk error" });
  await expect(err).toHaveAttribute("data-source", "browser");
  await expect(err).toContainText("v3.1.4");
});

test("a viewer without log access cannot read history", async ({ browser, baseURL }) => {
  // carol is a viewer on demo-app staging only, so static-demo is invisible to her entirely.
  const ctx = await browser.newContext({ storageState: authFile("carol"), baseURL });
  const res = await ctx.request.get("/api/apps/static-demo/envs/staging/logs/history");
  expect(res.status()).toBe(403);
  const own = await ctx.request.get("/api/apps/demo-app/envs/staging/logs/history");
  expect(own.status(), "a viewer has no logs.view on her own app either").toBe(403);
  await ctx.close();
});
