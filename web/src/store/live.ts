// The live connection: GET /events as Server-Sent Events, with backoff and
// reconnect, closed while the tab is hidden. After a gap, status and lists are
// loaded again, because patches that were missed cannot be replayed.

import { get, isApiError } from "../api/client.ts";
import type { ConnectionState, ListName, Patch, Status } from "../api/types.ts";
import { loadPrefs, prefsAreSynced, route, status, stopHook, webReachable } from "./app.ts";
import { stores } from "./stores.ts";

const BACKOFF_START = 1000;
const BACKOFF_MAX = 20000;
const HIDDEN_GRACE = 15000;

let es: EventSource | null = null;
let backoff = BACKOFF_START;
let retryTimer: ReturnType<typeof setTimeout> | undefined;
let hiddenTimer: ReturnType<typeof setTimeout> | undefined;
let running = false;
let suspended = false;
let hasOpened = false;

export async function refreshStatus(): Promise<void> {
  try {
    status.value = await get<Status>("/status");
  } catch {
    /* the 401 case is handled by the client, others by the next event */
  }
}

/** Lists are loaded when they are first needed, and kept up to date from then on. */
export function ensureList(name: ListName): void {
  const s = stores[name];
  if (!s.loaded.peek()) void s.load();
}

function reloadLoaded(): void {
  for (const s of Object.values(stores)) {
    if (s.loaded.peek()) void s.load();
  }
}

/** The preferences are loaded once; if that failed, with the next reconnect. Nothing is saved before. */
export async function ensurePrefs(): Promise<void> {
  if (prefsAreSynced()) return;
  if (await loadPrefs()) for (const s of Object.values(stores)) s.loadExpandedFromPrefs();
}

async function refreshAll(): Promise<void> {
  void ensurePrefs();
  await refreshStatus();
  reloadLoaded();
  if (route.value === "downloads" || route.value === "linkgrabber") ensureList(route.value);
}

function json<T>(ev: MessageEvent): T | null {
  try {
    return JSON.parse(ev.data as string) as T;
  } catch {
    return null;
  }
}

function connect(): void {
  if (!running || es) return;
  const src = new EventSource("/api/v1/events");
  es = src;
  src.onopen = () => {
    backoff = BACKOFF_START;
    webReachable.value = true;
    // After the first open the lists come from the normal load; every later open is a reconnect.
    if (hasOpened || !status.peek()) void refreshAll();
    hasOpened = true;
  };
  src.addEventListener("status", (ev) => {
    const s = json<Status>(ev as MessageEvent);
    if (s) status.value = s;
  });
  src.addEventListener("connection", (ev) => {
    const c = json<{ connection: ConnectionState; detail: string }>(ev as MessageEvent);
    if (c && status.peek()) status.value = { ...status.peek()!, connection: c.connection, connectionDetail: c.detail };
  });
  src.addEventListener("captcha", (ev) => {
    const c = json<{ count: number }>(ev as MessageEvent);
    if (c && status.peek()) status.value = { ...status.peek()!, captchas: c.count };
  });
  src.addEventListener("dialog", (ev) => {
    const c = json<{ count: number }>(ev as MessageEvent);
    if (c && status.peek()) status.value = { ...status.peek()!, dialogs: c.count };
  });
  src.addEventListener("patch", (ev) => {
    const p = json<Patch>(ev as MessageEvent);
    if (p && stores[p.list]) stores[p.list].patch(p);
  });
  src.addEventListener("structure", (ev) => {
    const s = json<{ list: ListName }>(ev as MessageEvent);
    if (s && stores[s.list]?.loaded.peek()) stores[s.list].reloadSoon();
  });
  src.onerror = () => {
    // The browser would retry by itself with a fixed delay and forever, also on a lost session. Do it here.
    src.close();
    if (es === src) es = null;
    if (!running || suspended) return;
    webReachable.value = false;
    void probeThenRetry();
  };
}

async function probeThenRetry(): Promise<void> {
  clearTimeout(retryTimer);
  // The stream of a session that ended is closed, and its reconnect would be a 401 that EventSource
  // cannot show. Ask at once: a lost session shows the login, anything else is tried again after a pause.
  try {
    await get("/session");
  } catch (e) {
    if (isApiError(e) && e.code === "unauthorized") return;
  }
  if (!running || suspended) return;
  const wait = backoff * (0.75 + Math.random() * 0.5);
  backoff = Math.min(backoff * 2, BACKOFF_MAX);
  retryTimer = setTimeout(connect, wait);
}

function disconnect(): void {
  clearTimeout(retryTimer);
  es?.close();
  es = null;
}

function onVisibility(): void {
  if (!running) return;
  if (document.hidden) {
    clearTimeout(hiddenTimer);
    hiddenTimer = setTimeout(() => {
      suspended = true;
      disconnect();
    }, HIDDEN_GRACE);
  } else {
    clearTimeout(hiddenTimer);
    if (suspended) {
      suspended = false;
      backoff = BACKOFF_START;
      hasOpened = true; // the next open loads status and lists again
      connect();
    }
  }
}

export function startLive(): void {
  if (running) return;
  running = true;
  suspended = false;
  hasOpened = false;
  document.addEventListener("visibilitychange", onVisibility);
  void refreshAll();
  connect();
}

export function stopLive(): void {
  running = false;
  document.removeEventListener("visibilitychange", onVisibility);
  clearTimeout(hiddenTimer);
  disconnect();
  status.value = null;
  for (const s of Object.values(stores)) s.reset();
}
stopHook.fn = stopLive;
