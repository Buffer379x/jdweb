import "./dom-stubs.ts";
import { test, mock } from "node:test";
import assert from "node:assert/strict";
import { flush, json, stubFetch } from "./dom-stubs.ts";

const { ListStore } = await import("../src/store/list.ts");

interface P { id: string; name: string; hosts: string[]; childCount: number; bytesTotal: number; enabled: boolean; saveTo: string; priority: string; comment: string; downloadPassword: string }
interface L { id: string; packageId: string; name: string; host: string; url: string; bytesTotal: number; bytesLoaded: number; enabled: boolean; priority: string; comment: string; downloadPassword: string }
const pkg = (id: string, name = `p${id}`): P => ({ id, name, hosts: [], childCount: 0, bytesTotal: 0, enabled: true, saveTo: "", priority: "", comment: "", downloadPassword: "" });
const lnk = (id: string, packageId: string, name = `l${id}`): L => ({ id, packageId, name, host: "h", url: "", bytesTotal: 100, bytesLoaded: 0, enabled: true, priority: "", comment: "", downloadPassword: "" });

function newStore() {
  return new ListStore<P, L>({
    name: "downloads",
    textOfPackage: (p) => p.name,
    textOfLink: (l) => l.name,
    sortValuePackage: (p) => p.name,
    sortValueLink: (l, c) => (c === "size" ? l.bytesTotal : l.name),
    filterFields: new Set(["name", "host"]),
    sortFields: (c) => new Set(c === "size" ? ["bytesTotal"] : ["name"]),
  });
}

const list = (packages: P[], links: L[]) => () => json({ packages, links });

test("a patch that arrives while the list loads waits and is applied after the load", async () => {
  let release!: () => void;
  const gate = new Promise<void>((r) => (release = r));
  stubFetch({ "/downloads": async () => { await gate; return json({ packages: [pkg("1")], links: [lnk("11", "1")] }); } });
  const s = newStore();
  const loading = s.load();
  assert.equal(s.patch({ list: "downloads", links: { "11": { bytesLoaded: 50 } }, packages: {} }), true);
  assert.equal(s.loaded.value, false);
  release();
  await loading;
  assert.equal(s.loaded.value, true);
  assert.equal(s.model.links.get("11")!.bytesLoaded, 50, "the queued patch was applied after the load");
  // The entry signal follows the model.
  assert.equal(s.link("11")!.value.bytesLoaded, 50);
});

test("patches for a list that was never loaded are dropped without a reload", async () => {
  stubFetch({});
  const s = newStore();
  assert.equal(s.patch({ list: "downloads", links: { "1": { name: "x" } }, packages: {} }), true);
  assert.equal(s.model.links.size, 0);
});

test("a patch changes only the signal of its own entry", async () => {
  stubFetch({ "/downloads": list([pkg("1")], [lnk("11", "1"), lnk("12", "1")]) });
  const s = newStore();
  await s.load();
  const a = s.link("11")!, b = s.link("12")!;
  const before = b.value;
  s.patch({ list: "downloads", links: { "11": { bytesLoaded: 7 } }, packages: {} });
  assert.equal(a.value.bytesLoaded, 7);
  assert.equal(b.value, before, "the other row keeps its object, so it is not drawn again");
});

test("a reload keeps what still exists and drops the rest of selection, focus and expanded", async () => {
  const data = { packages: [pkg("1"), pkg("2")], links: [lnk("11", "1"), lnk("21", "2")] };
  stubFetch({ "/downloads": () => json(data) });
  const s = newStore();
  await s.load();
  s.selection.value = new Set(["p1", "l11", "l21", "p2"]);
  s.focusKey.value = "l21";
  s.anchorKey.value = "p2";
  s.expanded.value = new Set(["1", "2"]);
  // JDownloader removed package 2 and link 11 meanwhile.
  data.packages = [pkg("1")];
  data.links = [lnk("12", "1")];
  await s.load();
  assert.deepEqual([...s.selection.value], ["p1"]);
  assert.equal(s.focusKey.value, null);
  assert.equal(s.anchorKey.value, null);
  assert.deepEqual([...s.expanded.value], ["1"]);
});

test("a reload that changes nothing keeps the objects of the entries", async () => {
  stubFetch({ "/downloads": () => json({ packages: [pkg("1")], links: [lnk("11", "1")] }) });
  const s = newStore();
  await s.load();
  const before = s.model.links.get("11");
  const sig = s.link("11")!;
  await s.load();
  assert.equal(s.model.links.get("11"), before);
  assert.equal(sig.value, before);
});

test("reset ends a load that is still on its way and clears the pending reload", async () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const log = stubFetch({ "/downloads": async () => { await gate; return json({ packages: [pkg("1")], links: [] }); } });
    const s = newStore();
    const loading = s.load();
    s.reloadSoon(300);
    s.reset();
    release();
    await loading;
    assert.equal(s.model.pkgs.size, 0, "the answer of the old session is not used");
    assert.equal(s.loaded.value, false);
    mock.timers.tick(1000);
    await flush();
    assert.equal(log.filter((x) => x === "GET /downloads").length, 1, "the pending reload did not fire");
  } finally {
    mock.timers.reset();
  }
});

test("a patch for an entry the list does not know asks for one reload", async () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const log = stubFetch({ "/downloads": list([pkg("1")], [lnk("11", "1")]) });
    const s = newStore();
    await s.load();
    s.patch({ list: "downloads", links: { "999": { name: "x" } }, packages: {} });
    s.patch({ list: "downloads", links: { "998": { name: "y" } }, packages: {} });
    mock.timers.tick(400);
    await flush();
    assert.equal(log.filter((x) => x === "GET /downloads").length, 2, "the first load and one reload");
  } finally {
    mock.timers.reset();
  }
});

test("filter and sort look only at the fields that matter, progress does not rebuild the rows", async () => {
  stubFetch({ "/downloads": list([pkg("1")], [lnk("11", "1"), lnk("12", "1", "other")]) });
  const s = newStore();
  await s.load();
  const progress = () => s.patch({ list: "downloads", links: { "11": { bytesLoaded: Math.random() * 100 + 1 } }, packages: {} });

  const v0 = s.viewVer.value;
  s.filter.value = "l1";
  progress();
  assert.equal(s.viewVer.value, v0, "filter: progress is not in the filter text");
  s.patch({ list: "downloads", links: { "11": { name: "renamed" } }, packages: {} });
  assert.equal(s.viewVer.value, v0 + 1, "filter: a new name is");

  s.filter.value = "";
  s.sort.value = { col: "size", dir: "asc" };
  const v1 = s.viewVer.value;
  progress();
  assert.equal(s.viewVer.value, v1, "sort by size: progress does not move rows");
  s.patch({ list: "downloads", links: { "11": { bytesTotal: 5 } }, packages: {} });
  assert.equal(s.viewVer.value, v1 + 1, "sort by size: a new size does");

  s.sort.value = null;
  const v2 = s.viewVer.value;
  s.patch({ list: "downloads", links: { "11": { name: "again" } }, packages: {} });
  assert.equal(s.viewVer.value, v2, "no filter and no sort: the order never depends on a patch");
});

test("the setup status tells a password reset from the first setup", async () => {
  const app = await import("../src/store/app.ts");
  stubFetch({ "/setup/status": () => json({ required: true, reset: true }) });
  await app.checkSession();
  assert.equal(app.setupRequired.value, true);
  assert.equal(app.setupReset.value, true);
  stubFetch({ "/setup/status": () => json({ required: true }) });
  await app.checkSession();
  assert.equal(app.setupReset.value, false);
});
