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

// ---- settings pages ------------------------------------------------------
// The settings are a routed sub-layout: /settings (the list on a phone, General on a desktop),
// /settings/<page>, and /settings/extension?interface=<config interface> for the page of an extension.
// The interface is a query because a name with dots would look like a file to the web server.

export const SETTINGS_PAGES = [
  "general", "accounts", "basicauth", "plugins", "extensions", "extension", "expert", "system", "account", "diagnostics",
] as const;
export type SettingsPage = (typeof SETTINGS_PAGES)[number];

/** The page named by a path, or "" for /settings itself. */
export function settingsPageFromPath(path: string): SettingsPage | "" {
  const parts = path.split("/").filter(Boolean);
  if (parts[0] !== "settings") return "";
  return (SETTINGS_PAGES as readonly string[]).includes(parts[1] ?? "") ? (parts[1] as SettingsPage) : "";
}

export function extensionFromSearch(search: string): string {
  return new URLSearchParams(search).get("interface") ?? "";
}

export const settingsPage = signal<SettingsPage | "">(settingsPageFromPath(location.pathname));
/** The config interface of the extension whose page is open. */
export const extensionInterface = signal<string>(extensionFromSearch(location.search));

function syncFromLocation(): void {
  route.value = routeFromPath(location.pathname);
  settingsPage.value = settingsPageFromPath(location.pathname);
  extensionInterface.value = extensionFromSearch(location.search);
}

export function navigate(r: Route): void {
  // The tab "Settings" always leads to the start of the settings.
  if (route.value === r && !(r === "settings" && settingsPage.value !== "")) return;
  history.pushState(null, "", `/${r}`);
  syncFromLocation();
}

export function navigateSettings(page: SettingsPage | "", iface?: string): void {
  const path = page === "" ? "/settings" : `/settings/${page}`;
  const url = page === "extension" && iface ? `${path}?interface=${encodeURIComponent(iface)}` : path;
  if (location.pathname + location.search === url) return;
  history.pushState(null, "", url);
  syncFromLocation();
}

window.addEventListener("popstate", syncFromLocation);

// ---- session -----------------------------------------------------------

/** undefined while it is being asked, null when nobody is signed in. */
export const session = signal<Session | null | undefined>(undefined);
export const setupRequired = signal<boolean | undefined>(undefined);

onUnauthorized.fn = () => {
  if (session.value) {
    session.value = null;
    stopHook.fn();
    resetPrefs();
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
  resetPrefs();
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
/** True once the preferences of the server are known. Before that nothing is written: a save would replace them with what this browser holds. */
let prefsSynced = false;
/** Changes made while the preferences of the server are not known; they are applied on top once they are. */
let pendingLocal: Partial<Prefs> = {};

function scheduleSave(): void {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(flushPrefs, 800);
}

export async function flushPrefs(): Promise<void> {
  clearTimeout(saveTimer);
  if (!prefsSynced) return;
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

/** Merges into the preferences and saves them after a pause (once the preferences of the server are known). */
export function updatePrefs(patch: Partial<Prefs>): void {
  prefs.value = { ...prefs.value, ...patch };
  if (prefsSynced) scheduleSave();
  else pendingLocal = { ...pendingLocal, ...patch };
}

/** Loads the preferences. Returns false when that failed; it is tried again with the next reconnect. */
export async function loadPrefs(): Promise<boolean> {
  try {
    const p = await get<Prefs>("/prefs");
    const server = p && typeof p === "object" && !Array.isArray(p) ? p : {};
    const pending = pendingLocal;
    pendingLocal = {};
    prefs.value = { ...server, ...pending };
    prefsSynced = true;
    if (Object.keys(pending).length) scheduleSave();
  } catch {
    return false;
  }
  // Server preferences win over what this browser remembered.
  if (prefs.value.theme === "dark" || prefs.value.theme === "light") applyTheme(prefs.value.theme);
  if (prefs.value.lang === "de" || prefs.value.lang === "en") {
    setLangChoice(prefs.value.lang);
    storeLocal("jdweb.lang", prefs.value.lang);
  }
  return true;
}

export function prefsAreSynced(): boolean {
  return prefsSynced;
}

/** Forgets what belongs to a signed-in user. */
export function resetPrefs(): void {
  clearTimeout(saveTimer);
  prefsSynced = false;
  pendingLocal = {};
  prefs.value = {};
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

/** The captcha dialog is open. */
export const captchaDialogOpen = signal(false);
/** The questions of JDownloader are open. */
export const questionDialogOpen = signal(false);
/** The panel with the extraction jobs is open. */
export const extractionOpen = signal(false);
/** JDownloader was asked to restart or exit; the banner says that it comes back by itself. */
export const jdRestarting = signal(false);
export const helpOpen = signal(false);
