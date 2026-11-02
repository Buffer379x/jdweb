// Archive settings: what the form changes and what is sent. Secrets are only sent when they were changed.

import { MASK, holdsMask } from "./editors.ts";
import type { ArchiveSettings } from "./extraction.ts";

/** One password per line; blank lines and doubles go. */
export function splitPasswords(text: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of text.split(/\r?\n/)) {
    const p = raw.trim();
    if (p !== "" && !seen.has(p)) {
      seen.add(p);
      out.push(p);
    }
  }
  return out;
}

export interface ArchiveEdit {
  extractPath: string;
  autoExtract: boolean;
  removeDownloadLinksAfterExtraction: boolean;
  removeFilesAfterExtraction: boolean;
  /** null: not touched (the stored value stays). */
  finalPassword: string | null;
  /** null: not touched. A text with one password per line replaces the stored list. */
  passwords: string | null;
}

export type ArchivePatch = Partial<Pick<ArchiveSettings, "extractPath" | "finalPassword" | "passwords" | "autoExtract" | "removeDownloadLinksAfterExtraction" | "removeFilesAfterExtraction">>;

/** The fields to send: only those that differ, and never the mask. */
export function buildPatch(orig: ArchiveSettings, edit: ArchiveEdit): ArchivePatch {
  const p: ArchivePatch = {};
  if (edit.extractPath !== orig.extractPath) p.extractPath = edit.extractPath;
  if (edit.autoExtract !== orig.autoExtract) p.autoExtract = edit.autoExtract;
  if (edit.removeDownloadLinksAfterExtraction !== orig.removeDownloadLinksAfterExtraction) p.removeDownloadLinksAfterExtraction = edit.removeDownloadLinksAfterExtraction;
  if (edit.removeFilesAfterExtraction !== orig.removeFilesAfterExtraction) p.removeFilesAfterExtraction = edit.removeFilesAfterExtraction;
  if (edit.finalPassword !== null && edit.finalPassword !== MASK && edit.finalPassword !== orig.finalPassword) p.finalPassword = edit.finalPassword;
  if (edit.passwords !== null) {
    const list = splitPasswords(edit.passwords);
    if (!holdsMask(list) && JSON.stringify(list) !== JSON.stringify(orig.passwords)) p.passwords = list;
  }
  return p;
}

/** Whether the server holds a value it has masked. */
export function hasSecret(v: string | string[]): boolean {
  return Array.isArray(v) ? v.length > 0 : v !== "";
}
