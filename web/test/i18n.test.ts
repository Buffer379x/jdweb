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

import { readFileSync } from "node:fs";

test("every message key of the diagnostics of the server has a text in both languages", () => {
  const src = readFileSync(new URL("../../internal/diag/diag.go", import.meta.url), "utf8");
  const keys = new Set([...src.matchAll(/"(diag\.[\w.-]+)"/g)].map((m) => m[1]!));
  assert.ok(keys.size >= 12, `found ${keys.size} keys`);
  assert.ok(keys.has("diag.jd-outside-subnet.outside-private") && keys.has("diag.api-no-login.warn"));
  for (const k of keys) {
    assert.ok(k in de, `de ${k}`);
    assert.ok(k in en, `en ${k}`);
  }
  for (const id of ["api-reachable", "jd-revision", "api-no-login", "jd-outside-subnet"]) assert.ok(`diag.title.${id}` in de, id);
});

test("the texts built from a name exist for every name", () => {
  const sets: [string, string[]][] = [
    ["sys.action.", ["restart", "exit", "shutdown", "hibernate", "standby"]],
    ["sys.confirm.", ["restart", "exit", "shutdown", "hibernate", "standby"]],
    ["sys.consequence.", ["restart", "exit", "shutdown", "hibernate", "standby"]],
    ["sys.sent.", ["restart", "exit", "shutdown", "hibernate", "standby"]],
    ["cap.skip.", ["SINGLE", "BLOCK_HOSTER", "BLOCK_PACKAGE", "BLOCK_ALL_CAPTCHAS", "REFRESH", "STOP_CURRENT_ACTION", "TIMEOUT"]],
    ["conn.source.", ["ui", "env", "default"]],
    ["acc.err.", ["INVALID", "TEMP_ERROR", "PLUGIN_ERROR", "IO_ERROR"]],
  ];
  for (const [prefix, names] of sets) for (const n of names) assert.ok(`${prefix}${n}` in de && `${prefix}${n}` in en, `${prefix}${n}`);
  for (const n of ["SINGLE", "BLOCK_HOSTER", "BLOCK_PACKAGE", "BLOCK_ALL_CAPTCHAS", "REFRESH", "STOP_CURRENT_ACTION", "TIMEOUT"]) assert.ok(`cap.skip.${n}.effect` in de, n);
});
