// Diagnostics: which checks need the person's attention, and which key selects their text.

export interface Check {
  id: string;
  status: "ok" | "warn" | "fail";
  messageKey: string;
  detail: string;
}

export interface Report {
  checks: Check[];
  acknowledged: boolean;
}

/** Checks that are not fine. */
export function problems(r: Report): Check[] {
  return r.checks.filter((c) => c.status !== "ok");
}

/** The first-run dialog shows when something is not fine and nobody has confirmed the warnings yet. */
export function needsAttention(r: Report | null): boolean {
  return !!r && !r.acknowledged && problems(r).length > 0;
}

/** The worst status of a report, for the lamp. */
export function worst(r: Report): Check["status"] {
  if (r.checks.some((c) => c.status === "fail")) return "fail";
  if (r.checks.some((c) => c.status === "warn")) return "warn";
  return "ok";
}

/** The address part of a detail such as "http://x:3128 is outside ..."; empty when there is none. */
export function addressOf(detail: string): string {
  const m = /^(\S+) /.exec(detail);
  return m ? m[1]! : detail;
}

export interface ConnectionAnswer {
  url: string;
  connection: string;
  detail: string;
}

/**
 * Saving an address answers "syncing" at once: whether JDownloader answers there is only known a moment later.
 * Showing "Connected." for that would be wrong for a bad address. Asks again until the state is settled.
 * What comes back may still be "syncing" after all tries: that is not a success, the caller says "still checking".
 * A question that fails is not answered here either: the error goes to the caller, who shows it as a failure.
 */
export async function settleConnection(
  first: ConnectionAnswer,
  ask: () => Promise<ConnectionAnswer>,
  wait: (ms: number) => Promise<void>,
  tries = 30,
): Promise<ConnectionAnswer> {
  let cur = first;
  for (let i = 0; i < tries && cur.connection === "syncing"; i++) {
    await wait(500);
    cur = await ask();
  }
  return cur;
}
