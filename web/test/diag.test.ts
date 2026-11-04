import { test } from "node:test";
import assert from "node:assert/strict";
import { addressOf, needsAttention, problems, worst, type Report } from "../src/lib/diag.ts";

const ok = { id: "a", status: "ok" as const, messageKey: "diag.api-reachable.ok", detail: "" };
const warn = { id: "api-no-login", status: "warn" as const, messageKey: "diag.api-no-login.warn", detail: "x" };
const fail = { id: "b", status: "fail" as const, messageKey: "diag.api-reachable.no-answer", detail: "y" };

test("the first-run dialog shows for unconfirmed problems only", () => {
  assert.equal(needsAttention(null), false);
  assert.equal(needsAttention({ checks: [ok], acknowledged: false }), false);
  assert.equal(needsAttention({ checks: [ok, warn], acknowledged: false }), true);
  assert.equal(needsAttention({ checks: [ok, warn], acknowledged: true }), false);
  assert.equal(needsAttention({ checks: [fail], acknowledged: false }), true);
});

test("problems and the worst status", () => {
  const r: Report = { checks: [ok, warn, fail], acknowledged: false };
  assert.deepEqual(problems(r).map((c) => c.status), ["warn", "fail"]);
  assert.equal(worst(r), "fail");
  assert.equal(worst({ checks: [ok, warn], acknowledged: false }), "warn");
  assert.equal(worst({ checks: [ok], acknowledged: false }), "ok");
});

test("the address in front of a detail", () => {
  assert.equal(addressOf("10.0.0.5 is outside the subnets of JDWeb"), "10.0.0.5");
  assert.equal(addressOf("127.0.0.1"), "127.0.0.1");
  assert.equal(addressOf(""), "");
});

test("saving an address waits until JDownloader has answered there", async () => {
  const { settleConnection } = await import("../src/lib/diag.ts");
  const answers = ["syncing", "syncing", "misconfigured"].map((connection) => ({ url: "u", connection, detail: connection === "misconfigured" ? "another service" : "" }));
  let asked = 0;
  const r = await settleConnection({ url: "u", connection: "syncing", detail: "" }, async () => answers[asked++]!, async () => {});
  assert.equal(r.connection, "misconfigured");
  assert.equal(r.detail, "another service");
  assert.equal(asked, 3);
  // Already settled: no question. Never settles: gives up with the last answer.
  assert.equal((await settleConnection({ url: "u", connection: "connected", detail: "" }, async () => { throw new Error("asked"); }, async () => {})).connection, "connected");
  const stuck = await settleConnection({ url: "u", connection: "syncing", detail: "" }, async () => ({ url: "u", connection: "syncing", detail: "" }), async () => {}, 3);
  assert.equal(stuck.connection, "syncing"); // the form shows "still checking" for this, never "Connected."
  // A failing question is a failure for the caller, not a quiet "syncing".
  await assert.rejects(settleConnection({ url: "u", connection: "syncing", detail: "" }, async () => { throw new Error("x"); }, async () => {}), /x/);
});
