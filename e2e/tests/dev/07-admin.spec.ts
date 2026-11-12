import { expect, test } from "@playwright/test";
import { authFile } from "../support";

test.use({ storageState: authFile("alice") });

test("users lists the people who have signed in", async ({ page }) => {
  await page.goto("/admin/users");
  await expect(page.getByRole("heading", { name: "Users" })).toBeVisible();
  for (const login of ["alice", "bob", "carol", "erin"]) await expect(page.getByText(login, { exact: true }).first()).toBeVisible();
});

test("access shows the role bindings, including the team binding", async ({ page }) => {
  await page.goto("/admin/bindings");
  await expect(page.getByRole("heading", { name: "Access & Roles" })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "acme-org/leads" }).first()).toContainText("approver");
});

test("the catalog page reloads the catalog", async ({ page }) => {
  await page.goto("/admin/catalog");
  await expect(page.getByRole("heading", { name: "Catalog" })).toBeVisible();
  await page.getByRole("button", { name: "Reload now" }).click();
  await expect(page.getByRole("button", { name: "Reload now" })).toBeEnabled();
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("the agents page shows the dev agent online with no alert", async ({ page }) => {
  await page.goto("/admin/agents");
  await expect(page.getByRole("heading", { name: "Host Agents" })).toBeVisible();
  const row = page.getByRole("row").filter({ hasText: "dev-agent" });
  await expect(row).toContainText("online");
  await expect(row.getByRole("alert")).toHaveCount(0);
});

test("a new agent gets a one-time join token, and can be revoked", async ({ page }) => {
  const id = `e2e-${Date.now().toString(36)}`;
  await page.goto("/admin/agents");
  await page.getByPlaceholder("e.g. billing-host-1").fill(id);
  await page.getByPlaceholder("Billing staging server").fill("End to end agent");
  await page.getByRole("button", { name: "Create agent" }).click();
  await expect(page.getByText(/join token/i).first()).toBeVisible();
  // The install files are ready to copy: the agent's address, the token, and the compose file.
  await expect(page.getByText("compose.agent.yml", { exact: true })).toBeVisible();
  await expect(page.getByText("idp-agent:")).toBeVisible();
  await expect(page.getByText(/IDP_JOIN_TOKEN=[A-Za-z0-9_-]{30,}/)).toBeVisible();
  await expect(page.getByText(/idp_url: wss?:\/\/.*\/agent\/ws/)).toBeVisible();
  const row = page.getByRole("row").filter({ hasText: id });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: /revoke/i }).click();
  await page.getByRole("dialog").getByRole("button", { name: /revoke/i }).click();
  await expect(page.getByRole("row").filter({ hasText: id })).toContainText(/revoked/i);
});

test("an agent lists what runs on its host and drafts the files to add a service", async ({ page }) => {
  await page.goto("/admin/agents");
  await page.getByRole("row").filter({ hasText: "dev-agent" }).getByRole("button", { name: "Containers" }).click();
  const panel = page.getByLabel("Containers on dev-agent");
  // demo-app is exposed and in the catalog. Another service of the dev stack is only discovered.
  const demo = panel.getByRole("listitem").filter({ hasText: "helmsman-dev/demo-app" }).first();
  await expect(demo).toContainText("in the catalog");
  const other = panel.getByRole("listitem").filter({ hasText: "found, not exposed" }).first();
  await expect(other).toBeVisible();
  await other.getByRole("checkbox").check();
  await panel.getByPlaceholder("my-org/my-app").fill("my-org/my-app");
  await expect(panel.getByText("1. On the host: add to agent.yaml")).toBeVisible();
  await expect(panel.getByText(/compose_project: helmsman-dev\n\s+services: \[/)).toBeVisible();
  await expect(panel.getByText(/repo: my-org\/my-app/)).toBeVisible();
  await expect(panel.getByText(/agent: dev-agent/)).toBeVisible();
});

test("the GitHub page shows the repository as installed with actions enabled", async ({ page }) => {
  await page.goto("/admin/github");
  await expect(page.getByRole("heading", { name: "GitHub" })).toBeVisible();
  const row = page.getByRole("row").filter({ hasText: "acme-org/demo-app" });
  await expect(row).toContainText("enabled");
  await expect(row.getByLabel("yes").first()).toBeVisible();
  await page.getByRole("button", { name: "Check now" }).click();
  await expect(row).toContainText("enabled");
});

test("the audit log records sign-ins with who, what and when", async ({ page }) => {
  await page.goto("/admin/audit");
  await expect(page.getByRole("heading", { name: "Audit Log" })).toBeVisible();
  const login = page.getByRole("row").filter({ hasText: "auth.login" }).first();
  await expect(login).toContainText("ok");
  await expect(page.getByRole("columnheader", { name: /Timestamp/ })).toBeVisible();
});

test("the command palette opens from the keyboard and jumps to an app", async ({ page }) => {
  await page.goto("/");
  await page.keyboard.press("ControlOrMeta+k");
  const palette = page.getByRole("dialog");
  await expect(palette).toBeVisible();
  const box = palette.getByPlaceholder("Type a command or app...");
  await box.fill("demo");
  await expect(palette.getByText("Demo App").first()).toBeVisible();
  await box.press("Enter");
  await expect(page).toHaveURL(/\/apps\/demo-app/);
});

test("the sidebar can be collapsed and expanded", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Collapse sidebar" }).click();
  await expect(page.getByRole("button", { name: "Expand sidebar" })).toBeVisible();
  await page.getByRole("button", { name: "Expand sidebar" }).click();
  await expect(page.getByRole("button", { name: "Collapse sidebar" })).toBeVisible();
});
