import { test } from "node:test";
import assert from "node:assert/strict";
import { configPaths, revealText } from "../src/lib/configapi.ts";

function serve(value: unknown) {
  const calls: { url: string; body: unknown }[] = [];
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    calls.push({ url, body: JSON.parse(String(init.body)) });
    return new Response(JSON.stringify({ value }), { status: 200, headers: { "Content-Type": "application/json" } });
  }) as typeof fetch;
  return calls;
}

const ref = { interface: "jd.plugins.hoster.Foo", storage: "foo.com", key: "ApiKey" };

test("a plugin secret is revealed through the plugin route, a general one through the config route", async () => {
  const calls = serve("s3cret");
  assert.equal(await revealText("plugins", "string", ref), "s3cret");
  assert.equal(await revealText("config", "string", { ...ref, storage: "" }), "s3cret");
  assert.deepEqual(calls.map((c) => c.url), ["/api/v1/plugins/reveal", "/api/v1/config/reveal"]);
  assert.deepEqual(calls[0]!.body, { interface: ref.interface, storage: "foo.com", key: "ApiKey" });
});

test("every call of a row has its own route per kind of setting", () => {
  assert.deepEqual(configPaths("config"), { set: "/config", reset: "/config/reset", reveal: "/config/reveal" });
  assert.deepEqual(configPaths("plugins"), { set: "/plugins", reset: "/plugins/reset", reveal: "/plugins/reveal" });
});

test("a revealed list is the text of its editor", async () => {
  serve(["a", "b"]);
  assert.equal(await revealText("plugins", "list", ref), JSON.stringify(["a", "b"], null, 2));
});
