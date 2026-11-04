import { expect, open, test } from "../../support/test.ts";

// Regression: a validation error in a setting is announced to a screen reader (ConfigRow), once, not at every key.
test("a wrong value in a setting is announced, and the field is marked invalid", async ({ page }) => {
  await open(page, "/settings/expert");
  await page.getByLabel("Filter settings").fill("MaxSimultaneDownloadsPerHost");
  const field = page.getByRole("textbox", { name: /MaxSimultaneDownloadsPerHost/ }).first();
  await expect(field).toBeVisible();
  const live = page.locator('[role="status"][aria-live="polite"][aria-atomic="true"]');
  await field.fill("abc");
  await expect(field).toHaveAttribute("aria-invalid", "true");
  await expect(live).toContainText("MaxSimultaneDownloadsPerHost: A whole number between");
  // Typing on in the same wrong state does not repeat it: the text of the region stays what it was.
  const first = await live.textContent();
  await field.pressSequentially("de");
  await page.waitForTimeout(200);
  expect(await live.textContent()).toBe(first);
  // A good value clears the mark.
  await field.fill("2");
  await expect(field).not.toHaveAttribute("aria-invalid", "true");
});
