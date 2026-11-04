import { test } from "@playwright/test";
import { FAKE_APP_CONTAINER, PASSWORD, USER, setupToken } from "../../support/env.ts";

// The account of the second instance (the one that talks to the fake JDownloader), through the API: the
// browser flow of setup and sign-in is tested against the real one.
test("account for the fake-backed instance", async ({ request, baseURL }) => {
  const headers = { "X-JDWeb-Request": "1", Origin: baseURL!, "Content-Type": "application/json" };
  const need = (await (await request.get("/api/v1/setup/status")).json()) as { required: boolean };
  if (need.required) {
    const s = await request.post("/api/v1/setup", { headers, data: { token: setupToken(FAKE_APP_CONTAINER), username: USER, password: PASSWORD } });
    if (s.status() !== 204) throw new Error(`setup: ${s.status()} ${await s.text()}`);
  }
  const l = await request.post("/api/v1/login", { headers, data: { username: USER, password: PASSWORD } });
  if (l.status() !== 204) throw new Error(`login: ${l.status()}`);
  await request.post("/api/v1/diagnostics/acknowledge", { headers, data: {} });
  await request.storageState({ path: ".auth/fake.json" });
});
