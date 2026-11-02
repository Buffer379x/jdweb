// Global UI state: route, session, status, toasts, preferences.

import { computed, signal } from "@preact/signals";
import { get, put, post, onUnauthorized, isApiError } from "../api/client.ts";
import type { Prefs, Session, Status } from "../api/types.ts";
import { setLangChoice, type LangChoice } from "../i18n/index.ts";

// ---- route -------------------------------------------------------------

export type Route = "downloads" | "linkgrabber" | "settings";
export const ROUTES: readonly Route[] = ["downloads", "linkgrabber", "settings"];

export function routeFromPath(path: string): Route {
  const first = path.split("/").filter(Boolean)[0];
  return (ROUTES as readonly string[]).includes(first ?? "") ? (first as Route) : "downloads";
}

export const route = signal<Route>(routeFromPath(location.pathname));

export function navigate(r: Route): void {
  if (route.value === r) return;
  history.pushState(null, "", `/${r}`);
  route.value = r;
}

window.addEventListener("popstate", () => {
  route.value = routeFromPath(location.pathname);
});

// ---- session -----------------------------------------------------------

/** undefined while it is being asked, null when nobody is signed in. */
export const session = signal<Session | null | undefined>(undefined);
export const setupRequired = signal<boolean | undefined>(undefined);

onUnauthorized.fn = () => {
  if (session.value) {
    session.value = null;
    stopHook.fn();
  }
};

/** Set by live.ts so that signing out stops the event stream without a cycle of imports. */
export const stopHook: { fn: () => void } = { fn: () => {} };

export async function checkSession(): Promise<void> {
  try {
    const st = await get<{ required: boolean }>("/setup/status");
    setupRequired.value = st.required;
    if (st.required) {
      session.value = null;
      return;
    }
  } catch {
    setupRequired.value = false;
  }
  try {
    session.value = await get<Session>("/session");
  } catch (e) {
    session.value = null;
    if (isApiError(e) && e.code !== "unauthorized") pushToastForError(e);
  }
}

export async function logout(): Promise<void> {
  try {
    await post("/logout");
  } catch {
    /* the session is gone either way */
  }
  stopHook.fn();
  session.value = null;
}

// ---- status ------------------------------------------------------------

export const status = signal<Status | null>(null);
/** False while the event stream to JDWeb itself is down. */
export const webReachable = signal(true);

export const live = computed(() => {
  const c = status.value?.connection;
  return c === "connected" || c === "syncing";
});

// ---- toasts ------------------------------------------------------------

export interface Toast {
  id: number;
  kind: "info" | "error";
  text: string;
  detail?: string;
}
export const toasts = signal<Toast[]>([]);
let toastSeq = 0;

export function pushToast(kind: Toast["kind"], text: string, detail?: string): void {
  const id = ++toastSeq;
  // The same message twice in a row is one toast.
  const last = toasts.value[toasts.value.length - 1];
  if (last && last.text === text && last.detail === detail) return;
  toasts.value = [...toasts.value.slice(-3), { id, kind, text, detail }];
  setTimeout(() => dismissToast(id), kind === "error" ? 9000 : 4500);
}
export function dismissToast(id: number): void {
  toasts.value = toasts.value.filter((x) => x.id !== id);
}
/** Replaced in errors.ts, which needs the dictionaries. */
export const errorToast: { fn: (e: unknown) => void } = { fn: () => {} };
export function pushToastForError(e: unknown): void {
  errorToast.fn(e);
}

// ---- preferences ---------------------------------------------------------

export const prefs = signal<Prefs>({});

function storeLocal(key: string, value: string | null): void {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    /* private mode, blocked storage */
  }
}
function readLocal(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export type ThemeChoice = "system" | "dark" | "light";
export const themeChoice = signal<ThemeChoice>("system");

export function applyTheme(c: ThemeChoice): void {
  themeChoice.value = c;
  if (c === "system") document.documentElement.removeAttribute("data-theme");
  else document.documentElement.setAttribute("data-theme", c);
  storeLocal("jdweb.theme", c === "system" ? null : c);
}

export function initLocalPrefs(): void {
  const th = readLocal("jdweb.theme");
  applyTheme(th === "dark" || th === "light" ? th : "system");
  const lg = readLocal("jdweb.lang");
  setLangChoice(lg === "de" || lg === "en" ? lg : "auto");
}

let saveTimer: ReturnType<typeof setTimeout> | undefined;
let saving = false;

function scheduleSave(): void {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(flushPrefs, 800);
}

export async function flushPrefs(): Promise<void> {
  clearTimeout(saveTimer);
  if (saving) {
    scheduleSave();
    return;
  }
  saving = true;
  try {
    await put("/prefs", prefs.value);
  } catch {
    /* preferences are a convenience, a failed save is not worth a message */
  } finally {
    saving = false;
  }
}

/** Merges into the preferences and saves them after a pause. */
export function updatePrefs(patch: Partial<Prefs>): void {
  prefs.value = { ...prefs.value, ...patch };
  scheduleSave();
}

export async function loadPrefs(): Promise<void> {
  try {
    const p = await get<Prefs>("/prefs");
    prefs.value = p && typeof p === "object" ? p : {};
  } catch {
    prefs.value = {};
  }
  // Server preferences win over what this browser remembered.
  if (prefs.value.theme === "dark" || prefs.value.theme === "light") applyTheme(prefs.value.theme);
  if (prefs.value.lang === "de" || prefs.value.lang === "en") {
    setLangChoice(prefs.value.lang);
    storeLocal("jdweb.lang", prefs.value.lang);
  }
}

export function chooseTheme(c: ThemeChoice): void {
  applyTheme(c);
  updatePrefs({ theme: c === "system" ? undefined : c });
}

export function chooseLang(c: LangChoice): void {
  setLangChoice(c);
  storeLocal("jdweb.lang", c === "auto" ? null : c);
  updatePrefs({ lang: c === "auto" ? undefined : c });
}

// ---- small UI flags --------------------------------------------------------

/** Task 7 opens the captcha dialog when this is true. */
export const captchaDialogOpen = signal(false);
/** Task 7 opens the questions of JDownloader when this is true. */
export const questionDialogOpen = signal(false);
export const helpOpen = signal(false);
