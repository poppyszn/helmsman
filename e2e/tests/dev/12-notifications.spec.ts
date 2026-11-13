import { createHmac } from "node:crypto";
import { createServer, type IncomingMessage } from "node:http";
import { networkInterfaces } from "node:os";
import { expect, test } from "@playwright/test";
import { authFile } from "../support";

// Notifications: an admin adds a webhook channel, sends a test, and the receiver gets a signed request.
// The receiver runs in this test and the Helmsman server reaches it over the dev network.
test.describe.configure({ mode: "serial" });

function myAddress(): string {
  for (const list of Object.values(networkInterfaces())) {
    for (const n of list ?? []) if (n.family === "IPv4" && !n.internal) return n.address;
  }
  throw new Error("no network address");
}

test.describe("an admin", () => {
  test.use({ storageState: authFile("alice") });

  test("adds a webhook, gets its secret once, and a test arrives signed", async ({ page }) => {
    const got: { headers: IncomingMessage["headers"]; body: string }[] = [];
    const srv = createServer((req, res) => {
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        got.push({ headers: req.headers, body });
        res.statusCode = 200;
        res.end("ok");
      });
    });
    await new Promise<void>((r) => srv.listen(0, "0.0.0.0", r));
    const port = (srv.address() as { port: number }).port;
    const name = `e2e hook ${Date.now().toString(36)}`;
    try {
      await page.goto("/admin/notifications");
      await expect(page.getByRole("heading", { name: "Notifications" })).toBeVisible();
      await page.getByRole("button", { name: "Add channel" }).click();
      const form = page.getByRole("form", { name: "New channel" });
      await form.getByLabel("Type").selectOption("webhook");
      await form.getByLabel("Name", { exact: true }).fill(name);
      await form.getByPlaceholder("https://hooks.example.com/helmsman").fill(`http://${myAddress()}:${port}/hook`);
      await form.getByRole("button", { name: "Add channel" }).click();

      // The signing secret is shown once.
      const secretBox = page.getByLabel("Signing secret");
      await expect(secretBox).toBeVisible();
      const secret = (await secretBox.locator("pre").innerText()).trim();
      expect(secret.length).toBeGreaterThan(30);
      await page.getByRole("button", { name: "I have saved it" }).click();
      await expect(page.getByLabel("Signing secret")).toHaveCount(0);
      await page.reload();
      await expect(page.getByText(secret)).toHaveCount(0);

      const row = page.getByLabel(`Channel ${name}`);
      await expect(row).toBeVisible();
      await row.getByRole("button", { name: "Send test" }).click();
      await expect(row.getByText("The test was delivered.")).toBeVisible();

      expect(got).toHaveLength(1);
      const { headers, body } = got[0];
      expect(headers["x-helmsman-event"]).toBe("test");
      const ts = String(headers["x-helmsman-timestamp"]);
      expect(headers["x-helmsman-signature"]).toBe("sha256=" + createHmac("sha256", secret).update(`${ts}.${body}`).digest("hex"));
      expect(JSON.parse(body).title).toBe("Test notification from Helmsman");
    } finally {
      srv.close();
    }
    // Clean up.
    const row = page.getByLabel(`Channel ${name}`);
    await row.getByRole("button", { name: "Delete" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Delete channel" }).click();
    await expect(page.getByLabel(`Channel ${name}`)).toHaveCount(0);
  });

  test("a channel can be limited to chosen apps and environments, and its events are grouped", async ({ page }) => {
    const name = `e2e scoped ${Date.now().toString(36)}`;
    await page.goto("/admin/notifications");
    await page.getByRole("button", { name: "Add channel" }).click();
    const form = page.getByRole("form", { name: "New channel" });
    await form.getByLabel("Type").selectOption("webhook");
    await form.getByLabel("Name", { exact: true }).fill(name);
    await form.getByPlaceholder("https://hooks.example.com/helmsman").fill("http://10.255.255.1:9/hook");
    // Events are grouped, and workflow runs are among them.
    const events = form.getByRole("group", { name: "Events" });
    for (const g of ["Uptime", "Deployments", "Migrations and seeds", "Other GitHub workflows"]) await expect(events.getByText(g, { exact: true })).toBeVisible();
    await events.getByLabel("A deployment started").check();
    await events.getByLabel("Another workflow of the app's repository failed (tests, builds, and so on)").check();
    // Limit it to one app and one environment name.
    await form.getByRole("group", { name: "Which apps" }).getByLabel("Only these").check();
    await form.getByRole("group", { name: "Which apps" }).getByLabel("Demo App").check();
    await form.getByRole("group", { name: "Which environments" }).getByLabel("Only these names").check();
    await form.getByRole("group", { name: "Which environments" }).getByLabel("staging", { exact: true }).check();
    await form.getByRole("button", { name: "Add channel" }).click();
    await page.getByRole("button", { name: "I have saved it" }).click();

    const row = page.getByLabel(`Channel ${name}`);
    await expect(row).toContainText("apps: demo-app");
    await expect(row).toContainText(/environments: staging(?!,)/);
    await expect(row).toContainText("A deployment started");

    // Editing shows the saved scope, and switching back to everything clears it.
    await row.getByRole("button", { name: "Edit" }).click();
    const edit = page.getByRole("form", { name: "Edit channel" });
    await expect(edit.getByRole("group", { name: "Which apps" }).getByLabel("Only these")).toBeChecked();
    await edit.getByRole("group", { name: "Which apps" }).getByLabel("All apps").check();
    await edit.getByRole("group", { name: "Which environments" }).getByLabel("All environments").check();
    await edit.getByRole("button", { name: "Save changes" }).click();
    await expect(page.getByLabel(`Channel ${name}`)).toContainText("all apps");
    await expect(page.getByLabel(`Channel ${name}`)).toContainText("all environments");

    await page.getByLabel(`Channel ${name}`).getByRole("button", { name: "Delete" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Delete channel" }).click();
    await expect(page.getByLabel(`Channel ${name}`)).toHaveCount(0);
  });

  test("an email channel says a mail server is needed, and a bad address is refused", async ({ page }) => {
    const name = `e2e mail ${Date.now().toString(36)}`;
    await page.goto("/admin/notifications");
    await page.getByRole("button", { name: "Add channel" }).click();
    const form = page.getByRole("form", { name: "New channel" });
    await form.getByLabel("Name", { exact: true }).fill(name);
    await form.getByPlaceholder("oncall@example.com, lead@example.com").fill("not-an-address");
    await form.getByRole("button", { name: "Add channel" }).click();
    await expect(form.getByRole("alert")).toContainText("not a plain email address");
    await form.getByPlaceholder("oncall@example.com, lead@example.com").fill("oncall@example.com");
    await form.getByRole("button", { name: "Add channel" }).click();

    const row = page.getByLabel(`Channel ${name}`);
    await expect(row).toBeVisible();
    await expect(page.getByLabel("Mail server (SMTP)")).toContainText("needed by an email channel");
    await row.getByRole("button", { name: "Send test" }).click();
    await expect(row.getByRole("alert")).toContainText("no mail server is set up");

    await row.getByRole("button", { name: "Delete" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Delete channel" }).click();
    await expect(page.getByLabel(`Channel ${name}`)).toHaveCount(0);
  });
});

test.describe("a developer", () => {
  test.use({ storageState: authFile("bob") });

  test("has no Notifications page and the API refuses", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("link", { name: "Notifications", exact: true })).toHaveCount(0);
    expect((await page.request.get("/api/admin/notifications")).status()).toBe(403);
  });
});

test.describe("an admin choosing a webhook", () => {
  test.use({ storageState: authFile("alice") });

  test("a webhook can be started directly, without finding it in the type list", async ({ page }) => {
    await page.goto("/admin/notifications");
    await page.getByRole("button", { name: "Add webhook" }).click();
    const form = page.getByRole("form", { name: "New channel" });
    await expect(form.getByLabel("Type")).toHaveValue("webhook");
    await expect(form.getByLabel("Webhook address")).toBeVisible();
  });
});
