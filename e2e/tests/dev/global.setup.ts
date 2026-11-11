import { test as setup } from "@playwright/test";
import { authFile, loginAs } from "../support";

// Sign each dev user in once and keep the session, so the specs do not hit the sign-in rate limit.
for (const user of ["alice", "bob", "carol", "erin"]) {
  setup(`sign in as ${user}`, async ({ page }) => {
    await loginAs(page, user);
    await page.getByRole("heading", { name: "Applications" }).waitFor();
    await page.context().storageState({ path: authFile(user) });
  });
}
