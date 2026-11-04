import { test } from "node:test";
import assert from "node:assert/strict";
import { applyAutoStart, autoStartFrom, AUTO_START, LINKGRABBER } from "../src/lib/autostart.ts";

const entry = (value: unknown, storage = "") => ({ interface: LINKGRABBER, storage, key: AUTO_START, value }) as never;

test("the switch is read from the config answer", () => {
  assert.equal(autoStartFrom({ entries: [entry(true)] }), true);
  assert.equal(autoStartFrom({ entries: [entry(false)] }), false);
  assert.equal(autoStartFrom({ entries: [entry(true, "other")] }), null);
  assert.equal(autoStartFrom({}), null);
});

test("the switch moves only after JDownloader accepted the change", async () => {
  const sent: unknown[] = [];
  const shown: boolean[] = [];
  const errors: unknown[] = [];
  assert.equal(await applyAutoStart(async (p, b) => void sent.push([p, b]), (v) => shown.push(v), (e) => errors.push(e), false), true);
  assert.deepEqual(sent, [["/config", { interface: LINKGRABBER, storage: "", key: AUTO_START, value: false }]]);
  assert.deepEqual(shown, [false]);

  const boom = new Error("jd offline");
  assert.equal(await applyAutoStart(async () => { throw boom; }, (v) => shown.push(v), (e) => errors.push(e), true), false);
  assert.deepEqual(shown, [false], "a refused change does not move the switch");
  assert.deepEqual(errors, [boom], "and the error is shown");
});
