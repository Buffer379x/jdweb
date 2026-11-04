import { execFileSync } from "node:child_process";

export const PREFIX = "jdweb-e2e-";
export const USER = "e2e";
export const PASSWORD = "e2e-correct-horse-battery";
export const FIXTURE = process.env.E2E_FIXTURE ?? "http://jdweb-test-files:8000";
export const APP_CONTAINER = process.env.E2E_APP_CONTAINER ?? "jdweb-e2e";
export const FAKE_APP_CONTAINER = process.env.E2E_FAKE_APP_CONTAINER ?? "jdweb-e2e-fake";
export const JD_CONTAINER = process.env.E2E_JD_CONTAINER ?? "jdweb-test-jd";
export const FAKE_CTL = process.env.E2E_FAKE_CTL ?? "http://127.0.0.1:17470";

export function docker(...args: string[]): string {
  return execFileSync("docker", args, { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
}

/** The one-time setup token from the log of the container. */
export function setupToken(container: string): string {
  const log = docker("logs", container);
  const m = [...log.matchAll(/"setup_token":"([^"]+)"/g)].pop();
  if (!m) throw new Error(`no setup token in the log of ${container}`);
  return m[1]!;
}

/** JDownloader's own switch: it starts what is moved to the downloads. The tests need it off. */
export const AUTOSTART = { interface: "org.jdownloader.gui.views.linkgrabber.addlinksdialog.LinkgrabberSettings", storage: "", key: "LinkgrabberAutoStartEnabled" };

/** Where 00-setup saves the old value of the switch; only 99-teardown deletes it, after the restore. */
export const AUTOSTART_OLD = ".auth/autostart-old.json";

/** The API of the test JDownloader itself (published on the host only), to see what JDownloader did before JDWeb tells it. */
export const JD_URL = process.env.E2E_JD_URL ?? "http://127.0.0.1:13128";

export async function jdSpeed(): Promise<number> {
  const r = await fetch(`${JD_URL}/downloadcontroller/getSpeedInBps`);
  return ((await r.json()) as { data: number }).data;
}
