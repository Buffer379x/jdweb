// Formatters for sizes, speed, remaining time and percentages. Pure, the
// language is a parameter. Numbers follow the language ("1,84 GB" / "1.84 GB").

export type Lang = "de" | "en";

const UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];
const NBSP = " ";
const DASH = "–";

const nf = new Map<string, Intl.NumberFormat>();
function num(n: number, digits: number, lang: Lang): string {
  const key = `${lang}${digits}`;
  let f = nf.get(key);
  if (!f) {
    f = new Intl.NumberFormat(lang, { minimumFractionDigits: digits, maximumFractionDigits: digits });
    nf.set(key, f);
  }
  return f.format(n);
}

function known(n: number | null | undefined): n is number {
  return typeof n === "number" && Number.isFinite(n) && n >= 0;
}

/** Binary units like JDownloader: 1 KB is 1024 bytes. Null or negative is a dash. */
export function formatBytes(n: number | null | undefined, lang: Lang = "en"): string {
  if (!known(n)) return DASH;
  let v = n;
  let u = 0;
  while (v >= 1024 && u < UNITS.length - 1) {
    v /= 1024;
    u++;
  }
  if (u === 0) return `${Math.round(v)}${NBSP}B`;
  // Three significant digits at most, the way a dial would show them.
  const digits = v < 10 ? 2 : v < 100 ? 1 : 0;
  return `${num(v, digits, lang)}${NBSP}${UNITS[u]}`;
}

/** Speed in bytes per second. Zero is "0 B/s", unknown a dash. */
export function formatSpeed(n: number | null | undefined, lang: Lang = "en"): string {
  if (!known(n)) return DASH;
  return `${formatBytes(n, lang)}/s`;
}

/** Remaining time in seconds as mm:ss, h:mm:ss or "3 d 04 h". Null is a dash. */
export function formatEta(seconds: number | null | undefined): string {
  if (!known(seconds)) return DASH;
  const s = Math.floor(seconds);
  const pad = (x: number) => String(x).padStart(2, "0");
  if (s < 3600) return `${pad(Math.floor(s / 60))}:${pad(s % 60)}`;
  if (s < 86400) return `${Math.floor(s / 3600)}:${pad(Math.floor((s % 3600) / 60))}:${pad(s % 60)}`;
  return `${Math.floor(s / 86400)}${NBSP}d${NBSP}${pad(Math.floor((s % 86400) / 3600))}${NBSP}h`;
}

/** Whole percent, rounded down so that 99.9 % never reads as done. */
export function percent(loaded: number, total: number, finished = false): number {
  if (finished) return 100;
  if (!(total > 0) || !(loaded > 0)) return 0;
  return Math.max(0, Math.min(100, Math.floor((loaded / total) * 100)));
}

export function formatPercent(p: number, lang: Lang = "en"): string {
  return lang === "de" ? `${p}${NBSP}%` : `${p}%`;
}

/** Date and time in the local zone, empty for 0. */
export function formatDate(ms: number | null | undefined, lang: Lang = "en"): string {
  if (!ms || ms <= 0) return "";
  return new Intl.DateTimeFormat(lang, { dateStyle: "short", timeStyle: "short" }).format(new Date(ms));
}
