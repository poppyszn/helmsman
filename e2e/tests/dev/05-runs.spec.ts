import { expect, test, type Locator, type Page } from "@playwright/test";
import { authFile, clearActive, fakeScenario, waitIdle } from "../support";

// Migrations and seeds through GitHub Actions, against the fake GitHub.
test.use({ storageState: authFile("bob") });
test.describe.configure({ mode: "serial" });

test.beforeEach(async ({ request }) => {
  await clearActive(request, "demo-app", "staging"); // leftovers from an interrupted earlier run
  await waitIdle(request, "", "demo-app", "staging");
  await fakeScenario(request);
});
test.afterAll(async ({ playwright }) => {
  const ctx = await playwright.request.newContext();
  await fakeScenario(ctx);
  await ctx.dispose();
});

type Run = { id: string; row: Locator; toggle: Locator };

function runAt(page: Page, id: string): Run {
  const row = page.locator(`[data-run-id="${id}"]`);
  return { id, row, toggle: row.locator("button[aria-expanded]") };
}

async function openRuns(page: Page, env: "staging" | "prod" = "staging") {
  await page.goto("/apps/demo-app");
  if (env === "prod") await page.getByRole("tab", { name: /prod/ }).click();
  await page.getByRole("button", { name: "runs", exact: true }).click();
  await expect(page.getByRole("button", { name: "Run migrations" })).toBeVisible();
}

