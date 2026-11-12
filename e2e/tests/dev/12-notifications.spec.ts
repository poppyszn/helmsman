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
      await form.getByLabel("Name").fill(name);
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

  test("an email channel says a mail server is needed, and a bad address is refused", async ({ page }) => {
    const name = `e2e mail ${Date.now().toString(36)}`;
    await page.goto("/admin/notifications");
    await page.getByRole("button", { name: "Add channel" }).click();
    const form = page.getByRole("form", { name: "New channel" });
    await form.getByLabel("Name").fill(name);
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
