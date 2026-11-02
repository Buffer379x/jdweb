// What an action works on: the selection, or the focused row when nothing is selected.

import type { ListName } from "../api/types.ts";
import { idOfKey, isPkgKey, splitSelection } from "../lib/selection.ts";
import { plural } from "../i18n/index.ts";
import type { ListStore } from "./list.ts";
import type { LinkLike, PackageLike } from "../api/types.ts";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type AnyStore = ListStore<any, any>;

export interface Target {
  list: ListName;
  store: AnyStore;
  keys: ReadonlySet<string>;
  packageIds: string[];
  linkIds: string[];
  pkgs: PackageLike[];
  links: LinkLike[];
  /** Links of the selected packages plus the single links selected. */
  totalLinks: number;
}

export function targetOf(store: AnyStore): Target {
  let keys = store.selection.peek();
  if (keys.size === 0 && store.focusKey.peek()) keys = new Set([store.focusKey.peek()!]);
  const { packageIds, linkIds } = splitSelection(keys);
  const pkgs = packageIds.map((id) => store.model.pkgs.get(id)).filter(Boolean) as PackageLike[];
  const links = linkIds.map((id) => store.model.links.get(id)).filter(Boolean) as LinkLike[];
  let total = links.length;
  const inPkg = new Set(packageIds);
  total = links.filter((l) => !inPkg.has(l.packageId)).length;
  for (const id of packageIds) total += store.model.children.get(id)?.length ?? 0;
  return { list: store.name, store, keys, packageIds, linkIds, pkgs, links, totalLinks: total };
}

export function isEmpty(t: Target): boolean {
  return t.packageIds.length === 0 && t.linkIds.length === 0;
}

/** "3 Pakete mit 18 Links" or "5 Links". */
export function describe(t: Target): string {
  if (t.pkgs.length === 0) return plural("count.links", t.totalLinks);
  return `${plural("count.packages", t.pkgs.length)} ${plural("count.withLinks", t.totalLinks)}`;
}

export { idOfKey, isPkgKey };
