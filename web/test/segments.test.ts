import { test } from "node:test";
import assert from "node:assert/strict";
import { segments, summarize, linkFill, MAX_SEGMENTS, type SegLink } from "../src/lib/segments.ts";

const fin = (): SegLink => ({ state: "finished", bytesLoaded: 10, bytesTotal: 10 });
const run = (p: number): SegLink => ({ state: "running", bytesLoaded: p, bytesTotal: 100 });
const wait = (): SegLink => ({ state: "waiting", bytesLoaded: 0, bytesTotal: 100 });
const err = (): SegLink => ({ state: "extractError", bytesLoaded: 10, bytesTotal: 10 });
const failed = (): SegLink => ({ state: "failed", bytesLoaded: 0, bytesTotal: 10 });

test("one segment per link up to 24", () => {
  assert.equal(segments([]).length, 0);
  const five = [fin(), fin(), run(62), run(18), wait()];
  const s = segments(five);
  assert.equal(s.length, 5);
  assert.deepEqual(s[0], { fill: 1, kind: "done" });
  assert.deepEqual(s[2], { fill: 0.62, kind: "run" });
  assert.deepEqual(s[4], { fill: 0, kind: "none" });
});

test("24 links are 24 segments, 25 links are grouped into 24", () => {
  const l24 = Array.from({ length: 24 }, fin);
  assert.equal(segments(l24).length, 24);
  assert.equal(MAX_SEGMENTS, 24);
  const l25 = Array.from({ length: 25 }, fin);
  assert.equal(segments(l25).length, 24);
  const l10000 = Array.from({ length: 10000 }, wait);
  assert.equal(segments(l10000).length, 24);
});

test("groups cover every link exactly once", () => {
  // 30 links: the 7th link is the only error. It must color exactly one segment.
  const links = Array.from({ length: 30 }, wait);
  links[6] = err();
  const s = segments(links);
  assert.equal(s.filter((x) => x.kind === "err").length, 1);
  assert.equal(s.length, 24);
  // Every link falls into exactly one group: all-finished input gives all-full segments.
  assert.ok(segments(Array.from({ length: 31 }, fin)).every((x) => x.fill === 1 && x.kind === "done"));
});

test("an error in a range makes the whole segment an error and full", () => {
  const links = [...Array.from({ length: 49 }, fin), err()];
  const s = segments(links);
  assert.equal(s.length, 24);
  assert.deepEqual(s[23], { fill: 1, kind: "err" });
  assert.equal(s[0].kind, "done");
  // Error wins over running in the same group.
  const mixed = [...Array.from({ length: 24 }, wait), run(50)];
  mixed[24] = err();
  assert.equal(segments(mixed)[23].kind, "err");
});

test("running wins over done, skipped is wait colored", () => {
  const s = segments([...Array.from({ length: 25 }, fin).slice(0, 23), run(50), { state: "skipped", bytesLoaded: 0, bytesTotal: 5 }]);
  assert.equal(s.length, 24);
  assert.equal(s[23].kind, "run");
  assert.equal(segments([{ state: "skipped", bytesLoaded: 0, bytesTotal: 5 }])[0].kind, "wait");
});

test("partial progress without running is wait colored, disabled stays empty", () => {
  assert.deepEqual(segments([{ state: "waiting", bytesLoaded: 40, bytesTotal: 100 }])[0], { fill: 0.4, kind: "wait" });
  assert.deepEqual(segments([{ state: "disabled", bytesLoaded: 40, bytesTotal: 100 }])[0], { fill: 0, kind: "none" });
});

test("fill is clamped and tolerates zero totals", () => {
  assert.equal(linkFill({ state: "running", bytesLoaded: 500, bytesTotal: 100 }), 1);
  assert.equal(linkFill({ state: "running", bytesLoaded: 5, bytesTotal: 0 }), 0);
  assert.equal(linkFill({ state: "finished", bytesLoaded: 0, bytesTotal: 0 }), 1);
});

test("summary counts states for the label", () => {
  const s = summarize([{ state: "finished" }, { state: "finished" }, { state: "finished" }, { state: "extractError" }, { state: "waiting" }]);
  assert.deepEqual(s, { total: 5, finished: 3, errors: 1, running: 0, skipped: 0, disabled: 0, waiting: 1 });
});

test("a failed link colors its segment as an error, like an extraction error", () => {
  assert.deepEqual(segments([failed()])[0], { fill: 1, kind: "err" });
  const links = [...Array.from({ length: 30 }, wait)];
  links[10] = failed();
  const s = segments(links);
  assert.equal(s.filter((x) => x.kind === "err").length, 1);
  assert.deepEqual(summarize([{ state: "failed" }, { state: "extractError" }, { state: "finished" }]).errors, 2);
});
