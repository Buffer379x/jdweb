import AxeBuilder from "@axe-core/playwright";
import { expect, type Page } from "@playwright/test";

/** axe with the WCAG 2.1 A and AA rules. Serious and critical findings fail; the rest is logged. */
export async function axe(page: Page, what: string): Promise<void> {
  const r = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
  const bad = r.violations.filter((v) => v.impact === "serious" || v.impact === "critical");
  const minor = r.violations.filter((v) => !bad.includes(v));
  if (minor.length) console.log(`axe (${what}), not serious: ${minor.map((v) => `${v.id}(${v.nodes.length})`).join(", ")}`);
  expect(
    bad.map((v) => `${v.id} [${v.impact}]: ${v.nodes.slice(0, 3).map((n) => n.target.join(" ") + " :: " + (n.any[0]?.message ?? n.failureSummary?.split("\n")[1] ?? "")).join(" | ")}`),
    `axe on ${what}`,
  ).toEqual([]);
}
