import { expect, test } from "@playwright/test";
import { authFile } from "../support";

test.describe("bob restarts a container", () => {
  test.use({ storageState: authFile("bob") });

  test("staging restarts after one confirmation and the event shows up", async ({ page }) => {
    await page.goto("/apps/demo-app");
    await expect(page.getByText("online", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Restart container" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("heading", { name: "Restart demo-app/staging?" })).toBeVisible();
    await dialog.getByRole("button", { name: "Restart container" }).click();
    await expect(page.getByText("Restart requested.")).toBeVisible();
    await page.getByRole("button", { name: /^events/ }).click();
    await expect(page.getByRole("cell", { name: /restart|start/ }).first()).toBeVisible();
  });

  test("production needs the environment name typed before it is enabled", async ({ page }) => {
    await page.goto("/apps/demo-app");
    await page.getByRole("tab", { name: /prod/ }).click();
    await page.getByRole("button", { name: "Restart container" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("This is a production environment")).toBeVisible();
    const confirm = dialog.getByRole("button", { name: "Restart container" });
    await expect(confirm).toBeDisabled();
    await dialog.getByPlaceholder("prod").fill("pro");
    await expect(confirm).toBeDisabled();
    await dialog.getByPlaceholder("prod").fill("prod");
    await expect(confirm).toBeEnabled();
    await confirm.click();
    await expect(page.getByText("Restart requested.")).toBeVisible();
  });

  test("cancelling the dialog restarts nothing", async ({ page }) => {
    await page.goto("/apps/demo-app");
    await page.getByRole("button", { name: "Restart container" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByRole("dialog")).toBeHidden();
    await expect(page.getByText("Restart requested.")).toHaveCount(0);
  });
});

test.describe("restarts are audited twice", () => {
  test.use({ storageState: authFile("alice") });

  test("the audit log has the request and the result, attributed to bob", async ({ page }) => {
    await page.goto("/admin/audit");
    await expect(page.getByRole("heading", { name: "Audit Log" })).toBeVisible();
    const rows = page.getByRole("row");
    await expect(rows.filter({ hasText: "action.restart.requested" }).filter({ hasText: "bob" }).first()).toBeVisible();
    const result = rows.filter({ hasText: /action\.restart(?!\.)/ }).filter({ hasText: "bob" }).filter({ hasText: "demo-app/staging" }).first();
    await expect(result).toContainText("demo-app/staging");
    await expect(result).toContainText("ok");
  });
});
