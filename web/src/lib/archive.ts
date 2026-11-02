// Archive settings: what the form changes and what is sent. Secrets are only sent when they were changed.

import { post } from "../api/client.ts";
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
  /** The user chose "Remove" for the password: the only way to delete it. An empty field does not. */
  clearFinalPassword?: boolean;
  /** The same for the list. */
  clearPasswords?: boolean;
}

export interface Revealed {
  /** The settings with the real secrets: what a later edit is compared with, so that showing changes nothing. */
  base: ArchiveSettings;
  finalPassword: string;
  /** One password per line, exactly the stored list. */
  passwords: string;
}

/** Asks the server for the real secrets of an archive. */
export async function revealArchive(archiveId: string, s: ArchiveSettings): Promise<Revealed> {
  const r = await post<{ finalPassword?: string; passwords?: string[] }>(`/extraction/archives/${encodeURIComponent(archiveId)}/reveal`, {});
  const finalPassword = r.finalPassword ?? "";
  const passwords = r.passwords ?? [];
  return { base: { ...s, finalPassword, passwords }, finalPassword, passwords: passwords.join("\n") };
}

export type ArchivePatch = Partial<Pick<ArchiveSettings, "extractPath" | "finalPassword" | "passwords" | "autoExtract" | "removeDownloadLinksAfterExtraction" | "removeFilesAfterExtraction">>;

/**
 * The fields to send: only those that differ, and never the mask. An empty password field is no edit; deleting
 * a password is the explicit clear flags.
 */
export function buildPatch(orig: ArchiveSettings, edit: ArchiveEdit): ArchivePatch {
  const p: ArchivePatch = {};
  if (edit.extractPath !== orig.extractPath) p.extractPath = edit.extractPath;
  if (edit.autoExtract !== orig.autoExtract) p.autoExtract = edit.autoExtract;
  if (edit.removeDownloadLinksAfterExtraction !== orig.removeDownloadLinksAfterExtraction) p.removeDownloadLinksAfterExtraction = edit.removeDownloadLinksAfterExtraction;
  if (edit.removeFilesAfterExtraction !== orig.removeFilesAfterExtraction) p.removeFilesAfterExtraction = edit.removeFilesAfterExtraction;
  if (edit.clearFinalPassword) {
    if (orig.finalPassword !== "") p.finalPassword = "";
  } else if (edit.finalPassword !== null && edit.finalPassword !== "" && edit.finalPassword !== MASK && edit.finalPassword !== orig.finalPassword) {
    p.finalPassword = edit.finalPassword;
  }
  if (edit.clearPasswords) {
    if (orig.passwords.length > 0) p.passwords = [];
  } else if (edit.passwords !== null && edit.passwords.trim() !== "" && edit.passwords !== orig.passwords.join("\n")) {
    // Text that is still the stored list is no edit: cleaning it (trim, doubles) would change stored passwords unasked.
    const list = splitPasswords(edit.passwords);
    if (!holdsMask(list) && JSON.stringify(list) !== JSON.stringify(orig.passwords)) p.passwords = list;
  }
  return p;
}

/** Whether the server holds a value it has masked. */
export function hasSecret(v: string | string[]): boolean {
  return Array.isArray(v) ? v.length > 0 : v !== "";
}
