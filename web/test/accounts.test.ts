import { test } from "node:test";
import assert from "node:assert/strict";
import { accountStatus, daysLeft } from "../src/lib/accounts.ts";

const NOW = Date.UTC(2026, 10, 2);
const base = { enabled: true, valid: true, errorType: "", errorString: "", validUntil: -1 };

test("a valid account is valid", () => {
  assert.deepEqual(accountStatus(base, NOW), { state: "valid", tone: "run", detail: "" });
  assert.equal(accountStatus({ ...base, validUntil: NOW + 86_400_000 }, NOW).state, "valid");
});

test("a disabled account says so before anything else", () => {
  assert.equal(accountStatus({ ...base, enabled: false, valid: false, errorType: "EXPIRED" }, NOW).state, "disabled");
});

test("an expired account is recognized by the error or by the date", () => {
  assert.equal(accountStatus({ ...base, valid: false, errorType: "EXPIRED", errorString: "Account expired" }, NOW).state, "expired");
  assert.equal(accountStatus({ ...base, valid: false, validUntil: NOW - 1000 }, NOW).state, "expired");
  // A date in the past on an account that JDownloader still calls valid is not an error.
  assert.equal(accountStatus({ ...base, validUntil: NOW - 1000 }, NOW).state, "valid");
});

test("other failures carry the text of JDownloader", () => {
  const s = accountStatus({ ...base, valid: false, errorType: "INVALID", errorString: "Invalid username or password" }, NOW);
  assert.deepEqual(s, { state: "error", tone: "err", detail: "Invalid username or password" });
});

test("days left, negative when over, null when unknown", () => {
  assert.equal(daysLeft(NOW + 61 * 86_400_000, NOW), 61);
  assert.equal(daysLeft(NOW - 9 * 86_400_000, NOW), -9);
  assert.equal(daysLeft(-1, NOW), null);
  assert.equal(daysLeft(0, NOW), null);
});
