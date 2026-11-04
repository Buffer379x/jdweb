import { expect, open, test } from "../../support/test.ts";
import { fx } from "../../support/api.ts";
import { PREFIX, PASSWORD, USER } from "../../support/env.ts";

test.beforeEach(async ({ api }) => {
  await api.wipe();
});

// An action on an entry that JDownloader has removed meanwhile.
test("an action on an entry that is gone: clear message or quiet reload, no crash, the view catches up", async ({ page, api, watch }) => {
  await page.addInitScript(() => {
    // A view whose events arrive late: patches and structure events wait while "frozen", then come in order.
    const w = window as any;
    w.__queue = [] as (() => void)[];
    w.__thaw = () => { w.__freeze = false; for (const f of w.__queue.splice(0)) f(); };
    const add = EventSource.prototype.addEventListener;
    EventSource.prototype.addEventListener = function (this: EventSource, type: string, fn: any, o?: any) {
      if (type === "patch" || type === "structure") return add.call(this, type, (ev: Event) => { if (w.__freeze) w.__queue.push(() => fn(ev)); else fn(ev); }, o);
      return add.call(this, type, fn, o);
    } as typeof add;
  });
  await api.stage("gone-a", [fx("/e2e-gone-a.bin")]);
  await api.stage("gone-b", [fx("/e2e-gone-b.bin")]);
  await open(page, "/downloads");
  const a = page.getByRole("row", { name: /jdweb-e2e-gone-a/ });
  const b = page.getByRole("row", { name: /jdweb-e2e-gone-b/ });
  await expect(a).toBeVisible();
  await expect(b).toBeVisible();

  await page.evaluate(() => ((window as any).__freeze = true));
  const ids = (await api.downloads()).packages.filter((p) => p.name.endsWith("gone-a")).map((p) => p.id);
  await api.post("/downloads/actions/remove", { packageIds: ids, linkIds: [] });
  await page.waitForTimeout(1200);
  await expect(a, "the view that has not heard yet still shows the removed package").toBeVisible();

  // Act on it: rename. JDownloader no longer has it.
  await a.click();
  await page.getByRole("button", { name: "Actions" }).click();
  await page.getByRole("menuitem", { name: "Rename" }).click();
  await page.getByRole("dialog").getByRole("textbox").fill("jdweb-e2e-renamed");
  await page.getByRole("dialog").getByRole("button", { name: /Apply|Rename|Save/ }).click();
  await page.waitForTimeout(300);
  await page.screenshot({ path: "test-results/gone.png" });
  await page.evaluate(() => (window as any).__thaw());

  // The view converges on its own: the removed row goes, the other stays, nothing crashed.
  await expect(a).toHaveCount(0, { timeout: 10_000 });
  await expect(b).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(watch.errors.filter((e) => !/Failed to load resource/.test(e))).toEqual([]);
  // A clear text when JDownloader said no, never a raw error.
  const toasts = await page.locator(".toasts, [role=status], [role=alert]").allTextContents();
  console.log("Removed package, texts after the action on the removed package:", JSON.stringify(toasts.filter(Boolean)));
  expect(toasts.join(" ")).not.toMatch(/undefined|\[object|jd_error|TypeError/);
});

// Wrong addresses give different, clear hints.
test("diagnostics: wrong address variants are told apart", async ({ page, api }) => {
  const right = "http://jdweb-test-jd:3128";
  test.setTimeout(180_000);
  await open(page, "/settings/diagnostics");
  const field = page.locator(".conn-form input");
  const save = page.getByRole("button", { name: "Save and connect" });
  const msg = page.locator("#conn-msg");
  try {
    const texts: Record<string, string> = {};
    const tryUrl = async (name: string, url: string) => {
      await page.reload(); // a fresh message area
      await page.getByRole("button", { name: "Later" }).click({ timeout: 3000 }).catch(() => {}); // the safety notes come back with a new address
      await expect(field).toBeVisible();
      await field.fill(url);
      await save.click();
      await expect(msg).not.toHaveText(/^$|Checking/, { timeout: 30_000 });
      texts[name] = ((await msg.textContent()) ?? "").trim();
      return texts[name]!;
    };
    // A slash at the end and a missing scheme are fixed by JDWeb.
    expect(await tryUrl("slash", right + "/")).toBe("Connected.");
    await expect(field).toHaveValue(right);
    expect(await tryUrl("noscheme", "jdweb-test-jd:3128")).toBe("Connected.");
    await expect(field).toHaveValue(right);
    // Another service on the port.
    expect(await tryUrl("other", "http://jdweb-test-files:8000")).toMatch(/another service|not JDownloader/i);
    // Nothing on the port.
    const nothing = await tryUrl("nothing", "http://jdweb-test-jd:3999");
    expect(nothing).not.toBe("Connected.");
    expect(nothing).not.toMatch(/another service/i);
    // A name that does not exist.
    const noname = await tryUrl("noname", "http://no-such-host.invalid:3128");
    expect(noname).not.toBe("Connected.");
    console.log("Wrong address, texts:", JSON.stringify(texts));
    expect(new Set([texts.other, texts.nothing, texts.noname]).size, "the three failures read differently").toBe(3);
    // The banner and the diagnosis name the problem and the way out.
    await page.getByRole("button", { name: "Check again" }).click();
    await expect(page.getByText(/Problem/).first()).toBeVisible();
    // Back to the right address: it heals.
    expect(await tryUrl("back", right)).toBe("Connected.");
    await expect(page.getByText("JDownloader connected")).toBeVisible();

  } finally {
    await api.put("/connection", { url: right }, 200).catch(() => {});
    await api.post("/diagnostics/acknowledge").catch(() => {});
  }
});

// T-UI-08, T-SEC-08: hostile names stay text, the layout holds. JDownloader itself replaces
// < and > in names, so the markup that can arrive is entity text, a long name, right-to-left text and emoji;
// the address of a link (details panel) keeps its characters.
test("hostile names: shown as the text JDownloader holds, long and right-to-left names do not break the layout", async ({ page, api, watch }) => {
  const asked = [
    `jdweb-e2e-<script>window.__xss=1</script><img src=x onerror="window.__xss=2">`,
    `jdweb-e2e-&lt;script&gt;window.__xss=3&lt;/script&gt; &amp; &#60;b&#62;bold`,
    `jdweb-e2e-${"x".repeat(500)}`,
    `jdweb-e2e-עברית-عربي-\u202Eevil-gnp.exe`,
    `jdweb-e2e-😀🧨👨‍👩‍👧‍👦 emoji`,
  ];
  const held: string[] = [];
  for (const [i, name] of asked.entries()) held.push(await api.stage(name, [fx(`/e2e-evil-${i}-${"y".repeat(i === 2 ? 300 : 3)}.bin`)], { raw: true }));
  held.push(await api.stage("jdweb-e2e-files", [
    fx("/e2e-%3Cscript%3Ewindow.__xss%3D4%3C%2Fscript%3E.bin"),
    fx("/%D8%B9%D8%B1%D8%A8%D9%8A-%D7%A2%D7%91%D7%A8%D7%99%D7%AA.bin"),
    fx("/%F0%9F%98%80%F0%9F%A7%A8-emoji.bin"),
    fx(`/${"z".repeat(500)}.bin`),
  ], { raw: true }));
  console.log("Hostile names, what JDownloader made of the names:", JSON.stringify(held.map((h) => h.slice(0, 60))));
  await open(page, "/downloads");
  await expect(page.getByRole("row")).toHaveCount(held.length + 1, { timeout: 15_000 });
  const scripts = await page.evaluate(() => document.querySelectorAll("script").length);
  for (let i = 0; i < held.length; i++) await page.getByRole("button", { name: "Expand" }).first().click();
  await expect(page.getByRole("row")).toHaveCount(held.length + 1 + 4 + 5);
  // Every package is shown as exactly the text that JDownloader holds.
  const shown = await page.locator('[role="row"][aria-level="1"] .name').evaluateAll((els) => els.map((e) => e.textContent));
  // (the invisible reordering controls are shown as a replacement mark on purpose, lib/text.ts)
  expect([...shown].sort()).toEqual(held.map((h) => h.replace(/[\u202a-\u202e\u2066-\u2069\u200e\u200f\u061c]/g, "\ufffd")).sort());
  expect(shown.join("")).not.toMatch(/[\u202a-\u202e]/);
  const scrollsSideways = () => page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
  const state = () => page.evaluate(() => ({
    xss: (window as any).__xss ?? null,
    scripts: document.querySelectorAll("script").length,
    injected: document.querySelectorAll(".name img, .name script, .name b, [onerror]").length,
  }));
  let st = await state();
  expect(st).toEqual({ xss: null, scripts, injected: 0 });
  for (const w of [390, 768, 1280]) {
    await page.setViewportSize({ width: w, height: 800 });
    expect(await scrollsSideways(), `no sideways scroll at ${w}`).toBe(false);
  }
  // Rows keep one height: a 500 character name does not make its row grow.
  const heights = await page.locator('[role="row"][data-key]').evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().height)));
  expect(new Set(heights).size, `row heights ${heights}`).toBeLessThanOrEqual(2);
  // The details panel shows the address as it is.
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.getByRole("row", { name: /script.*window/ }).first().getByRole("button", { name: /Expand|Collapse/ }).first().focus().catch(() => {});
  await page.locator('[role="row"][aria-level="2"] .name', { hasText: /script/ }).first().click();
  if (!(await page.getByText("Address", { exact: true }).isVisible())) await page.getByRole("button", { name: "Details" }).click();
  await expect(page.getByText("Address", { exact: true })).toBeVisible();
  st = await state();
  expect(st).toEqual({ xss: null, scripts, injected: 0 });
  expect(await scrollsSideways()).toBe(false);
  await page.screenshot({ path: "test-results/hostile.png" });
  expect(watch.csp).toEqual([]);
});

// T-SEC-07: logout ends the stream; the browser's reconnect is a 401.
test("sign out closes the event stream", async ({ page, api }) => {
  await open(page, "/downloads");
  const streams: number[] = [];
  page.on("response", (r) => { if (r.url().endsWith("/api/v1/events")) streams.push(r.status()); });
  await page.reload();
  await open(page, "/downloads");
  await expect.poll(() => streams.length).toBeGreaterThan(0);
  expect(streams[0]).toBe(200);
  const before = streams.length;
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("menuitem", { name: `Sign out (${USER})` }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  // No new stream is opened by a signed-out page, and the API says 401 to a stream request.
  await page.waitForTimeout(4500);
  expect(streams.length, "the signed-out page does not reconnect").toBe(before);
  const res = await page.request.get("/api/v1/events");
  expect(res.status()).toBe(401);
  // The session of the stored login is gone for good: sign in again for the tests that follow.
  await page.getByLabel("Username").fill(USER);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("navigation", { name: "Sections" })).toBeVisible();
  await page.context().storageState({ path: ".auth/real.json" });
});