// Starts a run and returns it by id, so a test follows its own run and not a finished one from before.
async function startRun(page: Page, button: "Run migrations" | "Run seed"): Promise<Run> {
  const answer = page.waitForResponse((r) => /\/actions\/(migrate|seed)$/.test(r.url()) && r.request().method() === "POST");
  await page.getByRole("button", { name: button }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Start run" }).click();
  const body = (await (await answer).json()) as { run: { id: string } };
  const run = runAt(page, body.run.id);
  await expect(run.row).toBeVisible();
  return run;
}

test("a migration runs to success, with steps and a stored log", async ({ page }) => {
  await openRuns(page);
  const run = await startRun(page, "Run migrations");
  await expect(run.toggle).toContainText("migrate");
  await expect(run.toggle).toContainText("succeeded", { timeout: 40_000 });
  await run.toggle.click();
  await expect(run.row.getByText("Set up job").first()).toBeVisible();
  await expect(run.row.getByText("Run migrate").first()).toBeVisible();
  await expect(run.row.getByText("Log", { exact: true })).toBeVisible();
  await expect(run.row.locator("pre").first()).toContainText("Run migrate");
  await expect(run.row.getByRole("button", { name: "Rerun" })).toBeVisible();
});

test("a seed runs on a non-production environment", async ({ page }) => {
  await openRuns(page);
  const run = await startRun(page, "Run seed");
  await expect(run.toggle).toContainText("seed");
  await expect(run.toggle).toContainText("succeeded", { timeout: 40_000 });
});

test("a failing workflow shows as failed and keeps its log", async ({ page, request }) => {
  await fakeScenario(request, { conclusion: "failure" });
  await openRuns(page);
  const run = await startRun(page, "Run migrations");
  await expect(run.toggle).toContainText("failed", { timeout: 40_000 });
  await run.toggle.click();
  await expect(run.row.locator("pre").first()).toContainText("Error: migrate failed");
});

test("a rerun starts a new run of the same command", async ({ page, request }) => {
  await fakeScenario(request, { conclusion: "failure" });
  await openRuns(page);
  const first = await startRun(page, "Run migrations");
  await expect(first.toggle).toContainText("failed", { timeout: 40_000 });
  await fakeScenario(request);
  const answer = page.waitForResponse((r) => /\/runs\/[^/]+\/rerun$/.test(r.url()) && r.request().method() === "POST");
  await first.row.getByRole("button", { name: "Rerun" }).click();
  const body = (await (await answer).json()) as { run: { id: string; rerun_of: string } };
  expect(body.run.rerun_of).toBe(first.id);
  await expect(runAt(page, body.run.id).toggle).toContainText("succeeded", { timeout: 40_000 });
});

test("GitHub rejecting the dispatch fails the run with an explanation", async ({ page, request }) => {
  await fakeScenario(request, { dispatch: "422" });
  await openRuns(page);
  const run = await startRun(page, "Run migrations");
  await expect(run.toggle).toContainText("failed");
  await run.toggle.click();
  await expect(run.row.getByText(/rejected the dispatch/)).toBeVisible();
});

test("a GitHub rate limit fails the dispatch with a retry hint", async ({ page, request }) => {
  // More than one call is limited: background checks may use one before the dispatch does.
  await fakeScenario(request, { rate_limit_count: 3, rate_limit_retry_after: 1 });
  await openRuns(page);
  const run = await startRun(page, "Run migrations");
  await expect(run.toggle).toContainText("failed");
  await run.toggle.click();
  await expect(run.row.getByText(/rate limit/i).first()).toBeVisible();
  await page.waitForTimeout(1_500); // let the back-off pass before the next test
});

test("a run that waits for GitHub environment approval says so, then finishes", async ({ page, request }) => {
  await fakeScenario(request, { waiting: true });
  await openRuns(page);
  const run = await startRun(page, "Run migrations");
  await expect(run.toggle).toContainText("waiting for GitHub approval", { timeout: 30_000 });
  const detail = (await (await request.get(`/api/apps/demo-app/envs/staging/runs/${run.id}`)).json()) as { gh_run_id: number };
  const fake = process.env.E2E_FAKE_GITHUB ?? "http://localhost:8090";
  await request.post(`${fake}/_fake/runs/${detail.gh_run_id}/approve`);
  await expect(run.toggle).toContainText("succeeded", { timeout: 40_000 });
});

test("a long run can be cancelled", async ({ page, request }) => {
  await fakeScenario(request, { run_ms: 120_000 });
  await openRuns(page);
  const run = await startRun(page, "Run migrations");
  await expect(run.toggle).toContainText("running", { timeout: 40_000 });
  await run.row.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(run.toggle).toContainText("cancelled", { timeout: 40_000 });
});

test("only one run can be active in an environment", async ({ page, request, browser }) => {
  await fakeScenario(request, { run_ms: 120_000 });
  await openRuns(page);
  const run = await startRun(page, "Run migrations");
  await expect(run.toggle).toContainText(/starting|queued|running/);
  // A second person is refused while the first run is active.
  const alice = await browser.newContext({ storageState: authFile("alice"), baseURL: test.info().project.use.baseURL });
  const csrf = ((await (await alice.request.get("/api/me")).json()) as { csrf_token: string }).csrf_token;
  const second = await alice.request.post("/api/apps/demo-app/envs/staging/actions/migrate", { headers: { "X-CSRF-Token": csrf }, data: {} });
  expect(second.status()).toBe(409);
  const refusal = (await second.json()) as { error: string; active_run: string };
  expect(refusal.error).toContain("already active");
  expect(refusal.active_run).toBe(run.id);
  await alice.close();
  await expect(run.toggle).toContainText("running", { timeout: 40_000 });
  await run.row.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(run.toggle).toContainText("cancelled", { timeout: 40_000 });
});

test("a run that never appears on GitHub becomes unknown with a link", async ({ page, request }) => {
  test.setTimeout(150_000);
  await fakeScenario(request, { dispatch: "never_appears" });
  await openRuns(page);
  const run = await startRun(page, "Run migrations");
  await expect(run.toggle).toContainText("unknown", { timeout: 100_000 });
  await run.toggle.click();
  await expect(run.row.getByRole("link", { name: "Open the workflow on GitHub" })).toBeVisible();
});

test("the deployments tab lists the deploy workflow runs", async ({ page }) => {
  await page.goto("/apps/demo-app");
  await page.getByRole("button", { name: "deployments", exact: true }).click();
  await expect(page.getByRole("columnheader", { name: "Commit" })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "succeeded" }).first()).toBeVisible();
});

test.describe("who may run what", () => {
  test("carol, a viewer, has the buttons disabled", async ({ browser }) => {
    const ctx = await browser.newContext({ storageState: authFile("carol"), baseURL: test.info().project.use.baseURL });
    const page = await ctx.newPage();
    await page.goto("/apps/demo-app");
    await page.getByRole("button", { name: "runs", exact: true }).click();
    await expect(page.getByRole("button", { name: "Run migrations" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Run seed" })).toBeDisabled();
    await ctx.close();
  });

  test("seeding production is not offered, even to an admin", async ({ browser }) => {
    const ctx = await browser.newContext({ storageState: authFile("alice"), baseURL: test.info().project.use.baseURL });
    const page = await ctx.newPage();
    await openRuns(page, "prod");
    await expect(page.getByRole("button", { name: "Run seed" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Run migrations" })).toBeEnabled();
    await ctx.close();
  });
});
