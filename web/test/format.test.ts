import { test } from "node:test";
import assert from "node:assert/strict";
import { formatBytes, formatSpeed, formatEta, percent, formatPercent } from "../src/lib/format.ts";

const N = " ";

test("sizes use binary units with the decimals of the language", () => {
  assert.equal(formatBytes(0, "de"), `0${N}B`);
  assert.equal(formatBytes(812, "de"), `812${N}B`);
  assert.equal(formatBytes(1024, "en"), `1.00${N}KB`);
  assert.equal(formatBytes(1.84 * 1024 ** 3, "de"), `1,84${N}GB`);
  assert.equal(formatBytes(1.84 * 1024 ** 3, "en"), `1.84${N}GB`);
  assert.equal(formatBytes(10.3 * 1024 ** 2, "de"), `10,3${N}MB`);
  assert.equal(formatBytes(512 * 1024 ** 2, "de"), `512${N}MB`);
  assert.equal(formatBytes(3 * 1024 ** 4, "en"), `3.00${N}TB`);
});

test("unknown or negative sizes are a dash, not 0 or NaN", () => {
  for (const v of [null, undefined, -1, NaN, Infinity]) {
    assert.equal(formatBytes(v as number, "de"), "–");
    assert.equal(formatSpeed(v as number, "de"), "–");
    assert.equal(formatEta(v as number), "–");
  }
});

test("speed", () => {
  assert.equal(formatSpeed(0, "de"), `0${N}B/s`);
  assert.equal(formatSpeed(12.4 * 1024 ** 2, "de"), `12,4${N}MB/s`);
});

test("eta", () => {
  assert.equal(formatEta(0), "00:00");
  assert.equal(formatEta(59), "00:59");
  assert.equal(formatEta(372), "06:12");
  assert.equal(formatEta(3599), "59:59");
  assert.equal(formatEta(3600), "1:00:00");
  assert.equal(formatEta(86399), "23:59:59");
  assert.equal(formatEta(86400 + 4 * 3600 + 5), `1${N}d${N}04${N}h`);
});

test("percent rounds down and stays in range", () => {
  assert.equal(percent(0, 0), 0);
  assert.equal(percent(5, 0), 0);
  assert.equal(percent(0, 0, true), 100);
  assert.equal(percent(999, 1000), 99);
  assert.equal(percent(1000, 1000), 100);
  assert.equal(percent(2000, 1000), 100);
  assert.equal(percent(-5, 1000), 0);
  assert.equal(percent(1, 3), 33);
  assert.equal(formatPercent(56, "de"), `56${N}%`);
  assert.equal(formatPercent(56, "en"), "56%");
});
