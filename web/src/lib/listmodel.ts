// A list (packages and their links) as normalized maps plus ordered arrays.
// Patches replace single entries; they never rebuild the list.

export interface HasId {
  id: string;
}
export interface HasPackage extends HasId {
  packageId: string;
}

export interface Model<P extends HasId, L extends HasPackage> {
  pkgs: Map<string, P>;
  links: Map<string, L>;
  /** Package ids in list order. */
  order: string[];
  /** Link ids per package, in list order. */
  children: Map<string, string[]>;
}

export function emptyModel<P extends HasId, L extends HasPackage>(): Model<P, L> {
  return { pkgs: new Map(), links: new Map(), order: [], children: new Map() };
}

/** Builds the model from an answer of GET /downloads or /linkgrabber. Links of an unknown package are dropped. */
export function buildModel<P extends HasId, L extends HasPackage>(packages: readonly P[], links: readonly L[]): Model<P, L> {
  const m = emptyModel<P, L>();
  for (const p of packages) {
    if (m.pkgs.has(p.id)) continue;
    m.pkgs.set(p.id, p);
    m.order.push(p.id);
    m.children.set(p.id, []);
  }
  for (const l of links) {
    const kids = m.children.get(l.packageId);
    if (!kids || m.links.has(l.id)) continue;
    m.links.set(l.id, l);
    kids.push(l.id);
  }
  return m;
}

export interface PatchResult {
  changedPackages: Set<string>;
  changedLinks: Set<string>;
  /** The patch named an entry the model does not have: the list is out of date and must be loaded again. */
  unknown: boolean;
  /** Names of the fields the patch touched, for deciding whether filter or sort are affected. */
  fields: Set<string>;
}

interface PatchLike {
  links?: Record<string, Record<string, unknown>> | null;
  packages?: Record<string, Record<string, unknown>> | null;
}

/** Applies a patch event. Fields that cannot move a link (id, packageId) are ignored. */
export function applyPatch<P extends HasId, L extends HasPackage>(m: Model<P, L>, patch: PatchLike): PatchResult {
  const res: PatchResult = { changedPackages: new Set(), changedLinks: new Set(), unknown: false, fields: new Set() };
  const merge = <T extends HasId>(old: T, fields: Record<string, unknown>, into: Set<string>) => {
    let next: T | undefined;
    for (const [k, v] of Object.entries(fields)) {
      if (k === "id" || k === "packageId") continue;
      if ((old as Record<string, unknown>)[k] === v) continue;
      next ??= { ...old };
      (next as Record<string, unknown>)[k] = v;
      res.fields.add(k);
    }
    if (next) into.add(old.id);
    return next ?? old;
  };
  for (const [id, f] of Object.entries(patch.packages ?? {})) {
    const old = m.pkgs.get(id);
    if (!old) {
      res.unknown = true;
      continue;
    }
    const next = merge(old, f, res.changedPackages);
    if (next !== old) m.pkgs.set(id, next);
  }
  for (const [id, f] of Object.entries(patch.links ?? {})) {
    const old = m.links.get(id);
    if (!old) {
      res.unknown = true;
      continue;
    }
    const next = merge(old, f, res.changedLinks);
    if (next !== old) m.links.set(id, next);
  }
  return res;
}
