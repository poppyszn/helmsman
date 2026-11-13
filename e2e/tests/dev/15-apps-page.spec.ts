import { expect, test } from "@playwright/test";
import { authFile } from "../support";

// The Apps page: switch organization, filter by tag, and keep external tools in one place.
test.describe.configure({ mode: "serial" });

test.describe("an admin", () => {
  test.use({ storageState: authFile("alice") });

  test("switches between organizations, and the choice is in the address", async ({ page }) => {
    await page.goto("/");
    const org = page.getByLabel("Organization");
    await expect(org).toBeVisible();
    await expect(page.getByRole("link", { name: /Demo App/ })).toBeVisible();
    await expect(page.getByRole("link", { name: /Ledger Demo/ })).toBeVisible();
    await org.selectOption("other-org");
    await expect(page).toHaveURL(/org=other-org/);
    await expect(page.getByRole("link", { name: /Ledger Demo/ })).toBeVisible();
    await expect(page.getByRole("link", { name: /Demo App/ })).toHaveCount(0);
    // Reloading keeps the choice, and "All organizations" shows everything again.
    await page.reload();
    await expect(page.getByLabel("Organization")).toHaveValue("other-org");
    await page.getByLabel("Organization").selectOption("");
    await expect(page.getByRole("link", { name: /Demo App/ })).toBeVisible();
  });

  test("groups apps by repository, and a group can be collapsed", async ({ page }) => {
    await page.goto("/");
    // demo-app and static-demo share a repository, so they sit under one heading; ledger-demo has its own.
    const group = page.getByRole("region", { name: "Repository acme-org/demo-app" });
    await expect(group.getByRole("link", { name: /Demo App/ })).toBeVisible();
    await expect(group.getByRole("link", { name: /Static Demo/ })).toBeVisible();
    await expect(group.getByRole("link", { name: /Ledger Demo/ })).toHaveCount(0);
    await expect(page.getByRole("region", { name: "Repository other-org/ledger-demo" }).getByRole("link", { name: /Ledger Demo/ })).toBeVisible();

    // Collapsing hides the apps, and the choice survives a reload.
    await group.getByRole("button", { name: /demo-app/ }).click();
    await expect(group.getByRole("link")).toHaveCount(0);
    await page.reload();
    await expect(page.getByRole("region", { name: "Repository acme-org/demo-app" }).getByRole("link")).toHaveCount(0);
    // Searching opens it again so a match is never hidden.
    await page.getByPlaceholder("Search apps or repos...").fill("static");
    await expect(page.getByRole("link", { name: /Static Demo/ })).toBeVisible();
    await page.getByPlaceholder("Search apps or repos...").fill("");
    await page.getByRole("region", { name: "Repository acme-org/demo-app" }).getByRole("button", { name: /demo-app/ }).click();
    await expect(page.getByRole("link", { name: /Demo App/ })).toBeVisible();
  });

  test("switches to another app of the same repository without going back to the list", async ({ page }) => {
    await page.goto("/apps/demo-app/staging/logs");
    await page.getByRole("heading").getByRole("button").click();
    const menu = page.getByRole("navigation", { name: "Apps in this repository" });
    // Only the apps of this repository are offered, and the current one is marked.
    await expect(menu.getByRole("link")).toHaveCount(2);
    await expect(menu.getByRole("link", { name: /Demo App/ })).toHaveAttribute("aria-current", "page");
    await expect(menu.getByRole("link", { name: /Ledger Demo/ })).toHaveCount(0);
    // The environment and the tab carry over where the other app has them.
    await menu.getByRole("link", { name: /Static Demo/ }).click();
    await expect(page).toHaveURL(/\/apps\/static-demo\/staging\/logs$/);
    await expect(page.getByRole("heading", { name: "Static Demo" })).toBeVisible();

    // An app of its own repository has no switcher.
    await page.goto("/apps/ledger-demo");
    await expect(page.getByRole("heading", { name: "Ledger Demo" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Ledger Demo" }).getByRole("button")).toHaveCount(0);
  });

  test("when the other app lacks the environment, the switch lands on its first one", async ({ page }) => {
    await page.goto("/apps/demo-app/prod");
    await page.getByRole("heading").getByRole("button").click();
    await page.getByRole("navigation", { name: "Apps in this repository" }).getByRole("link", { name: /Static Demo/ }).click();
    await expect(page).toHaveURL(/\/apps\/static-demo$/);
    await expect(page.getByRole("heading", { name: "Static Demo" })).toBeVisible();
  });

  test("filters by tag, including environment names, and clears", async ({ page }) => {
    await page.goto("/");
    // The tags live in a dropdown that stays open while several are picked.
    await page.getByRole("button", { name: /^Tags/ }).click();
    const tags = page.getByRole("group", { name: "Filter by tag" });
    await tags.getByRole("button", { name: /^ui/ }).click();
    await expect(page).toHaveURL(/tag=ui/);
    await expect(page.getByRole("link", { name: /Static Demo/ })).toBeVisible();
    await expect(page.getByRole("link", { name: /Demo App/ })).toHaveCount(0);
    // Two tags mean both: nothing is both ui and finance.
    await tags.getByRole("button", { name: /^finance/ }).click();
    await expect(page.getByText("no matching apps")).toBeVisible();
    await page.getByRole("button", { name: "clear" }).click();
    await page.getByRole("button", { name: /^Tags/ }).click();
    // An environment name works as a tag: only demo-app has a prod environment.
    await tags.getByRole("button", { name: /^prod/ }).click();
    await expect(page.getByRole("link", { name: /Demo App/ })).toBeVisible();
    await expect(page.getByRole("link", { name: /Static Demo/ })).toHaveCount(0);
  });

  test("adds a tool, everyone sees it, and it can be edited and removed", async ({ page, browser }) => {
    const name = `Grafana ${Date.now().toString(36)}`;
    await page.goto("/");
    await page.getByRole("button", { name: "Add tool" }).click();
    const form = page.getByRole("form", { name: "New tool" });
    await form.getByLabel("Name", { exact: true }).fill(name);
    await form.getByLabel("Address", { exact: true }).fill("javascript:alert(1)");
    await form.getByRole("button", { name: "Add tool" }).click();
    await expect(form.getByRole("alert")).toContainText("http://");
    await form.getByLabel("Address", { exact: true }).fill("https://grafana.example.com");
    await form.getByLabel(/^Description/).fill("Dashboards and alerts");
    await form.getByLabel(/^Tags/).fill("monitoring, ui");
    await form.getByRole("button", { name: "Add tool" }).click();

    const tool = page.getByLabel(`Tool ${name}`);
    await expect(tool).toBeVisible();
    await expect(tool.getByRole("link")).toHaveAttribute("href", "https://grafana.example.com");
    await expect(tool.getByRole("link")).toHaveAttribute("rel", /noopener/);
    await expect(tool).toContainText("Dashboards and alerts");

    // A developer sees it, without any way to change it.
    const b = await browser.newContext({ storageState: authFile("bob") });
    const bp = await b.newPage();
    await bp.goto("/");
    await expect(bp.getByLabel(`Tool ${name}`)).toBeVisible();
    await expect(bp.getByRole("button", { name: "Add tool" })).toHaveCount(0);
    await expect(bp.getByRole("button", { name: `Edit ${name}` })).toHaveCount(0);
    // The tool's tags take part in the tag filter.
    await bp.getByRole("button", { name: /^Tags/ }).click();
    await bp.getByRole("group", { name: "Filter by tag" }).getByRole("button", { name: /^ui/ }).click();
    await expect(bp.getByLabel(`Tool ${name}`)).toBeVisible();
    await b.close();

    await tool.hover();
    await page.getByRole("button", { name: `Edit ${name}` }).click();
    await page.getByRole("form", { name: "Edit tool" }).getByLabel(/^Description/).fill("Edited");
    await page.getByRole("form", { name: "Edit tool" }).getByRole("button", { name: "Save changes" }).click();
    await expect(page.getByLabel(`Tool ${name}`)).toContainText("Edited");

    await page.getByLabel(`Tool ${name}`).hover();
    await page.getByRole("button", { name: `Delete ${name}` }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Delete tool" }).click();
    await expect(page.getByLabel(`Tool ${name}`)).toHaveCount(0);
  });
});
