import { expect, open, test } from "../../support/test.ts";
import { axe } from "../../support/a11y.ts";
import { fx } from "../../support/api.ts";

const VIEWS = [
  "/downloads", "/linkgrabber", "/settings/general", "/settings/accounts", "/settings/basicauth", "/settings/plugins",
  "/settings/extensions", "/settings/expert", "/settings/system", "/settings/account", "/settings/diagnostics",
];
const SCHEMES = ["light", "dark"] as const;
const WIDTHS = [390, 768, 1280, 1920];

test.beforeEach(async ({ api }) => {
  await api.wipe();
  // Something to look at in both lists: a download package, a linkgrabber package with an offline link.
  await api.stage("views-dl", [fx("/e2e-views-1.bin"), fx("/e2e-views-2.bin")]);
  await api.add("views-lg", [fx("/e2e-views-3.bin"), fx("/offline/e2e-views-off.bin")]);
  await api.waitGrabber(2);
  await expect.poll(async () => (await api.grabber()).links.some((l) => l.availability === "OFFLINE"), { timeout: 60_000 }).toBe(true);
});

const sideways = (page: import("@playwright/test").Page) => page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);

// T-UI-05: axe on every view, light and dark. T-UI-04: widths. E2, T-SEC-09, T-SEC-10 are watched on the way.
test("axe: every view and dialog, light and dark; no sideways page scroll at four widths; no CSP violation, no third party", async ({ page, watch }) => {
  test.setTimeout(600_000);
  for (const scheme of SCHEMES) {
    await page.emulateMedia({ colorScheme: scheme, reducedMotion: "no-preference" });
    for (const v of VIEWS) {
      await page.setViewportSize({ width: 1280, height: 800 });
      await open(page, v);
      await page.waitForLoadState("networkidle");
      if (v === "/settings/expert") await expect(page.locator("main, .settings").first()).toBeVisible();
      await axe(page, `${v} ${scheme}`);
      for (const w of WIDTHS) {
        await page.setViewportSize({ width: w, height: 800 });
        expect(await sideways(page), `${v} at ${w} px ${scheme}`).toBe(false);
        // The phone layout is another layout: axe sees it too.
        if (w === 390 && ["/downloads", "/linkgrabber", "/settings/general", "/settings/diagnostics"].includes(v)) await axe(page, `${v} at 390 px ${scheme}`);
      }
    }
    // Dialogs, menus and panels.
    await page.setViewportSize({ width: 1280, height: 800 });
    await open(page, "/downloads");
    const modal = async (what: string, opener: () => Promise<void>, closer = "Escape") => {
      await opener();
      await expect(page.locator('[role="dialog"], [role="alertdialog"], [role="menu"]').first()).toBeVisible();
      await page.waitForTimeout(300);
      await axe(page, `${what} ${scheme}`);
      for (const w of [390, 1920]) {
        await page.setViewportSize({ width: w, height: 800 });
        expect(await sideways(page), `${what} at ${w} px ${scheme}`).toBe(false);
      }
      await page.setViewportSize({ width: 1280, height: 800 });
      await page.keyboard.press(closer);
      await expect(page.locator('[role="dialog"], [role="alertdialog"], [role="menu"]')).toHaveCount(0);
    };
    await modal("add links", () => page.getByRole("button", { name: "Add links", exact: true }).click());
    await modal("cleanup", () => page.getByRole("button", { name: "Clean up" }).click());
    await modal("help", () => page.keyboard.press("?"));
    await modal("user menu", () => page.getByRole("button", { name: "Menu" }).click());
    await modal("speed limit", async () => {
      await page.getByRole("button", { name: "Menu" }).click();
      await page.getByRole("menuitem", { name: "Change speed limit …" }).last().click();
    });
    await page.getByRole("row", { name: /views-dl/ }).click();
    await modal("actions menu", () => page.getByRole("button", { name: "Actions" }).click());
    await page.getByRole("button", { name: "Details" }).click();
    await axe(page, `details ${scheme}`);
    await page.getByRole("button", { name: "Details" }).click();
    await page.getByRole("row", { name: /views-dl/ }).getByRole("button", { name: "Expand" }).click();
    await axe(page, `expanded package ${scheme}`);
  }
  expect(watch.csp, "CSP violations").toEqual([]);
  expect(watch.foreign, "requests to third parties").toEqual([]);
  expect(watch.errors.filter((e) => !/Failed to load resource/.test(e)), "console errors").toEqual([]);
});

// T-UI-07: with prefers-reduced-motion nothing pulses and nothing animates.
test("prefers-reduced-motion: no animation or transition runs", async ({ page, api }) => {
  await api.limit(60_000);
  await open(page, "/downloads");
  await api.post("/controller/start");
  await expect(page.locator(".speed b")).not.toHaveText(/^0\s/, { timeout: 30_000 });
  const motion = () => page.evaluate(() => {
    const running = document.getAnimations().filter((a) => a.playState === "running" && (a.effect?.getTiming().iterations ?? 1) !== 1);
    const longTransitions = [...document.querySelectorAll<HTMLElement>("*")].filter((e) => {
      const cs = getComputedStyle(e);
      return cs.animationName !== "none" && cs.animationDuration !== "0s" && cs.animationIterationCount !== "1";
    }).map((e) => `${e.tagName}.${e.className}`);
    return { infinite: running.length, names: longTransitions.slice(0, 5) };
  });
  await page.emulateMedia({ reducedMotion: "no-preference" });
  const before = await motion();
  console.log("T-UI-07 with motion allowed:", JSON.stringify(before));
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.waitForTimeout(300);
  expect(await motion(), "nothing loops with reduced motion").toEqual({ infinite: 0, names: [] });
  const anim = await page.evaluate(() => [...document.querySelectorAll<HTMLElement>("*")].filter((e) => {
    const cs = getComputedStyle(e);
    return cs.animationName !== "none" && parseFloat(cs.animationDuration) > 0.011;
  }).length);
  expect(anim, "no running CSS animation of real length").toBe(0);
});

// T-SEC-09: the headers of the app are complete.
test("security headers on the page and on the API", async ({ request }) => {
  for (const path of ["/", "/api/v1/session", "/assets/"]) {
    const r = await request.get(path);
    const h = r.headers();
    expect(h["content-security-policy"], path).toContain("default-src 'none'");
    expect(h["content-security-policy"], path).toContain("frame-ancestors 'none'");
    expect(h["x-content-type-options"], path).toBe("nosniff");
    expect(h["referrer-policy"], path).toBeTruthy();
    expect(h["x-frame-options"] ?? h["content-security-policy"], path).toBeTruthy();
  }
});
