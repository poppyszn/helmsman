import { expect, test } from "@playwright/test";
import { loginAs } from "../support";

test.describe("sign in and out", () => {
  test("a visitor with no session is sent to the sign-in page", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByRole("link", { name: "Sign in with GitHub" })).toBeVisible();
  });

  test("alice signs in with GitHub and lands on the applications", async ({ page }) => {
    await loginAs(page, "alice");
    await expect(page.getByRole("heading", { name: "Applications" })).toBeVisible();
    await expect(page.getByText("alice", { exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Demo App" })).toBeVisible();
  });

  test("a user outside the allowed organizations is refused with a clear message", async ({ page }) => {
    await loginAs(page, "dave");
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByRole("alert")).toContainText("not in an organization");
  });

  test("an organization that restricts OAuth apps is named in the message", async ({ page }) => {
    await loginAs(page, "frank");
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByRole("alert")).toContainText("acme-org");
    await expect(page.getByRole("alert")).toContainText("restricts third-party OAuth apps");
  });

  test("signing out ends the session", async ({ page }) => {
    await loginAs(page, "carol");
    await expect(page.getByRole("heading", { name: "Applications" })).toBeVisible();
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/login/);
    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
    const me = await page.request.get("/api/me");
    expect(me.status()).toBe(401);
  });
});
