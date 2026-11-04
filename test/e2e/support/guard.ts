import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { JD_CONTAINER, JD_URL, PREFIX } from "./env.ts";

// The same rule is in internal/testguard/guard.go; keep the text identical.
export const RULE =
  "A JDownloader counts as a test instance only if all of these hold: " +
  "(1) the URL host is 127.0.0.1, or JDWEB_TEST_ALLOW_REMOTE is exactly that URL; " +
  '(2) its device name is "jdweb-test-stack", which scripts/teststack.sh up sets; ' +
  "(3) every package name starts with the prefix of the tests; " +
  "(4) no dialog and no captcha is open.";

export const MARKER = { interface: "org.jdownloader.api.myjdownloader.MyJDownloaderSettings", key: "DeviceName", value: "jdweb-test-stack" };

/** Written by 00-setup after the guard passed. Without it, 99-teardown does nothing destructive. */
export const GUARD_FILE = ".auth/guard-passed";

async function jd<T>(path: string, ...params: unknown[]): Promise<T> {
  const res = await fetch(JD_URL + path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ params }), signal: AbortSignal.timeout(10_000) });
  if (!res.ok) throw new Error(`${path}: HTTP ${res.status}`);
  return ((await res.json()) as { data: T }).data;
}

/**
 * Refuses (throws) unless JDownloader at E2E_JD_URL is the test stack (RULE). It changes nothing.
 * Parts 1 and 2 decide who the instance is; parts 3 and 4 decide whether it is empty. The teardown asks
 * for the first two only, because packages the UI named itself do not carry the prefix.
 */
export async function assertTestInstance(opts: { content?: boolean } = {}): Promise<void> {
  const content = opts.content ?? true;
  const refuse = (why: string): never => {
    throw new Error(`REFUSING to run against ${JD_URL}: ${why}\n${RULE}`);
  };
  const u = new URL(JD_URL);
  const remote = (process.env.JDWEB_TEST_ALLOW_REMOTE ?? "").replace(/\/+$/, "");
  if (u.hostname !== "127.0.0.1" && remote !== JD_URL.replace(/\/+$/, "")) refuse(`the host is "${u.hostname}"`);
  let published = "";
  try {
    published = execFileSync("docker", ["port", JD_CONTAINER], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  } catch (e) {
    refuse(`cannot ask docker for the port of ${JD_CONTAINER}: ${(e as Error).message}`);
  }
  if (!published.split("\n").some((l) => l.trim().endsWith(`:${u.port}`))) refuse(`the container ${JD_CONTAINER} does not publish port ${u.port} (docker port: ${published.trim()})`);
  let name: string;
  try {
    if ((await jd<boolean>("/device/ping")) !== true) refuse("JDownloader does not answer the ping");
    name = await jd<string>("/config/get", MARKER.interface, null, MARKER.key);
  } catch (e) {
    return refuse(`cannot read JDownloader: ${(e as Error).message}`);
  }
  if (name !== MARKER.value) refuse(`the device name is "${name}", not "${MARKER.value}"`);
  if (!content) return;
  const lists = [...(await jd<{ name: string }[]>("/downloadsV2/queryPackages", {})), ...(await jd<{ name: string }[]>("/linkgrabberv2/queryPackages", {}))];
  for (const p of lists) if (!p.name.startsWith(PREFIX)) refuse(`JDownloader holds the package "${p.name}" (prefix ${PREFIX})`);
  if ((await jd<unknown[]>("/dialogs/list")).length > 0) refuse("a dialog is open");
  if ((await jd<unknown[]>("/captcha/list")).length > 0) refuse("a captcha is open");
}

export function clearGuardMark(): void {
  rmSync(GUARD_FILE, { force: true });
}

export function markGuardPassed(): void {
  mkdirSync(".auth", { recursive: true });
  writeFileSync(GUARD_FILE, new Date().toISOString());
}

export const guardPassed = (): boolean => existsSync(GUARD_FILE);
