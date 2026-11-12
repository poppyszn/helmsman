import { expect, test } from "@playwright/test";
import { authFile } from "../support";

test.use({ storageState: authFile("alice") });

test("tailing logs streams lines from the container", async ({ page }) => {
  await page.goto("/apps/demo-app");
  const logs = page.getByRole("region", { name: "Logs" });
  await logs.getByRole("button", { name: "tail logs" }).click();
  await expect(logs.getByText(/demo-app heartbeat \d+/).first()).toBeVisible();
  await expect(logs.getByRole("button", { name: "stop" })).toBeVisible();
  await logs.getByRole("button", { name: "stop" }).click();
  await expect(logs.getByRole("button", { name: "reconnect" })).toBeVisible();
});

test("pause holds new lines and the button changes to resume", async ({ page }) => {
  await page.goto("/apps/demo-app");
  const logs = page.getByRole("region", { name: "Logs" });
  await logs.getByRole("button", { name: "tail logs" }).click();
  await expect(logs.getByText(/demo-app heartbeat \d+/).first()).toBeVisible();
  await logs.getByRole("button", { name: "pause" }).click();
  // The opposite action is now offered, not the same icon again, and lines pile up behind it.
  await expect(logs.getByRole("button", { name: /^resume/ })).toBeVisible();
  await expect(logs.getByRole("button", { name: /^resume \(\d+\)/ })).toBeVisible({ timeout: 10_000 });
  await logs.getByRole("button", { name: /^resume/ }).click();
  await expect(logs.getByRole("button", { name: "pause" })).toBeVisible();
});

test("the filter keeps only matching lines", async ({ page }) => {
  await page.goto("/apps/demo-app");
  const logs = page.getByRole("region", { name: "Logs" });
  await logs.getByRole("button", { name: "tail logs" }).click();
  await expect(logs.getByText(/demo-app heartbeat \d+/).first()).toBeVisible();
  await logs.getByPlaceholder("filter...").fill("no-such-text-anywhere");
  await expect(logs.getByText(/demo-app heartbeat \d+/)).toHaveCount(0);
  await logs.getByPlaceholder("filter...").fill("heartbeat");
  await expect(logs.getByText(/demo-app heartbeat \d+/).first()).toBeVisible();
});

test("opening a log stream is audited without the log text", async ({ page }) => {
  await page.goto("/apps/demo-app");
  const logs = page.getByRole("region", { name: "Logs" });
  await logs.getByRole("button", { name: "tail logs" }).click();
  await expect(logs.getByText(/demo-app heartbeat \d+/).first()).toBeVisible();
  const res = await page.request.get("/api/admin/audit?limit=50");
  const rows = (await res.json()) as { action: string; target: string; details: unknown }[];
  const row = rows.find((r) => r.action === "logs.view" && r.target === "demo-app/staging");
  expect(row, "a logs.view entry").toBeTruthy();
  expect(JSON.stringify(row)).not.toContain("heartbeat");
});

test("lines in any format are laid out for reading, and raw shows them as written", async ({ page, request }) => {
  const sent = await request.post("http://demo-app:8080/log-samples");
  expect(sent.ok()).toBeTruthy();
  await page.goto("/apps/demo-app");
  const logs = page.getByRole("region", { name: "Logs" });
  await logs.getByRole("button", { name: "tail logs" }).click();

  // JSON: a level badge, the message, and the other fields as key=value.
  const json = logs.getByText("payment gateway timeout").first();
  await expect(json).toBeVisible();
  const row = json.locator("xpath=ancestor::div[contains(@class,'flex')][1]");
  await expect(row).toContainText("error");
  await expect(row).toContainText("order=1042");
  // key=value and plain lines get a level too.
  await expect(logs.getByText("cache miss storm").first()).toBeVisible();
  await expect(logs.getByText("could not reach the mailer").first()).toBeVisible();
  // nginx access line: method, path and status.
  const access = logs.getByText("/missing").first();
  await expect(access).toBeVisible();
  await expect(access.locator("xpath=..")).toContainText("404");

  // Only errors: the warning and the access line (a 404 is a warning) are hidden, the error and its stack stay.
  await logs.getByLabel("Minimum level").selectOption("error");
  await expect(logs.getByText("payment gateway timeout").first()).toBeVisible();
  await expect(logs.getByText("cache miss storm")).toHaveCount(0);

  // Raw: the JSON exactly as the app wrote it.
  await logs.getByLabel("Minimum level").selectOption("all");
  await logs.getByLabel("raw").check();
  await expect(logs.getByText('{"level":"error","msg":"payment gateway timeout","order":1042,"ms":5003}').first()).toBeVisible();
});
