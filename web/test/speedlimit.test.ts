import { test } from "node:test";
import assert from "node:assert/strict";
import { edit, followStatus, formBytes, initialForm, isDirty, switchRequest } from "../src/lib/speedlimit.ts";

test("with no limit ever set, switching on sends the typed default, never 0 (a 400)", () => {
  const form = initialForm(0);
  assert.deepEqual(form, { value: "1", unit: "MB", edited: false });
  assert.deepEqual(switchRequest(true, form, 0), { ok: true, enabled: true, bytesPerSecond: 1048576 });
  assert.deepEqual(switchRequest(true, initialForm(undefined), undefined), { ok: true, enabled: true, bytesPerSecond: 1048576 });
});

test("switching on sends what is typed, off sends only the switch", () => {
  const typed = edit(initialForm(2097152), { value: "300", unit: "KB" });
  assert.deepEqual(switchRequest(true, typed, 2097152), { ok: true, enabled: true, bytesPerSecond: 307200 });
  assert.deepEqual(switchRequest(false, typed, 2097152), { ok: true, enabled: false, bytesPerSecond: 0 });
});

test("a typed value that is no speed: only the switch if a limit is stored, nothing if none is", () => {
  const bad = edit(initialForm(0), { value: "abc" });
  assert.deepEqual(switchRequest(true, bad, 0), { ok: false });
  assert.deepEqual(switchRequest(true, edit(initialForm(51200), { value: "" }), 51200), { ok: true, enabled: true, bytesPerSecond: 0 });
});

test("the form follows the status until the person edits it", () => {
  let form = initialForm(51200);
  assert.deepEqual(form, { value: "50", unit: "KB", edited: false });
  form = followStatus(form, 3 * 1048576); // another browser changed the limit
  assert.deepEqual(form, { value: "3", unit: "MB", edited: false });
  form = edit(form, { value: "7" });
  assert.deepEqual(followStatus(form, 1048576), form, "an edited form is left alone");
  assert.equal(form.edited, true);
});

test("every limit JDownloader can hold goes round the form without making it dirty", () => {
  for (const bytes of [1, 1000, 1500, 51200, 123457, 1048576, 1572864, 5 * 1048576, 99999999, 2147483647]) {
    const form = initialForm(bytes);
    assert.equal(formBytes(form), bytes, `${bytes} comes back from its own text`);
    assert.equal(isDirty(form, bytes), false, `${bytes}: not dirty without an edit`);
    // Even the same text typed again is no change.
    assert.equal(isDirty(edit(form, { value: form.value }), bytes), false, `${bytes}: typed again is no change`);
  }
});

test("dirty means a valid value that differs", () => {
  const base = 1048576;
  assert.equal(isDirty(edit(initialForm(base), { value: "2" }), base), true);
  assert.equal(isDirty(edit(initialForm(base), { value: "x" }), base), false);
  assert.equal(isDirty(edit(initialForm(base), { value: "1024", unit: "KB" }), base), false);
  assert.equal(isDirty(edit(initialForm(0), { value: "1" }), 0), true, "no limit stored: a first limit is something to save");
});
