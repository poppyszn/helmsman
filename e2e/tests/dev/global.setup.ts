import { expect, test as setup } from "@playwright/test";
import { authFile, loginAs } from "../support";

// Sign each dev user in once and keep the session, so the specs do not hit the sign-in rate limit.
for (const user of ["alice", "bob", "carol", "erin"]) {
  setup(`sign in as ${user}`, async ({ page }) => {
    await loginAs(page, user);
    await page.getByRole("heading", { name: "Applications" }).waitFor();
    await page.context().storageState({ path: authFile(user) });
  });
}

// A fresh stack needs a moment: the agent registers after the first catalog load, the catalog has to be
// reloaded once it exists, and the agent has to send its first snapshot. Wait for all of it, so the first
// real test does not race the startup.
setup("the stack is ready", async ({ browser }) => {
  setup.setTimeout(180_000);
  const ctx = await browser.newContext({ storageState: authFile("alice"), baseURL: process.env.E2E_BASE_URL ?? "http://localhost:5173" });
  const api = ctx.request;
  const csrf = ((await (await api.get("/api/me")).json()) as { csrf_token: string }).csrf_token;
  await expect
    .poll(
      async () => {
        await api.post("/api/admin/catalog/reload", { headers: { "X-CSRF-Token": csrf }, data: {} });
        const apps = (await (await api.get("/api/apps")).json()) as { id: string }[];
        return apps.some((a) => a.id === "demo-app");
      },
      { timeout: 120_000, message: "demo-app never appeared in the catalog" },
    )
    .toBe(true);
  await expect
    .poll(async () => {
      const s = (await (await api.get("/api/apps/demo-app/envs/staging/status")).json()) as { container: unknown; agent: { online: boolean } };
      return s.agent.online && s.container !== null;
    }, { timeout: 120_000, message: "the agent never reported the demo container" })
    .toBe(true);
  await expect
    .poll(async () => {
      const g = (await (await api.get("/api/admin/github")).json()) as { repos: { repo: string; problem: string }[] };
      return g.repos.some((r) => r.repo === "acme-org/demo-app" && r.problem === "");
    }, { timeout: 90_000, message: "the repository check never ran" })
    .toBe(true);
  await ctx.close();
});
