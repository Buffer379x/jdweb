import { existsSync, readFileSync, rmSync } from "node:fs";
import { test } from "@playwright/test";
import { Api } from "../../support/api.ts";
import { AUTOSTART, AUTOSTART_OLD } from "../../support/env.ts";
import { assertTestInstance, guardPassed } from "../../support/guard.ts";

// Nothing here is destructive unless 00-setup passed the guard in this run (.auth/guard-passed), and the
// instance is checked again first. A stale session file of an earlier run enables nothing. The content check
// (packages, dialogs) is left out the second time: packages the UI named itself do not carry the prefix.
test("leave JDownloader as found", async ({ request, baseURL }) => {
  test.skip(!guardPassed(), "the guard did not pass in this run: JDownloader is left untouched");
  await assertTestInstance({ content: false });
  const api = new Api(request, baseURL!);
  await api.wipe();
  // The switch goes back to what it was. The saved value stays until the restore worked; after a setup that
  // died early there is none and nothing to restore.
  if (existsSync(AUTOSTART_OLD)) {
    await api.put("/config", { ...AUTOSTART, value: JSON.parse(readFileSync(AUTOSTART_OLD, "utf8")) });
    rmSync(AUTOSTART_OLD);
  }
});
