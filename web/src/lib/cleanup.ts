// What a cleanup would touch, so the confirmation can state the count.

import type { CleanupAction, CleanupSelection, LinkState } from "../api/types.ts";
import type { Model, HasId, HasPackage } from "./listmodel.ts";
import { idOfKey, isPkgKey } from "./selection.ts";

export interface CleanupLink {
  packageId: string;
  state?: LinkState;
  enabled: boolean;
  availability?: string;
}

function matchesAction(l: CleanupLink, a: CleanupAction): boolean | null {
  switch (a) {
    case "DELETE_ALL": return true;
    case "DELETE_DISABLED": return !l.enabled;
    case "DELETE_FINISHED": return l.state === "finished";
    case "DELETE_FAILED": return l.state === "extractError";
    case "DELETE_OFFLINE": return l.availability === "OFFLINE";
    case "DELETE_DUPE": return null; // only JDownloader knows
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
    if (matchesAction(l, action)) n++;
  }
  return n;
}
