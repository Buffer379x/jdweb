import { expect, open, test } from "../../support/test.ts";
import { fx } from "../../support/api.ts";
import { PREFIX } from "../../support/env.ts";

test.beforeEach(async ({ api }) => {
  await api.wipe();
});

// T-UI-03 (all shortcuts work and do not collide with input fields) and T-UI-06 (core flows by keyboard only, focus visible).
test("keyboard only: shortcuts, no collision with text fields, rename, remove, add, start and pause", async ({ page, api }) => {
  await api.limit(60_000);
  await api.stage("kb-one", [fx("/e2e-kb-1.bin")]);
  await api.stage("kb-two", [fx("/e2e-kb-2.bin")]);
  await open(page, "/downloads");
  await expect(page.getByRole("row", { name: /jdweb-e2e-kb-one/ })).toBeVisible();
  const key = (k: string) => page.keyboard.press(k);
  const dialogs = page.locator('[role="dialog"], [role="alertdialog"]');

  // The overview of the keys.
  await key("?");
  await expect(page.getByRole("dialog", { name: "Keyboard shortcuts" })).toBeVisible();
  await key("Escape");
  await expect(dialogs).toHaveCount(0);

  // 1, 2, 3 switch the views.
  const current = () => page.getByRole("navigation", { name: "Sections" }).locator('[aria-current="page"]');
  await key("2");
  await expect(current()).toContainText("Linkgrabber");
  await key("3");
  await expect(current()).toContainText("Settings");
  await key("1");
  await expect(current()).toContainText("Downloads");

  // "/" focuses the filter, and what is typed there is not a shortcut: 1, 2, a, ? stay letters.
  await key("/");
  await expect(page.getByLabel("Filter")).toBeFocused();
  await page.keyboard.type("a12? kb-one");
  await expect(page.getByLabel("Filter")).toHaveValue("a12? kb-one");
  await expect(current()).toContainText("Downloads");
  await expect(dialogs).toHaveCount(0);
  await page.getByLabel("Filter").fill("");

  // Into the list with Tab, along the rows with the arrows, Space selects, Right expands.
  await page.getByLabel("Filter").focus();
  for (let i = 0; i < 6 && !(await page.locator('[role="grid"]:focus, [role="treegrid"]:focus, .list-wrap :focus').count()); i++) await key("Tab");
  await key("ArrowDown");
  await key("ArrowDown");
  await key(" ");
  const selected = page.getByRole("row", { selected: true });
  await expect(selected).toHaveCount(1);
  await key("ArrowRight");
  await expect(selected).toHaveAttribute("aria-expanded", "true");

  // F2 renames: typing "a" and digits into the name field are letters, Enter confirms.
  await key("F2");
  const rename = page.getByRole("dialog");
  await expect(rename.getByRole("textbox")).toBeFocused();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type(PREFIX + "kb-renamed-a1");
  await key("Enter");
  await expect(dialogs).toHaveCount(0);
  await expect.poll(async () => (await api.downloads()).packages.map((p) => p.name).sort()).toContain(PREFIX + "kb-renamed-a1");

  // Delete asks first; Escape says no, and nothing is removed.
  await key("Delete");
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await expect(page.locator('[role="alertdialog"] :focus'), "focus moves into the dialog").toHaveCount(1);
  await key("Escape");
  await expect(dialogs).toHaveCount(0);
  expect((await api.downloads()).packages).toHaveLength(2);

  // "a" adds links: the letters typed in the text field stay letters; Ctrl/Cmd+Enter sends.
  await key("Escape");
  await page.locator("body").press("a");
  const add = page.getByRole("dialog");
  await expect(add.getByLabel("Links")).toBeFocused();
  await page.keyboard.type(fx("/e2e-kb-added.bin"));
  await expect(add.getByLabel("Links")).toHaveValue(fx("/e2e-kb-added.bin"));
  await key("Tab");
  await key("Tab");
  await page.keyboard.type(PREFIX + "kb-added");
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(dialogs).toHaveCount(0);
  await expect.poll(async () => (await api.grabber()).links.length, { timeout: 60_000 }).toBe(1);

  // Shift+S starts, Shift+P pauses and resumes, Shift+S stops.
  const controller = async () => (await api.status()).controller;
  await key("Shift+S");
  await expect.poll(controller, { timeout: 10_000 }).toBe("RUNNING");
  await key("Shift+P");
  await expect.poll(controller, { timeout: 10_000 }).toBe("PAUSE");
  await key("Shift+P");
  await expect.poll(controller, { timeout: 10_000 }).toBe("RUNNING");
  await key("Shift+S");
  await expect.poll(controller, { timeout: 15_000 }).toBe("IDLE");
});

test("focus is visible on everything that takes the keyboard", async ({ page, api }) => {
  await api.stage("kb-focus", [fx("/e2e-kbf.bin")]);
  await open(page, "/downloads");
  await page.locator("body").press("Tab");
  const seen: string[] = [];
  for (let i = 0; i < 18; i++) {
    const info = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement | null;
      if (!el || el === document.body) return null;
      const cs = getComputedStyle(el);
      // Own outline, or a box shadow ring, or the ring of a parent that wraps it (:focus-within).
      const outline = cs.outlineStyle !== "none" && parseFloat(cs.outlineWidth) > 0;
      const shadow = cs.boxShadow !== "none";
      const within = !!el.closest(":focus-within") && getComputedStyle(el.parentElement!).boxShadow !== "none";
      const bg = cs.backgroundColor;
      return { name: (el.getAttribute("aria-label") || el.textContent || el.tagName).trim().slice(0, 30), visible: outline || shadow || within, tag: el.tagName, bg };
    });
    if (info) {
      seen.push(`${info.tag}:${info.name}`);
      expect(info.visible, `focus ring on ${info.tag} "${info.name}"`).toBe(true);
    }
    await page.keyboard.press("Tab");
  }
  console.log("T-UI-06 focus order:", seen.join(" > "));
  expect(seen.length).toBeGreaterThan(8);
});
