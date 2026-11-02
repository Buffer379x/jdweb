// The visible rows of a list: packages in order, their links when expanded,
// reduced by the filter and ordered by the sort. Pure.

import type { Model, HasId, HasPackage } from "./listmodel.ts";
import { linkKey, pkgKey } from "./selection.ts";

export interface Row {
  key: string;
  kind: "pkg" | "link";
  id: string;
  packageId: string;
}

export type SortDir = "asc" | "desc";
export interface Sort {
  col: string;
  dir: SortDir;
}

export interface RowOptions<P, L> {
  expanded: ReadonlySet<string>;
  /** Lower-case search words, all of them must be found. */
  words: readonly string[];
  /** The text of an entry that the filter looks at. */
  textOfPackage: (p: P) => string;
  textOfLink: (l: L) => string;
  sort: Sort | null;
  sortValuePackage: (p: P, col: string) => string | number;
  sortValueLink: (l: L, col: string) => string | number;
}

export function filterWords(query: string): string[] {
  return query.toLowerCase().split(/\s+/).filter(Boolean);
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

function compare(a: string | number, b: string | number): number {
  if (typeof a === "number" && typeof b === "number") return a - b;
  return collator.compare(String(a), String(b));
}

/** Sorts ids by a value that is worked out once per id, not once per comparison. */
function sorted(ids: readonly string[], dir: SortDir, value: (id: string) => string | number): string[] {
  const keyed = ids.map((id, i) => ({ id, i, v: value(id) }));
  const sign = dir === "asc" ? 1 : -1;
  // Equal values keep the order of the list, so that the view does not jump.
  keyed.sort((a, b) => sign * compare(a.v, b.v) || a.i - b.i);
  return keyed.map((k) => k.id);
}

function matches(text: string, words: readonly string[]): boolean {
  const t = text.toLowerCase();
  return words.every((w) => t.includes(w));
}

/**
 * With a filter, a package shows when it matches itself (then all its links
 * follow the expanded state) or when some link matches (then it opens and
 * shows only the matching links).
 */
export function buildRows<P extends HasId, L extends HasPackage>(m: Model<P, L>, o: RowOptions<P, L>): Row[] {
  const filtering = o.words.length > 0;
  let pkgIds = m.order;
  if (o.sort) {
    const { col } = o.sort;
    pkgIds = sorted(pkgIds, o.sort.dir, (id) => o.sortValuePackage(m.pkgs.get(id)!, col));
  }
  const rows: Row[] = [];
  for (const pid of pkgIds) {
    const p = m.pkgs.get(pid)!;
    let kids = m.children.get(pid) ?? [];
    let open = o.expanded.has(pid);
    if (filtering) {
      const own = matches(o.textOfPackage(p), o.words);
      if (!own) {
        kids = kids.filter((id) => matches(o.textOfLink(m.links.get(id)!), o.words));
        if (kids.length === 0) continue;
        open = true;
      }
    }
    rows.push({ key: pkgKey(pid), kind: "pkg", id: pid, packageId: pid });
    if (!open) continue;
    if (o.sort) {
      const { col } = o.sort;
      kids = sorted(kids, o.sort.dir, (id) => o.sortValueLink(m.links.get(id)!, col));
    }
    for (const id of kids) rows.push({ key: linkKey(id), kind: "link", id, packageId: pid });
  }
  return rows;
}
