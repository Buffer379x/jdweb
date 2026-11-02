// What a row says about the state of its entry: text and tone. Texts of the
// app go through the dictionaries; the `status` text of JDownloader is shown as
// it is where it adds information, and is never evaluated.

import type { DlLink, DlPackage, LgLink, LgPackage } from "../api/types.ts";
import { formatSpeed } from "../lib/format.ts";
import type { Summary } from "../lib/segments.ts";
import { lang, plural, t, type Key } from "../i18n/index.ts";

export type Tone = "run" | "wait" | "err" | "info" | "done" | "muted";
export interface StateText {
  text: string;
  tone: Tone;
  /** The text is a number (speed): set it in the monospace face. */
  mono?: boolean;
  /** Extra text for the tooltip. */
  title?: string;
}

/** The first line of a text of JDownloader, cut short. Its status can be a Java stack trace. */
export function oneLine(s: string | undefined, max = 160): string {
  if (!s) return "";
  const first = s.split(/\r?\n/, 1)[0]!.trim();
  return first.length > max ? `${first.slice(0, max - 1)}…` : first;
}

const SKIP_REASONS = new Set([
  "CONNECTION_UNAVAILABLE", "TOO_MANY_RETRIES", "CAPTCHA", "MANUAL", "DISK_FULL", "NO_ACCOUNT",
  "INVALID_DESTINATION", "FILE_EXISTS", "UPDATE_RESTART_REQUIRED", "FFMPEG_MISSING", "FFPROBE_MISSING",
]);

/** The reason of a skipped link in the language of the app; one JDownloader added later is shown as its id in lower case. */
export function skipReasonText(r: string): string {
  if (SKIP_REASONS.has(r)) return t(`skip.${r}` as Key);
  return r.replace(/_/g, " ").toLowerCase();
}

/** `live` is false while JDownloader is offline: nothing is shown as running then. */
export function dlLinkStatus(l: DlLink, live: boolean): StateText {
  switch (l.state) {
    case "disabled":
      return { text: t("state.disabled"), tone: "muted" };
    case "extractError":
      return { text: t("state.extractError"), tone: "err", title: oneLine(l.extractionStatus) };
    case "failed":
      return { text: l.finalState === "OFFLINE" ? t("state.failedOffline") : t("state.failed"), tone: "err", title: oneLine(l.status) };
    case "finished":
      // The text of JDownloader says more when something followed the download ("Extraction OK"); otherwise it only says "Finished".
      if (l.extractionStatus && l.status) return { text: oneLine(l.status), tone: "done" };
      return { text: t("state.finished"), tone: "done", title: oneLine(l.status) };
    case "skipped":
      return { text: l.skipReason ? t("state.skippedReason", { reason: skipReasonText(l.skipReason) }) : t("state.skipped"), tone: "wait", title: oneLine(l.status) };
    case "running":
      if (!live) return { text: t("state.interrupted"), tone: "muted" };
      // A running link without speed is busy with something else (waiting for a captcha, connecting): say what JDownloader says.
      if (l.speed <= 0 && l.status) return { text: oneLine(l.status), tone: "run" };
      return { text: formatSpeed(l.speed, lang.value), tone: "run", mono: true };
    default:
      return { text: t("state.waiting"), tone: "muted", title: oneLine(l.status) };
  }
}

/** The state of a link as a word, for the filter. It does not change with the speed. */
export function dlLinkFilterText(l: DlLink): string {
  switch (l.state) {
    case "running": return t("state.running");
    case "finished": return t("state.finished");
    case "skipped": return `${t("state.skipped")} ${l.skipReason ? skipReasonText(l.skipReason) : ""}`;
    case "failed": return t(l.finalState === "OFFLINE" ? "state.failedOffline" : "state.failed");
    case "extractError": return t("state.extractError");
    case "disabled": return t("state.disabled");
    default: return t("state.waiting");
  }
}

export function dlPackageStatus(p: DlPackage, s: Summary, live: boolean): StateText {
  if (!p.enabled) return { text: t("state.disabled"), tone: "muted" };
  if (s.errors > 0) return { text: plural("pkg.errors", s.errors), tone: "err" };
  if (p.finished || (s.total > 0 && s.finished === s.total)) return { text: t("state.finished"), tone: "done" };
  if (s.running > 0 || (live && p.running)) {
    if (!live) return { text: t("state.interrupted"), tone: "muted" };
    return { text: t("state.running"), tone: "run" };
  }
  if (!live && p.running) return { text: t("state.interrupted"), tone: "muted" };
  if (s.skipped > 0 && s.skipped + s.finished === s.total) return { text: plural("pkg.skipped", s.skipped), tone: "wait" };
  return { text: t("state.waiting"), tone: "wait" };
}

export function lgAvailability(a: string): StateText {
  switch (a) {
    case "OFFLINE":
      return { text: t("avail.offline"), tone: "err" };
    case "UNKNOWN":
      return { text: t("avail.unknown"), tone: "info" };
    case "TEMP_UNKNOWN":
      return { text: t("avail.tempUnknown"), tone: "wait" };
    default:
      return { text: t("avail.online"), tone: "run" };
  }
}

export function lgLinkStatus(l: LgLink): StateText {
  const base = lgAvailability(l.availability);
  if (!l.enabled) return { text: t("state.disabled"), tone: "muted" };
  if (l.variant?.name) return { ...base, text: `${base.text} · ${l.variant.name}` };
  if (l.variants) return { ...base, text: `${base.text} · ${t("avail.variants")}` };
  return base;
}

export function lgPackageStatus(p: LgPackage): StateText {
  if (!p.enabled) return { text: t("state.disabled"), tone: "muted" };
  const parts: string[] = [];
  if (p.onlineCount) parts.push(t("avail.nOnline", { n: p.onlineCount }));
  if (p.offlineCount) parts.push(t("avail.nOffline", { n: p.offlineCount }));
  if (p.unknownCount + p.tempUnknownCount) parts.push(t("avail.nUnknown", { n: p.unknownCount + p.tempUnknownCount }));
  const tone: Tone = p.offlineCount ? "err" : p.unknownCount + p.tempUnknownCount ? "info" : "run";
  return { text: parts.join(", ") || t("avail.unknown"), tone };
}

/** Order of states for sorting a column: problems first, then what runs. */
export function stateRank(state: string): number {
  return { failed: 0, extractError: 0, running: 1, skipped: 2, waiting: 3, finished: 4, disabled: 5 }[state] ?? 6;
}
