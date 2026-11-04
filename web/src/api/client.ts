// The one place that talks to /api/v1. Every state-changing request carries
// X-JDWeb-Request: 1. Errors come back as ApiError with the code of docs/api.md.

import { signal } from "@preact/signals";

export type ErrorCode =
  | "unauthorized"
  | "totp_required"
  | "forbidden"
  | "bad_request"
  | "not_found"
  | "conflict"
  | "selection_gone"
  | "rate_limited"
  | "jd_offline"
  | "jd_error"
  | "internal"
  | "network"
  | "unknown";

export class ApiError extends Error {
  readonly status: number;
  readonly code: ErrorCode;
  /** Seconds, from the Retry-After header of a 429. */
  readonly retryAfter: number | null;
  constructor(status: number, code: ErrorCode, message: string, retryAfter: number | null = null) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.retryAfter = retryAfter;
  }
}

const KNOWN: ReadonlySet<string> = new Set([
  "unauthorized", "totp_required", "forbidden", "bad_request", "not_found", "conflict", "selection_gone",
  "rate_limited", "jd_offline", "jd_error", "internal",
]);

/** Called when the server says "sign in again". The session store sets this. */
export const onUnauthorized: { fn: () => void } = { fn: () => {} };

/** Requests in flight, for the thin progress hint of the toolbar. */
export const pending = signal(0);

function parseRetryAfter(h: string | null): number | null {
  if (!h) return null;
  const n = Number(h);
  return Number.isFinite(n) && n >= 0 ? Math.ceil(n) : null;
}

interface Options {
  body?: unknown;
  /** The login and setup calls handle a 401 themselves. */
  ownUnauthorized?: boolean;
  signal?: AbortSignal;
}

export async function request<T = void>(method: string, path: string, opts: Options = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  const mutating = method !== "GET" && method !== "HEAD";
  if (mutating) headers["X-JDWeb-Request"] = "1";
  let body: string | undefined;
  if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.body);
  }
  let res: Response;
  pending.value++;
  try {
    res = await fetch(`/api/v1${path}`, { method, headers, body, credentials: "same-origin", signal: opts.signal });
  } catch (e) {
    if (e instanceof DOMException && e.name === "AbortError") throw e;
    throw new ApiError(0, "network", "The server cannot be reached.");
  } finally {
    pending.value--;
  }
  // 204 and 202 may come without a body; so may any success.
  const text = await res.text().catch(() => "");
  if (res.ok) {
    if (!text) return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch {
      return undefined as T;
    }
  }
  let code: ErrorCode = "unknown";
  let message = res.statusText;
  try {
    const j = JSON.parse(text) as { error?: { code?: string; message?: string } };
    if (j.error?.code) code = KNOWN.has(j.error.code) ? (j.error.code as ErrorCode) : "unknown";
    if (j.error?.message) message = j.error.message;
  } catch {
    /* an error that is no JSON, a proxy page for example */
  }
  const err = new ApiError(res.status, code, message, parseRetryAfter(res.headers.get("Retry-After")));
  if (res.status === 401 && code === "unauthorized" && !opts.ownUnauthorized) onUnauthorized.fn();
  throw err;
}

export const get = <T>(path: string, signal?: AbortSignal) => request<T>("GET", path, { signal });
export const post = <T = void>(path: string, body?: unknown, opts: Options = {}) => request<T>("POST", path, { ...opts, body: body ?? {} });
export const put = <T = void>(path: string, body: unknown) => request<T>("PUT", path, { body });
export const del = <T = void>(path: string) => request<T>("DELETE", path);

/** The error code of an unknown failure, for code that only wants to know "was it 404". */
export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError;
}
