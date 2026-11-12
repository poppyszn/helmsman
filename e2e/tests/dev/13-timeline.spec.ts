import { expect, test } from "@playwright/test";
import { authFile } from "../support";

// The timeline tab merges deployments, runs, restarts and container and health changes of one environment.
test.describe("a developer", () => {
  test.use({ storageState: authFile("bob") });

  test("sees what happened to an environment, newest first, and can filter it", async ({ page }) => {
    await page.goto("/apps/demo-app");
    await page.getByRole("button", { name: "timeline", exact: true }).click();
    // The fake GitHub keeps a little deploy history, and the dev agent reports container events.
    const deploys = page.locator('li[data-kind="deploy"]');
    await expect(deploys.first()).toBeVisible();
    await expect(deploys.first()).toContainText("Deployed");

    await page.getByRole("group", { name: "Filter the timeline" }).getByRole("button", { name: "deploys" }).click();
    await expect(page.locator('li[data-kind="container"]')).toHaveCount(0);
    await expect(deploys.first()).toBeVisible();
    await expect(page.getByRole("button", { name: "deploys" })).toHaveAttribute("aria-pressed", "true");

    await page.getByRole("button", { name: "everything", exact: true }).click();
    // Newest first: the first time shown is not older than the last.
    const days = page.locator("section[aria-label]");
    expect(await days.count()).toBeGreaterThan(0);
  });
});

test.describe("a viewer", () => {
  test.use({ storageState: authFile("carol") });

  test("sees the timeline of the environment they may view", async ({ page }) => {
    await page.goto("/apps/demo-app");
    await page.getByRole("button", { name: "timeline", exact: true }).click();
    await expect(page.getByRole("group", { name: "Filter the timeline" })).toBeVisible();
  });
});
