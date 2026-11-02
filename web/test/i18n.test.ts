import { test } from "node:test";
import assert from "node:assert/strict";
import { de } from "../src/i18n/de.ts";
import { en } from "../src/i18n/en.ts";
import { detectLang, format } from "../src/i18n/index.ts";

test("both dictionaries have the same keys and no empty text", () => {
  assert.deepEqual(Object.keys(en).sort(), Object.keys(de).sort());
  for (const [k, v] of Object.entries({ ...de })) assert.ok(v.trim() !== "", `de ${k}`);
  for (const [k, v] of Object.entries(en)) assert.ok(v.trim() !== "", `en ${k}`);
});

test("plural keys come in pairs, and both texts carry the same placeholders", () => {
  const keys = Object.keys(de);
  for (const k of keys.filter((x) => x.endsWith(".one"))) assert.ok(keys.includes(k.replace(/\.one$/, ".other")), k);
  for (const k of keys.filter((x) => x.endsWith(".other"))) assert.ok(keys.includes(k.replace(/\.other$/, ".one")), k);
  const holders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(",");
  for (const k of keys) assert.equal(holders(en[k as keyof typeof en]), holders(de[k as keyof typeof de]), k);
});

test("every code of docs/api.md has an error text", () => {
  for (const c of ["unauthorized", "totp_required", "forbidden", "bad_request", "not_found", "conflict", "rate_limited", "jd_offline", "jd_error", "internal", "network", "unknown"]) {
    assert.ok(`err.${c}` in de, c);
  }
});

test("the language follows the browser list, English otherwise", () => {
  assert.equal(detectLang(["de-AT", "en"]), "de");
  assert.equal(detectLang(["fr", "en-GB"]), "en");
  assert.equal(detectLang(["fr"]), "en");
  assert.equal(detectLang([]), "en");
});

test("placeholders are filled, unknown ones stay visible", () => {
  assert.equal(format("{n} Links", { n: 3 }), "3 Links");
  assert.equal(format("{x}"), "{x}");
  assert.equal(format("{x}", { y: 1 }), "{x}");
});
