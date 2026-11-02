import { test } from "node:test";
import assert from "node:assert/strict";
import { availableActions, formatUptime, heapPercent } from "../src/lib/system.ts";

const MIN = 60_000;

test("uptime in the two largest units", () => {
  const t0 = 1_000_000_000_000;
  assert.equal(formatUptime(t0, t0 + 12 * MIN), "12 min");
  assert.equal(formatUptime(t0, t0 + (5 * 60 + 7) * MIN), "5 h 07 min");
  assert.equal(formatUptime(t0, t0 + (3 * 1440 + 4 * 60 + 9) * MIN), "3 d 04 h");
  assert.equal(formatUptime(0, t0), "");
  assert.equal(formatUptime(t0, t0 - 1), "");
});

test("memory share", () => {
  assert.equal(heapPercent(50, 200), 25);
  assert.equal(heapPercent(300, 200), 100);
  assert.equal(heapPercent(10, 0), 0);
  assert.equal(heapPercent(0, 10), 0);
});

test("system actions of the machine are missing in Docker", () => {
  assert.deepEqual(availableActions(true), ["restart", "exit"]);
  assert.deepEqual(availableActions(false), ["restart", "exit", "shutdown", "hibernate", "standby"]);
});
