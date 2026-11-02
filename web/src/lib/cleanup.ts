// What a cleanup would touch, so the confirmation can state the count.

import type { CleanupAction, CleanupSelection, LinkState } from "../api/types.ts";
import type { Model, HasId, HasPackage } from "./listmodel.ts";
import { idOfKey, isPkgKey } from "./selection.ts";

export interface CleanupLink {
  packageId: string;
  state?: LinkState;
  enabled: boolean;
  availability?: string;
  /** How JDownloader ended the download (downloads only). */
  finalState?: string;
}

/** Whether a link is hit by an action; null when only JDownloader can tell (duplicates). */
function matchesAction(l: CleanupLink, a: CleanupAction): boolean | null {
  switch (a) {
    case "DELETE_ALL": return true;
    case "DELETE_DISABLED": return !l.enabled;
    case "DELETE_FINISHED": return l.state === "finished";
    // Recorded on a real JDownloader: it removes the links that ended as failed (OFFLINE included). Whether it
    // also takes links with an extraction error is not known, so those make the count unknown.
    case "DELETE_FAILED": return l.state === "extractError" ? null : l.state === "failed";
    // Offline in the linkgrabber is an availability; in the downloads it is a download that ended as OFFLINE.
    case "DELETE_OFFLINE": return l.availability === "OFFLINE" || l.finalState === "OFFLINE";
    case "DELETE_DUPE": return null;
  }
}

/** The number of links the cleanup affects, or null when it cannot be known here (duplicates). */
export function cleanupCount<P extends HasId, L extends HasPackage & CleanupLink>(
  m: Model<P, L>,
  action: CleanupAction,
  selection: CleanupSelection,
  selected: ReadonlySet<string>,
): number | null {
  if (action === "DELETE_DUPE") return null;
  const selLinks = new Set<string>();
  for (const k of selected) {
    if (isPkgKey(k)) for (const id of m.children.get(idOfKey(k)) ?? []) selLinks.add(id);
    else selLinks.add(idOfKey(k));
  }
  let n = 0;
  for (const l of m.links.values()) {
    const inSel = selLinks.has(l.id);
    if (selection === "SELECTED" && !inSel) continue;
    if (selection === "UNSELECTED" && inSel) continue;
    if (selection === "NONE") continue;
    const hit = matchesAction(l, action);
    if (hit === null) return null;
    if (hit) n++;
  }
  return n;
}
