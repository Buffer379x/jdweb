// Selection helpers. A selection is a set of row keys: "p<id>" for a package
// and "l<id>" for a link.

import type { Model, HasId, HasPackage } from "./listmodel.ts";

export const pkgKey = (id: string) => `p${id}`;
export const linkKey = (id: string) => `l${id}`;
export const isPkgKey = (k: string) => k.charCodeAt(0) === 112; // "p"
export const idOfKey = (k: string) => k.slice(1);

/** Keeps only keys of entries that still exist, so that a removed entry cannot be acted on. Returns the same set when nothing changed. */
export function pruneSelection<P extends HasId, L extends HasPackage>(sel: ReadonlySet<string>, m: Model<P, L>): ReadonlySet<string> {
  let out: Set<string> | undefined;
  for (const k of sel) {
    const ok = isPkgKey(k) ? m.pkgs.has(idOfKey(k)) : m.links.has(idOfKey(k));
    if (!ok) {
      out ??= new Set(sel);
      out.delete(k);
    }
  }
  return out ?? sel;
}

export interface Ids {
  packageIds: string[];
  linkIds: string[];
}

export function splitSelection(sel: ReadonlySet<string>): Ids {
  const packageIds: string[] = [];
  const linkIds: string[] = [];
  for (const k of sel) (isPkgKey(k) ? packageIds : linkIds).push(idOfKey(k));
  return { packageIds, linkIds };
}

/** Keys from index a to b (inclusive, either order) of the visible rows. */
export function rangeKeys(keys: readonly string[], a: number, b: number): string[] {
  if (a < 0 || b < 0) return [];
  const [from, to] = a <= b ? [a, b] : [b, a];
  return keys.slice(from, to + 1);
}

export function toggled(sel: ReadonlySet<string>, key: string): Set<string> {
  const out = new Set(sel);
  if (!out.delete(key)) out.add(key);
  return out;
}
