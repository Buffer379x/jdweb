import { readFileSync } from "node:fs";
import { test } from "@playwright/test";
import { Api } from "../../support/api.ts";
import { AUTOSTART } from "../../support/env.ts";

test("leave JDownloader as found", async ({ request, baseURL }) => {
  const api = new Api(request, baseURL!);
  await api.wipe();
  await api.put("/config", { ...AUTOSTART, value: JSON.parse(readFileSync(".auth/autostart-old.json", "utf8")) });
});
