import { test } from "node:test";
import assert from "node:assert/strict";
import { MAX_BYTES_PER_SECOND, describeRate, parseDecimal, splitBytesPerSecond, toBytesPerSecond } from "../src/lib/units.ts";

test("parseDecimal takes comma and point and nothing else", () => {
  assert.equal(parseDecimal("1,5"), 1.5);
  assert.equal(parseDecimal(" 2.25 "), 2.25);
  assert.equal(parseDecimal("10"), 10);
  for (const bad of ["", "abc", "1e3", "-1", "1,2,3", "1.", ".5", "0x10"]) assert.equal(parseDecimal(bad), null, bad);
});

test("KB and MB convert with 1024", () => {
  assert.equal(toBytesPerSecond(500, "KB"), 512000);
  assert.equal(toBytesPerSecond(1, "MB"), 1048576);
  assert.equal(toBytesPerSecond(1.5, "MB"), 1572864);
  assert.equal(toBytesPerSecond(0.5, "KB"), 512);
});

test("zero, negative, NaN and too large are refused", () => {
  assert.equal(toBytesPerSecond(0, "KB"), null);
  assert.equal(toBytesPerSecond(-3, "MB"), null);
  assert.equal(toBytesPerSecond(Number.NaN, "MB"), null);
  assert.equal(toBytesPerSecond(Infinity, "MB"), null);
  assert.equal(toBytesPerSecond(0.0001, "KB"), null);
  assert.equal(toBytesPerSecond(MAX_BYTES_PER_SECOND / 1024 + 1, "KB"), null);
  assert.equal(toBytesPerSecond(MAX_BYTES_PER_SECOND / 1048576, "MB"), MAX_BYTES_PER_SECOND);
});

test("whole megabytes are shown as MB, the rest as KB", () => {
  assert.deepEqual(splitBytesPerSecond(51200), { value: 50, unit: "KB" });
  assert.deepEqual(splitBytesPerSecond(1048576), { value: 1, unit: "MB" });
  assert.deepEqual(splitBytesPerSecond(5 * 1048576), { value: 5, unit: "MB" });
  assert.deepEqual(splitBytesPerSecond(1572864), { value: 1536, unit: "KB" });
  assert.deepEqual(splitBytesPerSecond(1000), { value: 0.9765625, unit: "KB" });
  assert.equal(describeRate(1000), "0.98 KB/s");
  assert.equal(describeRate(5 * 1048576), "5 MB/s");
});

test("what is shown converts back", () => {
  for (const b of [51200, 1048576, 1572864, 3 * 1048576, 777 * 1024, 1000, 1, 1500, 123457, 99999999]) {
    const { value, unit } = splitBytesPerSecond(b);
    assert.equal(toBytesPerSecond(value, unit), b);
  }
});
