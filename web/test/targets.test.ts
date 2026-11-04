import { test } from "node:test";
import assert from "node:assert/strict";
import { canChangeDirectory } from "../src/lib/targets.ts";

test("the save-to directory belongs to packages: a selection of links alone cannot change it", () => {
  assert.equal(canChangeDirectory({ packageIds: [], linkIds: ["1", "2"] }), false);
  assert.equal(canChangeDirectory({ packageIds: [], linkIds: [] }), false);
  assert.equal(canChangeDirectory({ packageIds: ["7"], linkIds: [] }), true);
  assert.equal(canChangeDirectory({ packageIds: ["7"], linkIds: ["1"] }), true);
});
