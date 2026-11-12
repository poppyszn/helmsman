import { expect, test } from "@playwright/test";
import { authFile } from "../support";

test.describe("alice (admin)", () => {
  test.use({ storageState: authFile("alice") });

  test("the app page has quick links: the repository, the app's and the environment's", async ({ page }) => {
    await page.goto("/apps/demo-app");
    const links = page.getByLabel("Quick links");
    await expect(links.getByRole("link", { name: "Repository" })).toHaveAttribute("href", "https://github.com/acme-org/demo-app");
    await expect(links.getByRole("link", { name: "Runbook" })).toHaveAttribute("href", "https://wiki.example.com/demo-app");
    await expect(links.getByRole("link", { name: "Dashboards" })).toBeVisible();
    // The staging-only link is not shown on prod.
    await page.getByRole("tab", { name: "prod" }).click();
    await expect(links.getByRole("link", { name: "Runbook" })).toBeVisible();
    await expect(links.getByRole("link", { name: "Dashboards" })).toHaveCount(0);
  });

  test("the breadcrumbs at the top are links back up the path", async ({ page }) => {
    await page.goto("/apps/demo-app");
    const crumbs = page.getByRole("navigation", { name: "Breadcrumb" });
    await expect(crumbs.getByText("demo-app")).toHaveAttribute("aria-current", "page");
    await crumbs.getByRole("link", { name: "apps" }).click();
    await expect(page).toHaveURL(/\/$/);
    await page.goto("/apps/demo-app");
    await crumbs.getByRole("link", { name: "helmsman" }).click();
    await expect(page).toHaveURL(/\/$/);
    // A step without a page of its own is not a link.
    await page.goto("/admin/integrations");
    await expect(crumbs.getByRole("link", { name: "admin" })).toHaveCount(0);
    await expect(crumbs.getByText("integrations")).toHaveAttribute("aria-current", "page");
  });

  test("the dashboard lists the app with both environments and their status", async ({ page }) => {
    await page.goto("/");
    const card = page.getByRole("link", { name: /Demo App/ });
    await expect(card).toContainText("acme-org/demo-app");
    await expect(card).toContainText("staging");
    await expect(card).toContainText("prod");
  });

  test("the filter narrows the list and says when nothing matches", async ({ page }) => {
    await page.goto("/");
    await page.getByPlaceholder(/Search apps/).fill("zzz-no-such-app");
    await expect(page.getByText("no matching apps")).toBeVisible();
    await page.getByPlaceholder(/Search apps/).fill("demo");
    await expect(page.getByRole("heading", { name: "Demo App" })).toBeVisible();
  });

  test("an environment shows status, container, revision and agent facts", async ({ page }) => {
    await page.goto("/apps/demo-app");
    await expect(page.getByRole("tab", { name: /staging/ })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByText("healthy", { exact: true })).toBeVisible();
    await expect(page.getByText("Health probe")).toBeVisible();
    await expect(page.getByText("Container", { exact: true })).toBeVisible();
    await expect(page.getByText(/^running$/i)).toBeVisible();
    await expect(page.getByText("Host agent")).toBeVisible();
    await expect(page.getByText("online", { exact: true })).toBeVisible();
  });

  test("switching to prod shows that environment", async ({ page }) => {
    await page.goto("/apps/demo-app");
    await page.getByRole("tab", { name: /prod/ }).click();
    await expect(page.getByRole("tab", { name: /prod/ })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByText(/^running$/i)).toBeVisible();
  });
});

test.describe("carol (viewer on demo-app staging only)", () => {
  test.use({ storageState: authFile("carol") });

  test("sees staging but not prod, and no admin navigation", async ({ page }) => {
    await page.goto("/apps/demo-app");
    await expect(page.getByRole("tab", { name: /staging/ })).toBeVisible();
    await expect(page.getByRole("tab", { name: /prod/ })).toHaveCount(0);
    for (const item of ["Users", "Access", "Catalog", "Agents", "Audit log", "GitHub", "Approvals", "Integrations"]) {
      await expect(page.getByRole("link", { name: item, exact: true })).toHaveCount(0);
    }
  });

  test("cannot restart or tail logs, and is told why", async ({ page }) => {
    await page.goto("/apps/demo-app");
    await expect(page.getByRole("button", { name: "Restart container" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "tail logs" })).toBeDisabled();
    await expect(page.getByText("You do not have permission to view logs")).toBeVisible();
  });

  test("the admin API refuses her", async ({ page }) => {
    for (const path of ["/api/admin/users", "/api/admin/audit", "/api/admin/agents", "/api/admin/github"]) {
      const res = await page.request.get(path);
      expect(res.status(), path).toBe(403);
    }
  });
});

test.describe("bob (developer on non-prod, operator on demo-app prod)", () => {
  test.use({ storageState: authFile("bob") });

  test("is not an admin", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("link", { name: "Users", exact: true })).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Approvals", exact: true })).toHaveCount(0);
  });
});
