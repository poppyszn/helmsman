import { expect, test } from "@playwright/test";
import { authFile } from "../support";

// The setup check page: server-side checks plus a WebSocket test that runs in the browser.
test.describe("an admin", () => {
  test.use({ storageState: authFile("alice") });

  test("sees the checks, and the live-connection test passes through the dev proxy", async ({ page }) => {
    await page.goto("/admin/doctor");
    await expect(page.getByRole("heading", { name: "Setup check" })).toBeVisible();
    await expect(page.getByLabel("Server and HTTPS")).toBeVisible();
    await expect(page.getByLabel("Sign-in")).toBeVisible();
    await expect(page.getByLabel("Catalog, agents and apps")).toBeVisible();
    // The audit log protection and the limited database role are verified.
    await expect(page.getByLabel("Database and security").getByText("Audit log protection")).toBeVisible();
    const ws = page.getByRole("listitem").filter({ hasText: "Live connections (WebSocket)" });
    await expect(ws).toHaveAttribute("data-status", "ok");
    // The dev agent is online and its containers are found.
    await expect(page.getByRole("listitem").filter({ hasText: "Agent dev-agent" })).toHaveAttribute("data-status", "ok");
    await page.getByRole("button", { name: "Check again" }).click();
    await expect(page.getByRole("heading", { name: "Setup check" })).toBeVisible();
  });
});

test.describe("a developer", () => {
  test.use({ storageState: authFile("bob") });

  test("has no Setup check and the API refuses", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("link", { name: "Setup check", exact: true })).toHaveCount(0);
    expect((await page.request.get("/api/admin/doctor")).status()).toBe(403);
  });
});
