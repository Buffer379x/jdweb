import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { request, post, onUnauthorized, ApiError } from "../src/api/client.ts";

interface Seen { url: string; init: RequestInit }
let seen: Seen[] = [];

function respond(status: number, body: string | null, headers: Record<string, string> = {}) {
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    seen.push({ url, init });
    return new Response(body, { status, headers });
  }) as typeof fetch;
}

beforeEach(() => {
  seen = [];
  onUnauthorized.fn = () => {};
});

test("every state-changing request carries X-JDWeb-Request, reads do not", async () => {
  respond(204, null);
  for (const m of ["POST", "PUT", "DELETE", "PATCH"]) await request(m, "/x", { body: {} });
  for (const s of seen) assert.equal((s.init.headers as Record<string, string>)["X-JDWeb-Request"], "1");
  seen = [];
  respond(200, "{}");
  await request("GET", "/status");
  assert.equal((seen[0]!.init.headers as Record<string, string>)["X-JDWeb-Request"], undefined);
  assert.equal(seen[0]!.url, "/api/v1/status");
});

test("empty answers of 204 and 202 are fine", async () => {
  respond(204, null);
  assert.equal(await post("/controller/start"), undefined);
  respond(202, "");
  assert.equal(await post("/system/restart"), undefined);
});

test("a 401 with the code unauthorized shows the login, a login 401 does not", async () => {
  let called = 0;
  onUnauthorized.fn = () => called++;
  respond(401, JSON.stringify({ error: { code: "unauthorized", message: "no" } }));
  await assert.rejects(() => request("GET", "/status"), (e: ApiError) => e.code === "unauthorized" && e.status === 401);
  assert.equal(called, 1);
  await assert.rejects(() => post("/login", {}, { ownUnauthorized: true }));
  assert.equal(called, 1);
  // totp_required is not a lost session
  respond(401, JSON.stringify({ error: { code: "totp_required", message: "" } }));
  await assert.rejects(() => post("/login", {}), (e: ApiError) => e.code === "totp_required");
  assert.equal(called, 1);
});

test("a 429 carries the seconds of Retry-After", async () => {
  respond(429, JSON.stringify({ error: { code: "rate_limited", message: "slow" } }), { "Retry-After": "17" });
  await assert.rejects(() => post("/login", {}), (e: ApiError) => e.code === "rate_limited" && e.retryAfter === 17);
});

test("errors that are no JSON and network failures get a code", async () => {
  respond(502, "<html>Bad Gateway</html>");
  await assert.rejects(() => request("GET", "/status"), (e: ApiError) => e.code === "unknown" && e.status === 502);
  globalThis.fetch = (async () => { throw new TypeError("failed"); }) as typeof fetch;
  await assert.rejects(() => request("GET", "/status"), (e: ApiError) => e.code === "network");
});

test("an unknown code from the server is not trusted", async () => {
  respond(400, JSON.stringify({ error: { code: "__proto__", message: "x" } }));
  await assert.rejects(() => request("GET", "/x"), (e: ApiError) => e.code === "unknown");
});

test("a 409 selection_gone keeps its own code", async () => {
  respond(409, JSON.stringify({ error: { code: "selection_gone", message: "gone" } }));
  await assert.rejects(() => post("/downloads/actions/cleanup", {}), (e: ApiError) => e.code === "selection_gone" && e.status === 409);
});
