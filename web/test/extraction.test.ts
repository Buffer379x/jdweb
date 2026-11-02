import { test } from "node:test";
import assert from "node:assert/strict";
import { canCancel, jobState, partCounts, partList } from "../src/lib/extraction.ts";

test("parts are counted by state", () => {
  assert.deepEqual(partCounts({ a: "COMPLETE", b: "COMPLETE", c: "INCOMPLETE", d: "MISSING" }), { complete: 2, total: 4, missing: 1 });
  assert.deepEqual(partCounts(null), { complete: 0, total: 0, missing: 0 });
  assert.deepEqual(partCounts(undefined), { complete: 0, total: 0, missing: 0 });
});

test("the controller status becomes a job state", () => {
  assert.equal(jobState("RUNNING"), "running");
  assert.equal(jobState("queued"), "queued");
  assert.equal(jobState("NA"), "idle");
  assert.equal(jobState(""), "idle");
});

test("only a running controller can be cancelled", () => {
  assert.equal(canCancel({ controllerId: 41 }), true);
  assert.equal(canCancel({ controllerId: 0 }), true);
  assert.equal(canCancel({ controllerId: -1 }), false);
});

test("parts are listed in natural order", () => {
  assert.deepEqual(partList({ "a.part10.rar": "COMPLETE", "a.part2.rar": "INCOMPLETE", "a.part1.rar": "COMPLETE" }).map((p) => p.name), ["a.part1.rar", "a.part2.rar", "a.part10.rar"]);
});
