import { expect, test } from "@playwright/test";
import { authFile, loginAs } from "../support";

// Someone who signs in without a role is told why, appears for an admin to grant access, and gets in once
// granted. grace is in the allowed organization and has no role bound.
test.describe.configure({ mode: "serial" });

test("a newcomer is told an admin will grant access, and can switch account while the request stays", async ({ browser }) => {
  const g = await browser.newContext();
  const gp = await g.newPage();
  await loginAs(gp, "grace");
  await expect(gp.getByRole("heading", { name: "You don't have access yet" })).toBeVisible();
  await expect(gp.getByText("@grace")).toBeVisible();
  await expect(gp.getByText("an admin will grant you access")).toBeVisible();
  // There is no way into the app from here.
  await expect(gp.getByRole("link", { name: "Apps", exact: true })).toHaveCount(0);

  // Switching account signs out and asks GitHub for its account picker.
  await gp.getByRole("button", { name: "Switch account" }).click();
  await gp.waitForURL(/prompt=select_account/);

  // The request is still there for the admin, even though grace is signed out.
  const a = await browser.newContext({ storageState: authFile("alice") });
  const ap = await a.newPage();
  await ap.goto("/admin/bindings");
  await expect(ap.getByRole("region", { name: "Awaiting access" }).getByLabel("Request from grace")).toBeVisible();
  await a.close();
  await g.close();
});

test("an admin grants a role from the request, and the person gets in", async ({ browser }) => {
  const g = await browser.newContext();
  const gp = await g.newPage();
  await loginAs(gp, "grace");
  await expect(gp.getByRole("heading", { name: "You don't have access yet" })).toBeVisible();

  const a = await browser.newContext({ storageState: authFile("alice") });
  const ap = await a.newPage();
  try {
    await ap.goto("/admin/bindings");
    const req = ap.getByLabel("Request from grace");
    await expect(req).toBeVisible();
    await req.getByRole("button", { name: "Grant access" }).click();
    const form = ap.getByRole("form", { name: "Grant access to grace" });
    await form.getByLabel("Role", { exact: true }).selectOption({ label: "viewer" });
    await expect(form.getByLabel("Permissions of this role")).toContainText("app.view");
    await form.getByLabel("Apps", { exact: true }).selectOption("demo-app");
    await form.getByLabel("Environments", { exact: true }).selectOption("staging");
    await form.getByRole("button", { name: "Grant access" }).click();
    await expect(ap.getByLabel("Request from grace")).toHaveCount(0);

    // grace is let in. The page checks again by itself; reloading is just quicker.
    await gp.reload();
    await expect(gp.getByRole("heading", { name: "Applications" })).toBeVisible();
    await expect(gp.getByRole("link", { name: /Demo App/ })).toBeVisible();
  } finally {
    // Leave things as they were: take the binding away again.
    const me = (await (await ap.request.get("/api/me")).json()) as { csrf_token: string };
    const bindings = (await (await ap.request.get("/api/admin/bindings")).json()) as { id: number; subject_name: string }[];
    for (const b of bindings.filter((x) => x.subject_name.includes("grace"))) {
      await ap.request.delete(`/api/admin/bindings/${b.id}`, { headers: { "X-CSRF-Token": me.csrf_token } });
    }
    await a.close();
    await g.close();
  }
});
