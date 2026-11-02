// Extraction jobs: how far the parts of an archive are, in words and as segments.

export interface QueueEntry {
  archiveId: string;
  archiveName: string;
  type: string;
  controllerId: number;
  controllerStatus: string;
  states: Record<string, string> | null;
}

export type PartState = "COMPLETE" | "INCOMPLETE" | "MISSING";

export function partCounts(states: Record<string, string> | null | undefined): { complete: number; total: number; missing: number } {
  let complete = 0;
  let missing = 0;
  let total = 0;
  for (const v of Object.values(states ?? {})) {
    total++;
    if (v === "COMPLETE") complete++;
    else if (v === "MISSING") missing++;
  }
  return { complete, total, missing };
}

export type JobState = "running" | "queued" | "idle";

export function jobState(status: string): JobState {
  const s = status.toUpperCase();
  if (s === "RUNNING") return "running";
  if (s === "QUEUED" || s === "WAITING") return "queued";
  return "idle";
}

/** Only an entry with a running controller can be cancelled; -1 means there is none. */
export function canCancel(e: Pick<QueueEntry, "controllerId">): boolean {
  return e.controllerId >= 0;
}

/** The archive part files in name order, with the state of each, for the segments. */
export function partList(states: Record<string, string> | null | undefined): { name: string; state: string }[] {
  return Object.entries(states ?? {}).map(([name, state]) => ({ name, state })).sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));
}

export interface ArchiveSettings {
  archiveId: string;
  extractPath: string;
  finalPassword: string;
  passwords: string[];
  autoExtract: boolean;
  removeDownloadLinksAfterExtraction: boolean;
  removeFilesAfterExtraction: boolean;
  secret: boolean;
}

export interface ArchiveInfo extends QueueEntry {
  settings: ArchiveSettings | null;
}
