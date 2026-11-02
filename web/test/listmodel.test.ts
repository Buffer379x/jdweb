import { test } from "node:test";
import assert from "node:assert/strict";
import { buildModel, applyPatch } from "../src/lib/listmodel.ts";
import { pruneSelection, splitSelection, rangeKeys, toggled } from "../src/lib/selection.ts";
import { buildRows, filterWords } from "../src/lib/rows.ts";
import { moveTarget, dropAfter, planDrop } from "../src/lib/move.ts";
import { cleanupCount } from "../src/lib/cleanup.ts";

interface P { id: string; name: string; bytes: number }
interface L { id: string; packageId: string; name: string; bytesLoaded: number; state?: "finished" | "waiting" | "extractError" | "failed"; enabled: boolean; finalState?: string; availability?: string }
const p = (id: string, name = `pkg${id}`, bytes = 0): P => ({ id, name, bytes });
const l = (id: string, packageId: string, name = `file${id}`, extra: Partial<L> = {}): L => ({ id, packageId, name, bytesLoaded: 0, enabled: true, state: "waiting", ...extra });
const sample = () => buildModel<P, L>([p("1"), p("2")], [l("11", "1"), l("12", "1"), l("21", "2"), l("99", "404")]);

test("model keeps order and drops links of unknown packages", () => {
  const m = sample();
  assert.deepEqual(m.order, ["1", "2"]);
  assert.deepEqual(m.children.get("1"), ["11", "12"]);
  assert.equal(m.links.has("99"), false);
});

test("a patch replaces only the touched entries and keeps the others identical", () => {
  const m = sample();
  const other = m.links.get("12");
  const res = applyPatch(m, { links: { "11": { bytesLoaded: 50 } }, packages: { "1": { bytesLoaded: 50 } } });
  assert.equal(m.links.get("11")!.bytesLoaded, 50);
  assert.equal(m.links.get("12"), other);
  assert.deepEqual([...res.changedLinks], ["11"]);
  assert.deepEqual([...res.changedPackages], ["1"]);
  assert.equal(res.unknown, false);
  assert.ok(res.fields.has("bytesLoaded"));
});

test("a patch that changes nothing changes no entry", () => {
  const m = sample();
  const before = m.links.get("11");
  const res = applyPatch(m, { links: { "11": { name: "file11" } } });
  assert.equal(m.links.get("11"), before);
  assert.equal(res.changedLinks.size, 0);
});

test("a patch for an unknown entry asks for a reload and cannot move links", () => {
  const m = sample();
  assert.equal(applyPatch(m, { links: { "777": { name: "x" } } }).unknown, true);
  assert.equal(applyPatch(m, { packages: { "777": { name: "x" } } }).unknown, true);
  applyPatch(m, { links: { "11": { packageId: "2", id: "5" } } });
  assert.equal(m.links.get("11")!.packageId, "1");
  assert.equal(m.links.get("11")!.id, "11");
});

test("selection keeps only entries that still exist", () => {
  const m = sample();
  const sel = new Set(["p1", "l11", "l555", "p9"]);
  const pruned = pruneSelection(sel, m);
  assert.deepEqual([...pruned].sort(), ["l11", "p1"]);
  const all = new Set(["p1", "l11"]);
  assert.equal(pruneSelection(all, m), all, "unchanged selection is the same object");
  assert.deepEqual(splitSelection(pruned), { packageIds: ["1"], linkIds: ["11"] });
});

test("range and toggle", () => {
  assert.deepEqual(rangeKeys(["a", "b", "c", "d"], 3, 1), ["b", "c", "d"]);
  assert.deepEqual(rangeKeys(["a"], -1, 0), []);
  assert.deepEqual([...toggled(new Set(["a"]), "b")], ["a", "b"]);
  assert.deepEqual([...toggled(new Set(["a"]), "a")], []);
});

const opts = (over: Record<string, unknown> = {}) => ({
  expanded: new Set<string>(),
  words: [] as string[],
  textOfPackage: (x: P) => x.name,
  textOfLink: (x: L) => x.name,
  sort: null,
  sortValuePackage: (x: P, c: string) => (c === "size" ? x.bytes : x.name),
  sortValueLink: (x: L, c: string) => (c === "size" ? x.bytesLoaded : x.name),
  ...over,
});

test("collapsed packages show no links, expanded ones do", () => {
  const m = sample();
  assert.deepEqual(buildRows(m, opts()).map((r) => r.key), ["p1", "p2"]);
  assert.deepEqual(buildRows(m, opts({ expanded: new Set(["1"]) })).map((r) => r.key), ["p1", "l11", "l12", "p2"]);
});

