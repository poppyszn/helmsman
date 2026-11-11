import { expect, test } from "@playwright/test";
import { authFile } from "../support";

// The Mail page: messages the dev environments sent, read from Mailpit inside Helmsman. The dev server
// starts with Mailpit connected (IDP_MAILPIT_URL), and this test sends its own mail so it never depends on
// what was seeded.
const MAILPIT = process.env.E2E_MAILPIT_URL ?? "http://mailpit:8025";
const subject = `Order ${Date.now()} confirmed`;

test.beforeAll(async ({ request }) => {
  const res = await request.post(`${MAILPIT}/api/v1/send`, {
    data: {
      From: { Email: "orders@shop.example.com", Name: "E2E Shop" },
      To: [{ Email: "e2e-buyer@example.com", Name: "Buyer" }],
      Subject: subject,
      Text: "Plain text version of the order",
      HTML: '<h2>HTML version of the order</h2><p>Thanks!</p><script>document.body.innerHTML="HACKED"</script><img src="https://tracker.example.invalid/p.gif">',
      Attachments: [{ Filename: "receipt.txt", Content: Buffer.from("receipt body").toString("base64") }],
    },
  });
  expect(res.ok()).toBeTruthy();
});

test.describe("a developer", () => {
  test.use({ storageState: authFile("bob") });

  test("reads a dev email from the sidebar, with its HTML shown safely", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("link", { name: "Mail", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Mail", exact: true })).toBeVisible();
    await page.getByLabel("Search mail").fill(subject);
    await page.getByRole("button", { name: new RegExp(subject) }).click();

    await expect(page.getByRole("heading", { name: subject })).toBeVisible();
    await expect(page.getByText("orders@shop.example.com")).toBeVisible();
    // The HTML body renders in a sandboxed frame. Its script must not have run, and it cannot reach the page.
    const frame = page.frameLocator('iframe[title="Message body"]');
    await expect(frame.getByRole("heading", { name: "HTML version of the order" })).toBeVisible();
    await expect(frame.getByText("HACKED")).toHaveCount(0);
    expect(await page.locator('iframe[title="Message body"]').getAttribute("sandbox")).not.toContain("allow-scripts");
    expect(await page.locator('iframe[title="Message body"]').getAttribute("sandbox")).not.toContain("allow-same-origin");

    await page.getByRole("tab", { name: "text" }).click();
    await expect(page.getByText("Plain text version of the order")).toBeVisible();
    const attachment = page.getByRole("link", { name: "receipt.txt" });
    await expect(attachment).toBeVisible();
    const [download] = await Promise.all([page.waitForEvent("download"), attachment.click()]);
    expect(download.suggestedFilename()).toBe("receipt.txt");
  });
});

test.describe("a viewer", () => {
  test.use({ storageState: authFile("carol") });

  test("has no Mail item and cannot read the API", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("link", { name: "Apps", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Mail", exact: true })).toHaveCount(0);
    const res = await page.request.get("/api/mail/messages");
    expect(res.status()).toBe(403);
  });
});

test.describe("an admin", () => {
  test.use({ storageState: authFile("alice") });

  test("sees Mailpit connected on the Integrations page", async ({ page }) => {
    await page.goto("/admin/integrations");
    await expect(page.getByRole("heading", { name: "Mail (Mailpit)" })).toBeVisible();
    await expect(page.getByLabel("Mailpit address")).toHaveValue("http://mailpit:8025");
  });
});
