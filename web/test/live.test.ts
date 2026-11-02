import "./dom-stubs.ts";
import { test, mock, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { FakeEventSource, fakeDocument, flush, json, stubFetch } from "./dom-stubs.ts";

const app = await import("../src/store/app.ts");
const live = await import("../src/store/live.ts");
const { stores } = await import("../src/store/stores.ts");

const status = (connection = "connected") => ({
  connection, connectionDetail: "", controller: "IDLE", speed: 0,
  downloads: { packages: 1, links: 1, bytesTotal: 100, bytesLoaded: 0, running: 0, eta: null },
  linkgrabber: { packages: 0, links: 0, bytesTotal: 0, crawling: false }, captchas: 0, dialogs: 0,
  toggles: { limit: false, limitBytes: 0, reconnect: false, premium: false, clipboard: false, stopAfter: false },
  jd: { revision: 1, docker: true, url: "http://jd" },
});
const dlLink = (over = {}) => ({ id: "11", packageId: "1", name: "a", host: "h", url: "", bytesTotal: 100, bytesLoaded: 0, speed: 0, eta: null, enabled: true, finished: false, running: false, skipped: false, skipReason: "", finalState: "", extractionStatus: "", status: "", priority: "", comment: "", downloadPassword: "", addedDate: 0, finishedDate: 0, state: "waiting", ...over });
const dlPkg = { id: "1", name: "p", hosts: [], childCount: 1, bytesTotal: 100, bytesLoaded: 0, speed: 0, eta: null, enabled: true, finished: false, running: false, saveTo: "", status: "", priority: "", comment: "", downloadPassword: "" };

let log: string[];
let sessionOK = true;
beforeEach(() => {
  mock.timers.enable({ apis: ["setTimeout"] });
  mock.method(Math, "random", () => 0.5);
  FakeEventSource.all.length = 0;
  fakeDocument.hidden = false;
  sessionOK = true;
  app.session.value = { username: "u", totpEnabled: false, version: "t" };
  log = stubFetch({
    "/status": () => json(status()),
    "/downloads": () => json({ packages: [dlPkg], links: [dlLink()] }),
    "/prefs": () => json({}),
    "/session": () => (sessionOK ? json({ username: "u" }) : json({ error: { code: "unauthorized", message: "no" } }, 401)),
  });
});
afterEach(() => {
  live.stopLive();
  mock.timers.reset();
  mock.restoreAll();
});

const count = (p: string) => log.filter((x) => x === `GET ${p}`).length;

test("patch events change a loaded list, structure events reload it once", async () => {
  live.startLive();
  await flush();
  live.ensureList("downloads");
  await flush(30);
  assert.equal(stores.downloads.loaded.value, true);
  const es = FakeEventSource.all[0]!;
  assert.equal(es.url, "/api/v1/events");
  es.open();
  es.emit("patch", { list: "downloads", links: { "11": { state: "running", running: true } }, packages: {} });
  assert.equal(stores.downloads.model.links.get("11")!.state, "running");
  const loads = count("/downloads");
  es.emit("structure", { list: "downloads" });
  es.emit("structure", { list: "downloads" });
  es.emit("structure", { list: "downloads" });
  mock.timers.tick(500);
  await flush(30);
  assert.equal(count("/downloads"), loads + 1, "three structure events, one load");
});

test("connection, captcha and dialog events change the status", async () => {
  live.startLive();
  await flush(30);
  const es = FakeEventSource.all[0]!;
  es.open();
  es.emit("connection", { connection: "offline", detail: "gone" });
  assert.equal(app.status.value!.connection, "offline");
  assert.equal(app.status.value!.connectionDetail, "gone");
  assert.equal(app.live.value, false);
  es.emit("captcha", { count: 2 });
  es.emit("dialog", { count: 1 });
  assert.equal(app.status.value!.captchas, 2);
  assert.equal(app.status.value!.dialogs, 1);
});

test("a broken stream is asked about the session, then opened again with backoff, and status and lists load again", async () => {
  live.startLive();
  await flush(30);
  live.ensureList("downloads");
  await flush(30);
  FakeEventSource.all[0]!.open();
  const statusCalls = count("/status");
  const listCalls = count("/downloads");

  FakeEventSource.all[0]!.fail();
  await flush(30);
  assert.equal(FakeEventSource.all[0]!.closed, true);
  assert.equal(app.webReachable.value, false);
  assert.equal(FakeEventSource.all.length, 1, "not at once");
  assert.ok(count("/session") >= 1, "the session is asked about first");
  mock.timers.tick(1500);
  await flush(30);
  assert.equal(FakeEventSource.all.length, 2, "a new stream after the pause");

  FakeEventSource.all[1]!.open();
  await flush(30);
  assert.equal(app.webReachable.value, true);
  assert.equal(count("/status"), statusCalls + 1);
  assert.equal(count("/downloads"), listCalls + 1);

  // After an open the pause starts at the beginning again; without an open it doubles.
  FakeEventSource.all[1]!.fail();
  await flush(30);
  mock.timers.tick(1500);
  await flush(30);
  assert.equal(FakeEventSource.all.length, 3, "a successful open reset the pause to 1 s");
  FakeEventSource.all[2]!.fail();
  await flush(30);
  mock.timers.tick(1500);
  await flush(30);
  assert.equal(FakeEventSource.all.length, 3, "the next pause is 2 s");
  mock.timers.tick(1000);
  await flush(30);
  assert.equal(FakeEventSource.all.length, 4);
});

test("a stream that fails because the session ended shows the login and does not reconnect", async () => {
  live.startLive();
  await flush(30);
  FakeEventSource.all[0]!.open();
  sessionOK = false;
  FakeEventSource.all[0]!.fail();
  await flush(30);
  assert.equal(app.session.value, null, "login page");
  mock.timers.tick(60000);
  await flush(30);
  assert.equal(FakeEventSource.all.length, 1, "no retries after the 401");
});

test("a tab that stays hidden closes the stream, and coming back opens it and reloads", async () => {
  live.startLive();
  await flush(30);
  live.ensureList("downloads");
  await flush(30);
  FakeEventSource.all[0]!.open();
  fakeDocument.hidden = true;
  fakeDocument.fire("visibilitychange");
  mock.timers.tick(5000);
  assert.equal(FakeEventSource.all[0]!.closed, false, "a short look away keeps it");
  mock.timers.tick(11000);
  assert.equal(FakeEventSource.all[0]!.closed, true, "after the grace time it is closed");
  const statusCalls = count("/status");
  const listCalls = count("/downloads");

  fakeDocument.hidden = false;
  fakeDocument.fire("visibilitychange");
  assert.equal(FakeEventSource.all.length, 2);
  FakeEventSource.all[1]!.open();
  await flush(30);
  assert.equal(count("/status"), statusCalls + 1);
  assert.equal(count("/downloads"), listCalls + 1);
});

test("a short look away does not even reconnect", async () => {
  live.startLive();
  await flush(30);
  FakeEventSource.all[0]!.open();
  fakeDocument.hidden = true;
  fakeDocument.fire("visibilitychange");
  mock.timers.tick(3000);
  fakeDocument.hidden = false;
  fakeDocument.fire("visibilitychange");
  mock.timers.tick(30000);
  assert.equal(FakeEventSource.all.length, 1);
  assert.equal(FakeEventSource.all[0]!.closed, false);
});

test("stopping ends the stream and forgets the lists", async () => {
  live.startLive();
  await flush(30);
  live.ensureList("downloads");
  await flush(30);
  live.stopLive();
  assert.equal(FakeEventSource.all[0]!.closed, true);
  assert.equal(stores.downloads.loaded.value, false);
  assert.equal(app.status.value, null);
  assert.equal(fakeDocument.listenerCount("visibilitychange"), 0);
});

test("preferences are not saved before the server's are known, and then merged", async () => {
  app.resetPrefs();
  log = stubFetch({ "/prefs": () => json({ theme: "light", lang: "de", other: 1 }), "/session": () => json({}) });
  // First load fails.
  globalThis.fetch = (async (url: string, init?: RequestInit) => {
    log.push(`${init?.method ?? "GET"} ${String(url).replace("/api/v1", "")}`);
    throw new TypeError("offline");
  }) as typeof fetch;
  assert.equal(await app.loadPrefs(), false);
  app.updatePrefs({ details: true });
  mock.timers.tick(5000);
  await flush(30);
  assert.equal(log.filter((x) => x.startsWith("PUT")).length, 0, "nothing was written");

  log = stubFetch({ "/prefs": () => json({ theme: "light", other: 1 }) });
  assert.equal(await app.loadPrefs(), true);
  assert.deepEqual(app.prefs.value, { theme: "light", other: 1, details: true }, "the server's preferences with the local change on top");
  mock.timers.tick(1000);
  await flush(30);
  assert.equal(log.filter((x) => x === "PUT /prefs").length, 1);
});
