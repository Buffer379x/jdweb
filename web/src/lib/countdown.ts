// The time a captcha has left. JDownloader gives `remaining` and `timeout` in milliseconds (the unit is
// not in docs/api.md; the fake and the recorded job agree). The clock of the browser is only used to
// count down from the moment the answer arrived, never to compare with JDownloader's clock.

export interface CaptchaTimes {
  remaining: number;
  timeout: number;
  created: number;
}

/** The moment (browser clock, ms) the captcha runs out, or null when nothing is known. */
export function deadline(c: CaptchaTimes, receivedAt: number): number | null {
  if (c.remaining > 0) return receivedAt + c.remaining;
  if (c.timeout > 0 && c.created > 0) return c.created + c.timeout; // no remaining time: the browser clock has to do
  return null;
}

/** Whole seconds left, rounded up so that "0:00" only shows when it is over. Null without a deadline. */
export function secondsLeft(deadlineAt: number | null, now: number): number | null {
  if (deadlineAt === null) return null;
  return Math.max(0, Math.ceil((deadlineAt - now) / 1000));
}

export function formatCountdown(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}
