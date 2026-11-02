import { isApiError } from "../api/client.ts";

/**
 * The captcha or question is gone: JDownloader dropped it and the server answers 404 (docs/api.md: "404
 * jd_error" for a captcha or dialog that was solved, answered or timed out; a 404 not_found counts the same).
 * Any other failure, a 502 jd_error above all, is a real error and must be shown as one, not as "gone".
 */
export function isGone(e: unknown): boolean {
  return isApiError(e) && e.status === 404;
}
