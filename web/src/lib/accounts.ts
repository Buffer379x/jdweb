// The state of an account in words, from what JDownloader reports. The order decides, like the link states.

export interface AccountLike {
  enabled: boolean;
  valid: boolean;
  errorType: string;
  errorString: string;
  validUntil: number;
}

export type AccountState = "disabled" | "expired" | "error" | "valid";
export type AccountTone = "muted" | "err" | "run";

export interface AccountStatus {
  state: AccountState;
  tone: AccountTone;
  /** For an error that is none of the known ones: the text of JDownloader. */
  detail: string;
}

const EXPIRED = /EXPIRED/i;

export function accountStatus(a: AccountLike, now: number): AccountStatus {
  if (!a.enabled) return { state: "disabled", tone: "muted", detail: "" };
  const expiredByDate = a.validUntil > 0 && a.validUntil < now;
  if (EXPIRED.test(a.errorType) || (expiredByDate && !a.valid)) return { state: "expired", tone: "err", detail: "" };
  if (!a.valid) return { state: "error", tone: "err", detail: a.errorString };
  return { state: "valid", tone: "run", detail: "" };
}

/** Days until the account runs out, negative when it is over; null when unknown. */
export function daysLeft(validUntil: number, now: number): number | null {
  if (!(validUntil > 0)) return null;
  return Math.floor((validUntil - now) / 86_400_000);
}

export const KNOWN_ERRORS = new Set(["INVALID", "TEMP_ERROR", "PLUGIN_ERROR", "IO_ERROR"]);
