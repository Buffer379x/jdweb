import { writeFileSync } from "node:fs";
import { expect, test } from "@playwright/test";
import { Api } from "../../support/api.ts";
import { axe } from "../../support/a11y.ts";
import { AUTOSTART } from "../../support/env.ts";
import { APP_CONTAINER, PASSWORD, USER, setupToken } from "../../support/env.ts";

// Flow: first setup with the token from the log, then login. Leaves the session in .auth/real.json.
test("setup with the token from the log, then sign in", async ({ page, baseURL }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "First setup" })).toBeVisible();
  // A wrong token is refused with a clear text.
  await axe(page, "setup");
  await page.getByLabel("Setup token").fill("not-the-token");
  await page.getByLabel("Username").fill(USER);
  await page.locator("input[type=password]").first().fill(PASSWORD);
  await page.locator("input[type=password]").nth(1).fill(PASSWORD);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByText("The token is not right.")).toBeVisible();
  await page.getByLabel("Setup token").fill(setupToken(APP_CONTAINER));
  await page.getByRole("button", { name: "Create account" }).click();
  // The account is created and the browser is signed in. The safety notes of the connection come first.
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("The JDownloader API has no login");
  await dialog.getByRole("button", { name: "Confirm" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("navigation", { name: "Sections" })).toBeVisible();
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("menuitem", { name: "Sign out (e2e)" }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await axe(page, "sign in");
  await page.getByLabel("Username").fill(USER);
  await page.getByLabel("Password").fill("wrong-password-123");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByText("Sign-in failed")).toBeVisible();
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("navigation", { name: "Sections" })).toBeVisible();
  await expect(page.getByText("JDownloader connected")).toBeVisible();
  await page.context().storageState({ path: ".auth/real.json" });
  // The guard, then the one setting that would disturb the tests: JDownloader starts what is moved to
  // the downloads (LinkgrabberAutoStartEnabled, default on). Put back by 99-teardown.
  const api = new Api(page.request, baseURL!);
  await api.assertTestInstance();
  const cur = (await api.get("/config?q=LinkgrabberAutoStartEnabled")).entries.find((e: any) => e.key === AUTOSTART.key);
  writeFileSync(".auth/autostart-old.json", JSON.stringify(cur?.value ?? true));
  await api.put("/config", { ...AUTOSTART, value: false });
});
