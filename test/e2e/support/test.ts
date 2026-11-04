import { expect, test as base, type Page } from "@playwright/test";
import { Api, connected } from "./api.ts";

/** Everything a page must not do: CSP violations, console errors, requests to anybody but the app. */
export interface Watch {
  csp: string[];
  errors: string[];
  foreign: string[];
}

export const test = base.extend<{ api: Api; watch: Watch }>({
  api: async ({ request, baseURL }, use) => {
    await use(new Api(request, baseURL!));
  },
  watch: async ({ page, baseURL }, use) => {
    const w: Watch = { csp: [], errors: [], foreign: [] };
    const origin = new URL(baseURL!).origin;
    // The CSP report event of the page: the console of Chromium says it, a listener makes it countable.
    await page.addInitScript(() => {
      (window as any).__csp = [];
      document.addEventListener("securitypolicyviolation", (e) => (window as any).__csp.push(`${e.violatedDirective} ${e.blockedURI}`));
    });
    page.on("console", (m) => {
      if (m.type() === "error") w.errors.push(m.text());
    });
    page.on("pageerror", (e) => w.errors.push(String(e)));
    page.on("request", (r) => {
      const u = new URL(r.url());
      if (u.protocol.startsWith("http") && u.origin !== origin) w.foreign.push(r.url());
    });
    await use(w);
    w.csp.push(...(await page.evaluate(() => (window as any).__csp ?? []).catch(() => [])));
  },
});

export { expect };

/** Opens the app on a view and waits for the connection. */
export async function open(page: Page, path = "/"): Promise<void> {
  await page.goto(path);
  await connected(page);
}
