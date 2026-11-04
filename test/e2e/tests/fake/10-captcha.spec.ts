import { expect, open, test } from "../../support/test.ts";
import { axe } from "../../support/a11y.ts";
import { ctl } from "../../support/fake.ts";

// T-CAP-01..03 against the fake JDownloader: a real one cannot be made to ask for a captcha here.
test("image captcha: picture, answer, solve; interactive captcha: explained, skipped; dialogs of JDownloader", async ({ page, api }) => {
  await open(page, "/downloads");
  // The seed holds one image captcha.
  const chip = page.getByRole("button", { name: "1 captcha waiting" });
  await expect(chip).toBeVisible();
  await chip.click();
  const dlg = page.getByRole("dialog");
  const img = dlg.getByRole("img", { name: /Captcha image from example\.org/ });
  await expect(img).toBeVisible();
  // The picture is loaded, not a broken icon.
  await expect.poll(() => img.evaluate((i: HTMLImageElement) => i.complete && i.naturalWidth > 0)).toBe(true);
  await expect(dlg.getByText("Time left")).toBeVisible();
  await axe(page, "captcha dialog");
  await dlg.getByLabel("What does the picture say?").fill("W7K3Q");
  await dlg.getByRole("button", { name: "Solve" }).click();
  await expect.poll(async () => (await api.status()).captchas, { timeout: 10_000 }).toBe(0);
  await page.screenshot({ path: "test-results/captcha-image.png" });
  await page.keyboard.press("Escape");

  // Interactive: recognized, explained, can be skipped (T-CAP-03).
  await ctl("/captcha-interactive");
  await expect(page.getByRole("button", { name: "1 captcha waiting" })).toBeVisible({ timeout: 5000 });
  await page.getByRole("button", { name: "1 captcha waiting" }).click();
  const d2 = page.getByRole("dialog");
  await expect(d2.getByText("This captcha cannot be solved here.")).toBeVisible();
  await expect(d2.getByText(/clicks or sliders/)).toBeVisible();
  await axe(page, "interactive captcha dialog");
  await page.screenshot({ path: "test-results/captcha-interactive.png" });
  await d2.getByRole("button", { name: "Skip", exact: true }).first().click();
  await expect.poll(async () => (await api.status()).captchas, { timeout: 10_000 }).toBe(0);
  await page.keyboard.press("Escape");

  // Several ways to skip: blocking a hoster asks first.
  await ctl("/captcha");
  await page.getByRole("button", { name: "1 captcha waiting" }).click();
  const d3 = page.getByRole("dialog");
  await d3.getByRole("button", { name: "More ways" }).click();
  await d3.getByLabel(/All captchas of example\.org/).check();
  await d3.getByRole("button", { name: "Skip", exact: true }).last().click();
  await expect(page.getByRole("alertdialog")).toContainText("example.org");
  await page.getByRole("alertdialog").getByRole("button", { name: "Cancel" }).click();
  await expect.poll(async () => (await api.status()).captchas).toBe(1);
});
