import { test } from "node:test";
import assert from "node:assert/strict";
import { dismissal } from "../src/lib/dismiss.ts";
import { CLEANUP_ACTIONS } from "../src/lib/cleanup.ts";

test("a submit in flight is not interrupted by Escape", () => {
  assert.equal(dismissal(true, true), "ignore");
  assert.equal(dismissal(false, true), "ignore");
});

test("typed input is asked about, an untouched dialog closes", () => {
  assert.equal(dismissal(true, false), "ask");
  assert.equal(dismissal(false, false), "close");
});

test("the downloads cleanup offers offline links, the linkgrabber too", () => {
  assert.ok(CLEANUP_ACTIONS.downloads.includes("DELETE_OFFLINE"));
  assert.ok(CLEANUP_ACTIONS.linkgrabber.includes("DELETE_OFFLINE"));
});
