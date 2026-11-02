import { test } from "node:test";
import assert from "node:assert/strict";
import { describeAgent } from "../src/lib/agent.ts";

test("common browsers are named", () => {
  assert.equal(describeAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"), "Chrome / macOS");
  assert.equal(describeAgent("Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0"), "Firefox / Linux");
  assert.equal(describeAgent("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"), "Safari / iOS");
  assert.equal(describeAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0 Safari/537.36 Edg/120.0"), "Edge / Windows");
  assert.equal(describeAgent("curl/8.4.0"), "curl");
});

test("unknown and empty agents stay short and plain", () => {
  assert.equal(describeAgent(""), "");
  assert.equal(describeAgent("x".repeat(100)).length, 61);
  assert.equal(describeAgent("<script>"), "<script>");
});
