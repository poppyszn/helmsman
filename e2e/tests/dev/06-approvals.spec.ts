import { expect, test, type Browser, type BrowserContext, type Locator, type Page } from "@playwright/test";
import { authFile, clearActive, fakeScenario, waitIdle } from "../support";

// Production migrations need a second person. Bob asks, erin decides, alice reads the audit log.
test.describe.configure({ mode: "serial" });

const baseURL = () => test.info().project.use.baseURL;
async function session(browser: Browser, user: string): Promise<{ ctx: BrowserContext; page: Page }> {
  const ctx = await browser.newContext({ storageState: authFile(user), baseURL: baseURL() });
  return { ctx, page: await ctx.newPage() };
}

type Run = { id: string; row: Locator; toggle: Locator };
const runAt = (page: Page, id: string): Run => {
  const row = page.locator(`[data-run-id="${id}"]`);
  return { id, row, toggle: row.locator("button[aria-expanded]") };
};

async function openProdRuns(page: Page) {
  await page.goto("/apps/demo-app");
  await page.getByRole("tab", { name: /prod/ }).click();
  await page.getByRole("button", { name: "runs", exact: true }).click();
  await expect(page.getByRole("button", { name: "Run migrations" })).toBeEnabled();
}

async function requestProdMigration(page: Page): Promise<Run> {
  await openProdRuns(page);
  const answer = page.waitForResponse((r) => /\/actions\/migrate$/.test(r.url()) && r.request().method() === "POST");
  await page.getByRole("button", { name: "Run migrations" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Nothing runs until a second person approves it.")).toBeVisible();
  const submit = dialog.getByRole("button", { name: "Request approval" });
  await expect(submit).toBeDisabled();
  await dialog.getByPlaceholder("prod").fill("prod");
  await submit.click();
  const body = (await (await answer).json()) as { run: { id: string } };
  await expect(page.getByRole("dialog")).toBeHidden();
  const run = runAt(page, body.run.id);
  await expect(run.row).toBeVisible();
  return run;
}

test.beforeEach(async ({ playwright }) => {
  const ctx = await playwright.request.newContext({ baseURL: baseURL(), storageState: authFile("alice") });
  await clearActive(ctx, "demo-app", "prod");
  await waitIdle(ctx, "", "demo-app", "prod");
  await fakeScenario(ctx);
  await ctx.dispose();
});

test("bob requests, erin approves, and the run completes", async ({ browser }) => {
  const bob = await session(browser, "bob");
  const run = await requestProdMigration(bob.page);
  await expect(run.toggle).toContainText("needs approval");
  // The requester can withdraw but not approve.
  await expect(run.row.getByRole("button", { name: "Approve" })).toHaveCount(0);
  await expect(run.row.getByRole("button", { name: "Withdraw" })).toBeVisible();

  const erin = await session(browser, "erin");
  await erin.page.goto("/");
  await expect(erin.page.getByRole("link", { name: /Approvals/ })).toBeVisible();
  await erin.page.getByRole("link", { name: /Approvals/ }).click();
  const card = erin.page.locator("div").filter({ hasText: /demo-app\/prod/ }).filter({ hasText: "requested by bob" }).last();
  await expect(card).toContainText("migrate");
  await card.getByRole("button", { name: "Approve" }).click();
  await expect(erin.page.getByText("Nothing is waiting for approval.")).toBeVisible();

  await openProdRuns(bob.page);
  const again = runAt(bob.page, run.id);
  await expect(again.toggle).toContainText("succeeded", { timeout: 40_000 });
  await expect(again.row.getByText(/bob approved by erin/)).toBeVisible();
  await bob.ctx.close();
  await erin.ctx.close();
});

test("the audit log has every step of that production run", async ({ browser }) => {
  const alice = await session(browser, "alice");
  const res = await alice.page.request.get("/api/admin/audit?limit=200");
  const rows = (await res.json()) as { action: string; target: string; actor_id: string; result: string }[];
  const mine = rows.filter((r) => r.target === "demo-app/prod" && r.action.startsWith("action.run."));
  for (const want of ["action.run.requested", "action.run.approved", "action.run.dispatched", "action.run.completed"]) {
    expect(mine.map((r) => r.action), want).toContain(want);
  }
  expect(mine.find((r) => r.action === "action.run.requested")?.actor_id).toBe("bob");
  expect(mine.find((r) => r.action === "action.run.approved")?.actor_id).toBe("erin");
  await alice.page.goto("/admin/audit");
  await expect(alice.page.getByRole("row").filter({ hasText: "action.run.approved" }).filter({ hasText: "erin" }).first()).toBeVisible();
  await alice.ctx.close();
});

test("erin can reject a request and nothing runs", async ({ browser }) => {
  const bob = await session(browser, "bob");
  const run = await requestProdMigration(bob.page);
  await expect(run.toggle).toContainText("needs approval");
  const erin = await session(browser, "erin");
  await erin.page.goto("/approvals");
  await erin.page.getByRole("button", { name: "Reject" }).first().click();
  await openProdRuns(bob.page);
  await expect(runAt(bob.page, run.id).toggle).toContainText("rejected");
  await bob.ctx.close();
  await erin.ctx.close();
});

test("a requester can withdraw their own request", async ({ browser }) => {
  const bob = await session(browser, "bob");
  const run = await requestProdMigration(bob.page);
  await run.row.getByRole("button", { name: "Withdraw" }).click();
  await expect(run.toggle).toContainText("rejected");
  await bob.ctx.close();
});

test("nobody can approve their own production request, not even an admin", async ({ browser }) => {
  const alice = await session(browser, "alice");
  const run = await requestProdMigration(alice.page);
  await expect(run.toggle).toContainText("needs approval");
  await expect(run.row.getByRole("button", { name: "Approve" })).toHaveCount(0);

  // Straight to the API, as a script would.
  const csrf = ((await (await alice.page.request.get("/api/me")).json()) as { csrf_token: string }).csrf_token;
  const res = await alice.page.request.post(`/api/apps/demo-app/envs/prod/runs/${run.id}/approve`, { headers: { "X-CSRF-Token": csrf }, data: {} });
  expect(res.status()).toBe(403);
  expect(((await res.json()) as { error: string }).error).toContain("own request");
  await run.row.getByRole("button", { name: "Withdraw" }).click();
  await expect(run.toggle).toContainText("rejected");
  await alice.ctx.close();
});