test("the filter keeps matching links and opens their package", () => {
  const m = sample();
  const rows = buildRows(m, opts({ words: filterWords("FILE12") }));
  assert.deepEqual(rows.map((r) => r.key), ["p1", "l12"]);
  // A package that matches itself follows the expanded state.
  assert.deepEqual(buildRows(m, opts({ words: filterWords("pkg2") })).map((r) => r.key), ["p2"]);
  assert.deepEqual(buildRows(m, opts({ words: filterWords("nothing") })), []);
  // All words must match.
  assert.deepEqual(buildRows(m, opts({ words: filterWords("file11 file12") })), []);
});

test("sorting orders packages and links, descending reverses", () => {
  const m = buildModel<P, L>([p("1", "b", 5), p("2", "a", 9)], [l("11", "1", "z", { bytesLoaded: 1 }), l("12", "1", "y", { bytesLoaded: 2 })]);
  const ex = new Set(["1"]);
  assert.deepEqual(buildRows(m, opts({ expanded: ex, sort: { col: "name", dir: "asc" } })).map((r) => r.key), ["p2", "p1", "l12", "l11"]);
  assert.deepEqual(buildRows(m, opts({ expanded: ex, sort: { col: "size", dir: "desc" } })).map((r) => r.key), ["p2", "p1", "l12", "l11"]);
  assert.deepEqual(buildRows(m, opts({ expanded: ex, sort: { col: "size", dir: "asc" } })).map((r) => r.key), ["p1", "l11", "l12", "p2"]);
});

test("move targets", () => {
  const s = ["a", "b", "c", "d", "e"];
  const sel = (...x: string[]) => new Set(x);
  assert.equal(moveTarget(s, sel("c"), "top"), "-1");
  assert.equal(moveTarget(s, sel("a"), "top"), null);
  assert.equal(moveTarget(s, sel("a", "b"), "top"), null);
  assert.equal(moveTarget(s, sel("c"), "bottom"), "e");
  assert.equal(moveTarget(s, sel("e"), "bottom"), null);
  assert.equal(moveTarget(s, sel("c"), "up"), "a");
  assert.equal(moveTarget(s, sel("b"), "up"), "-1");
  assert.equal(moveTarget(s, sel("a"), "up"), null);
  assert.equal(moveTarget(s, sel("c"), "down"), "d");
  assert.equal(moveTarget(s, sel("e"), "down"), null);
  assert.equal(moveTarget(s, sel("c", "d"), "down"), "e");
  assert.equal(moveTarget(s, sel("c", "d"), "up"), "a");
  assert.equal(moveTarget(s, sel("zzz"), "up"), null);
});

test("cleanup counts links by action and selection", () => {
  const m = buildModel<P, L>([p("1"), p("2")], [
    l("11", "1", "a", { state: "finished" }),
    l("12", "1", "b", { state: "extractError" }),
    l("21", "2", "c", { state: "finished" }),
    l("22", "2", "d", { enabled: false }),
  ]);
  const none = new Set<string>();
  assert.equal(cleanupCount(m, "DELETE_FINISHED", "ALL", none), 2);
  assert.equal(cleanupCount(m, "DELETE_ALL", "ALL", none), 4);
  assert.equal(cleanupCount(m, "DELETE_DISABLED", "ALL", none), 1);
  // An extraction error may or may not be part of "failed" for JDownloader: the count is unknown then.
  assert.equal(cleanupCount(m, "DELETE_FAILED", "ALL", none), null);
  assert.equal(cleanupCount(m, "DELETE_FINISHED", "SELECTED", new Set(["p1"])), 1);
  assert.equal(cleanupCount(m, "DELETE_FINISHED", "SELECTED", new Set(["l21"])), 1);
  assert.equal(cleanupCount(m, "DELETE_FINISHED", "UNSELECTED", new Set(["p1"])), 1);
  assert.equal(cleanupCount(m, "DELETE_FINISHED", "SELECTED", none), 0);
  assert.equal(cleanupCount(m, "DELETE_DUPE", "ALL", none), null);
});

test("drop targets", () => {
  const s = ["a", "b", "c", "d"];
  const sel = (...x: string[]) => new Set(x);
  assert.equal(dropAfter(s, sel("a"), "c", "after"), "c");
  assert.equal(dropAfter(s, sel("a"), "c", "before"), "b");
  assert.equal(dropAfter(s, sel("d"), "a", "before"), "-1");
  assert.equal(dropAfter(s, sel("d"), "a", "after"), "a");
  assert.equal(dropAfter(s, sel("b"), "c", "before"), null, "dropped where it is");
  assert.equal(dropAfter(s, sel("b"), "a", "after"), null);
  assert.equal(dropAfter(s, sel("b"), "b", "after"), null);
  assert.equal(dropAfter(s, sel("a", "b"), "d", "before"), "c");
});

