import { execFileSync } from "node:child_process";
import { expect, open, test } from "../../support/test.ts";
import { fx, row } from "../../support/api.ts";
import { PREFIX, JD_CONTAINER, docker, jdSpeed } from "../../support/env.ts";

test.beforeEach(async ({ api }) => {
  await api.wipe();
});

const tab = (page: import("@playwright/test").Page, name: string) => page.getByRole("navigation", { name: "Sections" }).getByRole("link", { name });
const startBtn = (page: import("@playwright/test").Page) => page.getByRole("group", { name: "Download control" }).getByRole("button", { name: "Start" });

test("add links, linkgrabber to downloads, live progress within 2 s, pause, stop", async ({ page, api }) => {
  await open(page);
  await page.getByRole("button", { name: "Add links", exact: true }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByLabel("Links").fill([fx("/slow/e2e-a.bin?size=400000000"), fx("/slow/e2e-b.bin?size=400000000")].join("\n"));
  await dlg.getByLabel("Package name").fill(PREFIX + "slow");
  await dlg.getByRole("button", { name: "Add", exact: true }).click();
  await expect(dlg).toBeHidden();

  await tab(page, "Linkgrabber").click();
  const pkg = page.getByRole("row", { name: /jdweb-e2e-slow/ });
  await expect(pkg).toContainText("2 online", { timeout: 60_000 });
  await pkg.getByRole("button", { name: "Expand" }).click();
  await expect(row(page, "e2e-a.bin")).toBeVisible();
  await expect(row(page, "e2e-b.bin")).toBeVisible();

  // To the downloads: nothing starts by itself.
  await page.getByRole("button", { name: "All to downloads" }).click();
  await expect(page.getByText("Moved to the downloads.")).toBeVisible();
  await tab(page, "Downloads").click();
  await expect(page.getByRole("row", { name: /jdweb-e2e-slow/ })).toBeVisible();
  await expect(page.getByText("Stopped", { exact: true }).first()).toBeVisible();

  // E3: a change in JDownloader is in the browser within 2 s. Start, and watch the whole chain.
  await api.limit(100_000);
  const t0 = Date.now();
  // The clock starts when JDownloader itself reports a speed, not at the click: how long JDownloader needs
  // to connect to the source is not the browser's affair.
  await startBtn(page).click();
  let tJD = 0;
  while (!tJD) {
    if ((await jdSpeed()) > 0) tJD = Date.now();
    else await page.waitForTimeout(50);
    expect(Date.now() - t0, "JDownloader does not start").toBeLessThan(30_000);
  }
  await expect(startBtn(page)).toHaveAttribute("aria-pressed", "true", { timeout: 2000 });
  await expect(page.locator(".speed b")).not.toHaveText(/^0\s/, { timeout: 2000 });
  console.log(`E3 JDownloader reports a speed ${tJD - t0} ms after the click; the browser shows it ${Date.now() - tJD} ms later`);
  await expect(page).toHaveTitle(/%|B\/s/);
  // Progress moves while it runs: two readings, 2 s apart, differ.
  const pct = async () => (await page.getByRole("row", { name: /jdweb-e2e-slow/ }).locator(".pct").first().textContent()) ?? "";
  const p1 = await pct();
  await expect.poll(pct, { timeout: 4000, intervals: [500] }).not.toBe(p1);

  // Pause, then stop.
  await page.getByRole("group", { name: "Download control" }).getByRole("button", { name: "Pause" }).click();
  await expect(page.getByText("Paused", { exact: true }).first()).toBeVisible({ timeout: 3000 });
  await page.getByRole("group", { name: "Download control" }).getByRole("button", { name: "Stop" }).click();
  await expect(page.getByText("Stopped", { exact: true }).first()).toBeVisible({ timeout: 5000 });
  await expect.poll(async () => (await api.status()).controller, { timeout: 10_000 }).toBe("IDLE");
});

const CTL = (page: import("@playwright/test").Page) => page.getByRole("group", { name: "Download control" });

test("cleanup: files stay by default, the file mode asks and names count and consequence", async ({ page, api }) => {
  const dir = "/output/jdweb-e2e/clean";
  docker("exec", JD_CONTAINER, "sh", "-c", `rm -rf ${dir}`); // files of an earlier run stay on purpose (links-only cleanup)
  await api.stage("clean", [fx("/e2e-clean-1.bin?size=2000"), fx("/e2e-clean-2.bin?size=2000")], { dir });
  await open(page, "/downloads");
  await api.post("/controller/start");
  await expect.poll(async () => (await api.downloads()).links.filter((l) => l.state === "finished").length, { timeout: 60_000 }).toBe(2);
  await expect(page.getByRole("row", { name: /jdweb-e2e-clean/ })).toContainText("Finished");
  const files = () => docker("exec", JD_CONTAINER, "sh", "-c", `find ${dir} -type f 2>/dev/null | wc -l`).trim();
  expect(files()).toBe("2");

  // Default mode: only the links go, the files stay.
  await page.getByRole("button", { name: "Clean up" }).click();
  let dlg = page.getByRole("dialog");
  await expect(dlg.getByLabel("Remove links only, keep files")).toBeChecked();
  await expect(dlg).toContainText("Affects 2 links.");
  await dlg.getByLabel("Remove links, delete files for good").check();
  // Files are at stake: the confirmation names the count and what happens.
  await dlg.getByRole("button", { name: "Clean up" }).click();
  const confirm = page.getByRole("alertdialog");
  await expect(confirm).toContainText("Remove 2 links from the list.");
  await expect(confirm).toContainText("The files are deleted for good.");
  expect(files()).toBe("2"); // nothing happened before the confirmation
  await confirm.getByRole("button", { name: "Cancel" }).click();
  expect(files()).toBe("2");
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  await dlg.getByRole("button", { name: "Clean up" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Clean up" }).click();
  await expect(page.getByRole("row", { name: /jdweb-e2e-clean/ })).toHaveCount(0);
  await expect.poll(files).toBe("0");

  // Links only: the files stay.
  await api.stage("clean2", [fx("/e2e-clean-3.bin?size=2000")], { dir });
  await api.post("/controller/start");
  await expect.poll(async () => (await api.downloads()).links.filter((l) => l.state === "finished").length, { timeout: 60_000 }).toBe(1);
  await expect(page.getByRole("row", { name: /jdweb-e2e-clean2/ })).toContainText("Finished");
  await page.getByRole("button", { name: "Clean up" }).click();
  dlg = page.getByRole("dialog");
  await dlg.getByRole("button", { name: "Clean up" }).click();
  await expect(page.getByRole("row", { name: /jdweb-e2e-clean2/ })).toHaveCount(0);
  expect(files()).toBe("1");
  docker("exec", JD_CONTAINER, "sh", "-c", `rm -rf ${dir}`);
});

test("drag and drop: locked while a filter is on, works without", async ({ page, api }) => {
  await api.stage("dnd-a", [fx("/e2e-dnd-a.bin")]);
  await api.stage("dnd-b", [fx("/e2e-dnd-b.bin")]);
  await open(page, "/downloads");
  const a = page.getByRole("row", { name: /jdweb-e2e-dnd-a/ });
  const b = page.getByRole("row", { name: /jdweb-e2e-dnd-b/ });
  const order = async () => (await api.downloads()).packages.map((p) => p.name.replace(PREFIX, ""));
  expect(await order()).toEqual(["dnd-a", "dnd-b"]);

  // With a filter the rows are not draggable and a drop changes nothing.
  await page.getByLabel("Filter").fill("dnd");
  await expect(a).toBeVisible();
  await expect(a).toHaveAttribute("draggable", "false");
  await b.dragTo(a, { targetPosition: { x: 200, y: 2 } });
  await page.waitForTimeout(800);
  expect(await order()).toEqual(["dnd-a", "dnd-b"]);

  // Without it: b goes before a.
  await page.getByLabel("Filter").fill("");
  await expect(a).toHaveAttribute("draggable", "true");
  await b.dragTo(a, { targetPosition: { x: 200, y: 2 } });
  await expect.poll(order).toEqual(["dnd-b", "dnd-a"]);
  await expect(page.getByRole("row", { name: /jdweb-e2e-dnd/ }).first()).toContainText("dnd-b");
});

test("JDownloader restarts during a download: offline, heals by itself, nothing stays running", async ({ page, api }) => {
  await api.limit(60_000);
  await api.stage("restart", [fx("/e2e-restart-1.bin"), fx("/e2e-restart-2.bin")]);
  await open(page, "/downloads");
  await CTL(page).getByRole("button", { name: "Start" }).click();
  await expect(page.locator(".speed b")).not.toHaveText(/^0\s/, { timeout: 5000 });
  const rowStatus = page.getByRole("row", { name: /jdweb-e2e-restart/ });
  await expect(rowStatus).toContainText("Running");

  docker("restart", "-t", "2", JD_CONTAINER);
  // The page says so, with the banner, and the actions are off.
  const banner = page.getByRole("alert").filter({ hasText: "JDownloader does not answer." });
  await expect(banner).toBeVisible({ timeout: 20_000 });
  await expect(page.getByRole("button", { name: "Add links", exact: true })).toBeDisabled();
  // Nothing claims to run while it is away.
  await expect(page.getByText("Running", { exact: true })).toHaveCount(0);
  await expect(page.locator(".speed b")).toHaveText(/^0\s/);
  // It heals without a reload (E5: within 30 s of the answer).
  await expect(page.getByText("JDownloader connected")).toBeVisible({ timeout: 90_000 });
  await expect(banner).toBeHidden();
  await expect(page.getByRole("button", { name: "Add links", exact: true })).toBeEnabled();
  await expect.poll(async () => (await api.status()).controller, { timeout: 30_000 }).toBe("IDLE");
  await expect(page.getByRole("row", { name: /jdweb-e2e-restart/ })).not.toContainText("Running");
  await expect(page.locator(".speed b")).toHaveText(/^0\s/);
  expect((await api.downloads()).links.filter((l) => l.state === "running")).toHaveLength(0);
  // And it works again, in the same page.
  await page.getByRole("button", { name: "Add links", exact: true }).click();
  await page.getByRole("dialog").getByLabel("Links").fill(fx("/e2e-after-restart.bin"));
  await page.getByRole("dialog").getByLabel("Package name").fill(PREFIX + "after");
  await page.getByRole("dialog").getByRole("button", { name: "Add", exact: true }).click();
  await expect.poll(async () => (await api.grabber()).links.length, { timeout: 60_000 }).toBe(1);
});
