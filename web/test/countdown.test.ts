import { test } from "node:test";
import assert from "node:assert/strict";
import { deadline, formatCountdown, secondsLeft } from "../src/lib/countdown.ts";

test("the deadline counts from when the answer arrived", () => {
  assert.equal(deadline({ remaining: 90_000, timeout: 120_000, created: 1 }, 1_000_000), 1_090_000);
});

test("without remaining time the creation time and timeout are used", () => {
  assert.equal(deadline({ remaining: 0, timeout: 60_000, created: 2_000_000 }, 5), 2_060_000);
  assert.equal(deadline({ remaining: 0, timeout: 0, created: 2_000_000 }, 5), null);
  assert.equal(deadline({ remaining: 0, timeout: 60_000, created: 0 }, 5), null);
});

test("seconds round up and stop at zero", () => {
  assert.equal(secondsLeft(10_000, 0), 10);
  assert.equal(secondsLeft(10_001, 0), 11);
  assert.equal(secondsLeft(10_000, 9_001), 1);
  assert.equal(secondsLeft(10_000, 10_000), 0);
  assert.equal(secondsLeft(10_000, 99_000), 0);
  assert.equal(secondsLeft(null, 0), null);
});

test("the countdown reads m:ss", () => {
  assert.equal(formatCountdown(0), "0:00");
  assert.equal(formatCountdown(9), "0:09");
  assert.equal(formatCountdown(125), "2:05");
  assert.equal(formatCountdown(600), "10:00");
});