test("cleanup counts failed and offline links of the downloads", () => {
  const m = buildModel<P, L>([p("1")], [
    l("11", "1", "a", { state: "failed", finalState: "OFFLINE" }),
    l("12", "1", "b", { state: "failed", finalState: "FAILED" }),
    l("13", "1", "c", { state: "finished" }),
    l("14", "1", "d", { state: "waiting", enabled: false }),
  ]);
  const none = new Set<string>();
  assert.equal(cleanupCount(m, "DELETE_FAILED", "ALL", none), 2);
  assert.equal(cleanupCount(m, "DELETE_OFFLINE", "ALL", none), 1);
  assert.equal(cleanupCount(m, "DELETE_FAILED", "SELECTED", new Set(["l13"])), 0);
  assert.equal(cleanupCount(m, "DELETE_DUPE", "ALL", none), null);
});

const kids: Record<string, string[]> = { P1: ["a", "b", "c"], P2: ["x", "y"], P3: [] };
const plan = (over: Record<string, unknown>) =>
  planDrop({
    dragged: "link", ids: new Set(["b"]), target: { kind: "link", id: "c", packageId: "P1" }, pos: "after",
    order: ["P1", "P2", "P3"], children: (p: string) => kids[p] ?? [], targetExpanded: false, ...over,
  } as Parameters<typeof planDrop>[0]);

test("a link dropped where it already is does nothing", () => {
  assert.deepEqual(plan({ target: { kind: "link", id: "a", packageId: "P1" }, pos: "after" }), { kind: "noop" });
  assert.deepEqual(plan({ target: { kind: "link", id: "c", packageId: "P1" }, pos: "before" }), { kind: "noop" });
  assert.deepEqual(plan({ target: { kind: "link", id: "b", packageId: "P1" }, pos: "after" }), { kind: "noop" });
  assert.deepEqual(plan({ ids: new Set(["a"]), target: { kind: "pkg", id: "P1", packageId: "P1" }, targetExpanded: true, pos: "into" }), { kind: "noop" });
});

test("a link dropped within its package moves behind the right sibling", () => {
  assert.deepEqual(plan({ target: { kind: "link", id: "c", packageId: "P1" }, pos: "after" }), { kind: "links", after: "c", destPackageId: "P1" });
  assert.deepEqual(plan({ ids: new Set(["c"]), target: { kind: "link", id: "a", packageId: "P1" }, pos: "before" }), { kind: "links", after: "-1", destPackageId: "P1" });
  assert.deepEqual(plan({ ids: new Set(["c"]), target: { kind: "link", id: "b", packageId: "P1" }, pos: "before" }), { kind: "links", after: "a", destPackageId: "P1" });
});

test("links dropped on another package go to its end when closed, to its top when open", () => {
  const ids = new Set(["a"]);
  assert.deepEqual(plan({ ids, target: { kind: "pkg", id: "P2", packageId: "P2" }, pos: "into", targetExpanded: false }), { kind: "links", after: "y", destPackageId: "P2" });
  assert.deepEqual(plan({ ids, target: { kind: "pkg", id: "P2", packageId: "P2" }, pos: "into", targetExpanded: true }), { kind: "links", after: "-1", destPackageId: "P2" });
  assert.deepEqual(plan({ ids, target: { kind: "pkg", id: "P3", packageId: "P3" }, pos: "into", targetExpanded: false }), { kind: "links", after: "-1", destPackageId: "P3" });
});

test("links dropped on a link of another package land behind or before it", () => {
  assert.deepEqual(plan({ ids: new Set(["a"]), target: { kind: "link", id: "x", packageId: "P2" }, pos: "after" }), { kind: "links", after: "x", destPackageId: "P2" });
  assert.deepEqual(plan({ ids: new Set(["a"]), target: { kind: "link", id: "x", packageId: "P2" }, pos: "before" }), { kind: "links", after: "-1", destPackageId: "P2" });
  assert.deepEqual(plan({ ids: new Set(["a", "b"]), target: { kind: "link", id: "y", packageId: "P2" }, pos: "before" }), { kind: "links", after: "x", destPackageId: "P2" });
});

test("packages go between packages and never onto a link", () => {
  const base = { dragged: "package", ids: new Set(["P3"]) };
  assert.deepEqual(plan({ ...base, target: { kind: "link", id: "a", packageId: "P1" }, pos: "after" }), { kind: "invalid" });
  assert.deepEqual(plan({ ...base, target: { kind: "pkg", id: "P1", packageId: "P1" }, pos: "into" }), { kind: "invalid" });
  assert.deepEqual(plan({ ...base, target: { kind: "pkg", id: "P1", packageId: "P1" }, pos: "before" }), { kind: "packages", after: "-1" });
  assert.deepEqual(plan({ ...base, target: { kind: "pkg", id: "P1", packageId: "P1" }, pos: "after" }), { kind: "packages", after: "P1" });
  assert.deepEqual(plan({ ...base, target: { kind: "pkg", id: "P2", packageId: "P2" }, pos: "after" }), { kind: "noop" });
  assert.deepEqual(plan({ ...base, ids: new Set<string>() }), { kind: "invalid" });
});
