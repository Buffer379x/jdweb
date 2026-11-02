import { test } from "node:test";
import assert from "node:assert/strict";
import { ApiError, post } from "../src/api/client.ts";
import { isGone } from "../src/lib/gone.ts";

function answer(status: number, code: string) {
  globalThis.fetch = (async () => new Response(JSON.stringify({ error: { code, message: "m" } }), { status, headers: { "Content-Type": "application/json" } })) as typeof fetch;
}

// What the captcha dialog does with the failure of a solve: through the real client, not a hand-built error.
async function outcome(status: number, code: string): Promise<"gone" | "error"> {
  answer(status, code);
  try {
    await post("/captchas/1/solve", { result: "x" });
  } catch (e) {
    return isGone(e) ? "gone" : "error";
  }
  throw new Error("no failure");
}

test("404 means gone, whatever the code", async () => {
  assert.equal(await outcome(404, "jd_error"), "gone");
  assert.equal(await outcome(404, "not_found"), "gone");
});

test("a 502 jd_error is a real error and is not told as gone", async () => {
  assert.equal(await outcome(502, "jd_error"), "error");
  assert.equal(await outcome(503, "jd_offline"), "error");
  assert.equal(await outcome(409, "conflict"), "error");
});

test("things that are no api error are no 'gone'", () => {
  assert.equal(isGone(new Error("x")), false);
  assert.equal(isGone(new ApiError(502, "jd_error", "m")), false);
  assert.equal(isGone(null), false);
});
