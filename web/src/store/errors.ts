// Error messages per code, through the dictionaries.

import { ApiError, isApiError } from "../api/client.ts";
import { t, plural, type Key } from "../i18n/index.ts";
import { errorToast, pushToast } from "./app.ts";

export function errorText(e: unknown): string {
  if (!isApiError(e)) return t("err.unknown");
  if (e.code === "rate_limited") {
    return e.retryAfter != null ? plural("err.rate_limited_after", e.retryAfter) : t("err.rate_limited");
  }
  const key = `err.${e.code}` as Key;
  return t(key);
}

/** The English text of the server, shown small below the translation when it adds something. */
export function errorDetail(e: unknown): string | undefined {
  if (!(e instanceof ApiError) || !e.message) return undefined;
  if (e.code === "network" || e.code === "unauthorized" || e.code === "rate_limited") return undefined;
  return e.message;
}

errorToast.fn = (e) => {
  if (isApiError(e) && e.code === "unauthorized") return; // the login page says it
  pushToast("error", errorText(e), errorDetail(e));
};

/** A wrong password in /password and /totp/* is a 403, not a lost session. */
export function passwordErrorText(e: unknown): string {
  return isApiError(e) && e.code === "forbidden" ? t("account.wrongPassword") : errorText(e);
}
