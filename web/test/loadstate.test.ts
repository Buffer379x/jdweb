import { test } from "node:test";
import assert from "node:assert/strict";
import { begin, failed, initial, loaded, view, type Snap } from "../src/lib/loadstate.ts";

// The sequence the settings layout goes through when the same page component is reused for another extension.
test("a new path never shows the data of the old one, and its failure is shown", () => {
  const A = "/config?interface=A", B = "/config?interface=B";
  let s: Snap<string[]> = initial(A);
  s = loaded(begin(s, A), A, ["a1", "a2"]);
  assert.deepEqual(view(s, A).data, ["a1", "a2"]);

  // The page now asks for B. In this very render there is no state for B yet.
  assert.equal(view(s, B).data, undefined, "A's data must not be visible under B");
  assert.equal(view(s, B).loading, true);
  s = begin(s, B);
  assert.equal(view(s, B).data, undefined);

  // B fails: the error is there, and no data hides it.
  s = failed(s, B, new Error("jd offline"));
  assert.equal(view(s, B).data, undefined);
  assert.ok(view(s, B).error, "the error of B is visible");

  // Back to A: A is not shown from B's leftovers either.
  assert.equal(view(s, A).data, undefined);
});

test("a reload of the same path keeps the data, and a failed reload keeps both", () => {
  const P = "/accounts";
  let s: Snap<number[]> = loaded(begin(initial(P), P), P, [1, 2]);
  s = begin(s, P);
  assert.deepEqual(view(s, P).data, [1, 2], "kept while it reloads");
  assert.equal(view(s, P).loading, true);
  assert.equal(view(s, P).error, undefined);
  s = failed(s, P, new Error("502"));
  assert.deepEqual(view(s, P).data, [1, 2], "the old values stay on screen");
  assert.ok(view(s, P).error, "and the page can say they may be out of date");
  s = loaded(begin(s, P), P, [3]);
  assert.deepEqual(view(s, P).data, [3]);
  assert.equal(view(s, P).error, undefined);
});

test("a null path loads nothing", () => {
  const s = initial<string>(null);
  assert.equal(view(s, null).loading, false);
  assert.equal(view(s, null).data, undefined);
});
