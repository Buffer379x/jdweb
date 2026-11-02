// What a row says about the state of its entry: text and tone. Texts of the
// app go through the dictionaries; `status` from JDownloader is shown as it is.

import type { DlLink, DlPackage, LgLink, LgPackage } from "../api/types.ts";
import { formatSpeed } from "../lib/format.ts";
import type { Summary } from "../lib/segments.ts";
import { lang, plural, t } from "../i18n/index.ts";

export type Tone = "run" | "wait" | "err" | "info" | "done" | "muted";
export interface StateText {
  text: string;
  tone: Tone;
  /** The text is a number (speed): set it in the monospace face. */
  mono?: boolean;
  /** Extra text for the tooltip. */
  title?: string;
}

export function humanReason(r: string): string {
  return r.replace(/_/g, " ").toLowerCase();
}

/** `live` is false while JDownloader is offline: nothing is shown as running then. */
export function dlLinkStatus(l: DlLink, live: boolean): StateText {
  switch (l.state) {
    case "disabled":
      return { text: t("state.disabled"), tone: "muted" };
    case "extractError":
      return { text: t("state.extractError"), tone: "err", title: l.extractionStatus };
    case "finished":
      return { text: l.status || t("state.finished"), tone: "done" };
    case "skipped": {
      const reason = l.skipReason ? humanReason(l.skipReason) : "";
      return { text: reason ? t("state.skippedReason", { reason }) : t("state.skipped"), tone: "wait", title: l.status };
    }
    case "running":
      if (!live) return { text: t("state.interrupted"), tone: "muted" };
      // A running link without speed is busy with something else (waiting for a captcha, connecting): say what JDownloader says.
      if (l.speed <= 0 && l.status) return { text: l.status, tone: "run" };
      return { text: formatSpeed(l.speed, lang.value), tone: "run", mono: true };
    default:
      return { text: t("state.waiting"), tone: "muted", title: l.status };
  }
}

export function dlPackageStatus(p: DlPackage, s: Summary, live: boolean): StateText {
  if (!p.enabled) return { text: t("state.disabled"), tone: "muted" };
  if (s.errors > 0) return { text: plural("pkg.errors", s.errors), tone: "err" };
  if (p.finished || (s.total > 0 && s.finished === s.total)) return { text: t("state.finished"), tone: "done" };
  if (s.running > 0 || p.running) {
    if (!live) return { text: t("state.interrupted"), tone: "muted" };
    return { text: t("state.running"), tone: "run" };
  }
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
  return { extractError: 0, running: 1, skipped: 2, waiting: 3, finished: 4, disabled: 5 }[state] ?? 6;
}
