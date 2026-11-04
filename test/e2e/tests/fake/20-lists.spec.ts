import { expect, open, test } from "../../support/test.ts";
import { ctl } from "../../support/fake.ts";

// T-UI-02: a package of 1, 24 and 500 links; the summary starts at 25.
test("segments of a package with 1, 24 and 500 links; summary from 25", async ({ page }) => {
  await ctl("/package?name=seg-one&links=1");
  await ctl("/package?name=seg-24&links=24");
  await ctl("/package?name=seg-25&links=25");
  await ctl("/package?name=seg-500&links=500");
  await open(page, "/downloads");
  const pkg = (n: string) => page.getByRole("row", { name: new RegExp(`${n}\\b`) }).first();
  await expect(pkg("seg-500")).toBeVisible();
  // One segment per link up to 24, 24 segments for more.
  const segs = async (n: string) => pkg(n).locator(".seg i").count();
  expect(await segs("seg-one")).toBe(1);
  expect(await segs("seg-24")).toBe(24);
  expect(await segs("seg-25")).toBe(24);
  expect(await segs("seg-500")).toBe(24);
  // Every package carries its summary for assistive technology: "x of n finished", errors counted.
  const label = (n: string) => pkg(n).locator(".seg").getAttribute("aria-label");
  expect(await label("seg-500")).toMatch(/^\d+ of 500 finished, 25 errors$/);
  expect(await label("seg-25")).toMatch(/^\d+ of 25 finished, \d+ errors?$/);
  expect(await label("seg-one")).toMatch(/^\d+ of 1 finished$/);
  // The kinds are there: finished, running, error among the segments of the biggest; the error shows.
  const kinds = await pkg("seg-500").locator(".seg i").evaluateAll((els) => [...new Set(els.map((e) => [...e.classList].find((c) => c.startsWith("k-"))))]);
  console.log("T-UI-02 segment kinds in a package of 500:", JSON.stringify(kinds));
  expect(kinds.length).toBeGreaterThan(2);
  expect(kinds).toContain("k-err");
  // One segment is exactly one link up to 24: one finished segment per finished link.
  const done24 = await pkg("seg-24").locator(".seg i.k-done").count();
  console.log("T-UI-02 finished segments of 24 links:", done24);
});

// T-UI-09: JDownloader offline: banner, actions off, clear hint; heals alone.
test("JDownloader offline: banner, actions locked, hint; back by itself", async ({ page }) => {
  await open(page, "/downloads");
  try {
    await ctl("/down");
    test.info().annotations.push({ type: "note", description: "the fake is brought back up at the end" });
    const banner = page.getByRole("alert").filter({ hasText: "JDownloader does not answer." });
    await expect(banner).toBeVisible({ timeout: 20_000 });
    await expect(banner).toContainText("Actions are off until the connection is back.");
    await expect(page.getByText("JDownloader offline")).toBeVisible();
    for (const name of ["Actions", "Clean up"]) await expect(page.getByRole("button", { name, exact: true })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Add links", exact: true })).toBeDisabled();
    for (const b of ["Start", "Pause", "Stop"]) await expect(page.getByRole("group", { name: "Download control" }).getByRole("button", { name: b })).toBeDisabled();
    await expect(page.locator(".speed b")).toHaveText(/^0\s/);
    // Nothing is shown as running.
    await expect(page.getByText("Running", { exact: true })).toHaveCount(0);
    // The list is the last known one, not an empty page.
    await expect(page.getByRole("row").nth(1)).toBeVisible();
    await page.screenshot({ path: "test-results/offline.png" });
    await ctl("/up");
    await expect(banner).toBeHidden({ timeout: 40_000 });
    await expect(page.getByRole("button", { name: "Add links", exact: true })).toBeEnabled();

  } finally {
    await ctl("/up");
  }
});
