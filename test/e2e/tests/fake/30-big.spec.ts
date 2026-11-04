import { expect, open, test } from "../../support/test.ts";
import { ctl } from "../../support/fake.ts";
import { filterLatency, scrollSmoothness } from "../../support/perf.ts";

const LOAD = process.env.E2E_BIG ?? "10000";

// T-UI-01, E4: 10,000 links.
test("10,000 links: first display under 3 s, filter under 150 ms, scrolling smooth", async ({ page, api }) => {
  test.setTimeout(240_000);
  await ctl(`/many?links=${LOAD}`);
  await expect.poll(async () => (await api.status()).downloads.links, { timeout: 120_000 }).toBeGreaterThanOrEqual(Number(LOAD));
  const total = (await api.status()).downloads.links;
  console.log(`E4 links in the downloads: ${total}`);

  // First display: from the navigation until the list shows rows and the footer knows all links. Cold start.
  const results: number[] = [];
  for (let i = 0; i < 3; i++) {
    const t0 = Date.now();
    await page.goto("/downloads");
    await expect(page.locator('[role="row"][data-key]').first()).toBeVisible({ timeout: 20_000 });
    await expect(page.locator(".foot")).toContainText(`Links ${total.toLocaleString("en-US")}`.replace(",", ""), { timeout: 20_000 }).catch(async () => {
      await expect(page.locator(".foot")).toContainText(/Links\s*10[,.]?\d{3}/, { timeout: 20_000 });
    });
    results.push(Date.now() - t0);
  }
  console.log(`E4 first display (3 cold loads): ${results.join(", ")} ms`);
  expect(Math.min(...results), "first display").toBeLessThan(3000);
  expect(Math.max(...results), "first display, worst of three").toBeLessThan(3000);

  // Filter: one input event until the filtered list is painted.
  const filterTimes: number[] = [];
  for (const [text, done] of [
    ["paket-042-datei-007", `(() => { const r = [...document.querySelectorAll('[role=row][data-key]')]; return r.length > 0 && r.length <= 3 && r.some(x => /paket-042-datei-007/.test(x.textContent)); })()`],
    ["", `document.querySelectorAll('[role=row][data-key]').length > 10`],
    ["datei-007", `(() => { const r = [...document.querySelectorAll('[role=row][data-key]')]; return r.length > 10 && r.every(x => x.getAttribute('aria-level') === '1' || /datei-007/.test(x.textContent)); })()`],
    ["zzzz-nothing", `!!document.querySelector('.empty')`],
    ["", `document.querySelectorAll('[role=row][data-key]').length > 10`],
  ] as const) {
    filterTimes.push(Math.round(await filterLatency(page, "#filter", text, done)));
  }
  console.log(`E4 filter, input event until painted (ms): ${filterTimes.join(", ")}`);
  for (const ms of filterTimes) expect(ms, "filter").toBeLessThan(150);

  // Scrolling: with a filter that matches every link, all 10,000 rows are in the list. Frame times while it is flung down.
  await filterLatency(page, "#filter", "datei-", `document.querySelectorAll('[role=row][data-key]').length > 10`);
  await expect(page.locator(".foot")).toContainText(/Links\s*10\d{3}/);
  const s = await scrollSmoothness(page, ".vt", 2500);
  console.log(`E4 scrolling: ${JSON.stringify(s)}`);
  expect(s.frames).toBeGreaterThan(30);
  expect(s.scrolled, "the list really moved").toBeGreaterThan(5000);
  expect(s.p95, "95th percentile of frame time").toBeLessThan(34);
  expect(s.longTasks, "long tasks (over 50 ms) while scrolling").toBeLessThanOrEqual(2);
  // Select all matches and open the action menu with that many: the page does not freeze.
  await page.locator("body").click({ position: { x: 5, y: 300 } });
  const t0 = Date.now();
  await page.getByRole("checkbox", { name: "Select all rows" }).click();
  await expect(page.locator(".sel-count")).toContainText(/selected/);
  console.log(`E4 select all (${total} links) until shown: ${Date.now() - t0} ms`);
  expect(Date.now() - t0).toBeLessThan(2000);
});

// T-CFG-04: the filter over about 2,200 settings (the fake holds 2,000 generated ones).
test("advanced settings: filter over thousands of entries under 150 ms", async ({ page }) => {
  await open(page, "/settings/expert");
  await expect(page.locator(".cfg-count")).toContainText(/\d[\d,.]* entries/, { timeout: 30_000 });
  const n = Number(((await page.locator(".cfg-count").textContent()) ?? "").replace(/[^\d]/g, ""));
  console.log(`T-CFG-04 entries: ${n}`);
  expect(n).toBeGreaterThan(2000);
  const times: number[] = [];
  for (const [text, done] of [
    ["Delay", `/ of \\d/.test(document.querySelector('.cfg-count').textContent)`],
    ["", `/entries/.test(document.querySelector('.cfg-count').textContent)`],
    ["Enabled", `/ of \\d/.test(document.querySelector('.cfg-count').textContent)`],
    ["", `/entries/.test(document.querySelector('.cfg-count').textContent)`],
    ["zzzz-none", `/ of \\d/.test(document.querySelector('.cfg-count').textContent)`],
  ] as const) times.push(Math.round(await filterLatency(page, ".cfg-filter input", text, done)));
  console.log(`T-CFG-04 filter, input event until painted (ms): ${times.join(", ")}`);
  for (const ms of times) expect(ms, "settings filter").toBeLessThan(150);
});
