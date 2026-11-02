// Where a move goes. The API names the entry behind which the moved ones are
// put ("-1" is the top), so "one up" needs the neighbor of the neighbor.

export type MoveDir = "top" | "up" | "down" | "bottom";

/**
 * `siblings` are the ids in list order, `selected` the ones to move. Returns
 * the id to put them behind ("-1" for the top), or null when nothing would change.
 */
export function moveTarget(siblings: readonly string[], selected: ReadonlySet<string>, dir: MoveDir): string | null {
  const rest = siblings.filter((id) => !selected.has(id));
  const idx = siblings.map((id, i) => (selected.has(id) ? i : -1)).filter((i) => i >= 0);
  if (idx.length === 0) return null;
  const first = idx[0]!;
  const last = idx[idx.length - 1]!;
  const contiguous = last - first + 1 === idx.length;
  const before = (i: number) => siblings.slice(0, i).filter((id) => !selected.has(id)).length;
  switch (dir) {
    case "top":
      return contiguous && first === 0 ? null : "-1";
    case "bottom":
      return contiguous && last === siblings.length - 1 ? null : (rest[rest.length - 1] ?? null);
    case "up": {
      const k = before(first);
      if (k === 0) return null;
      return k - 1 === 0 ? "-1" : rest[k - 2]!;
    }
    case "down": {
      const k = before(last);
      if (k >= rest.length) return null;
      return rest[k]!;
    }
  }
}

/**
 * Where a drop puts the moved entries. `target` is the sibling that was dropped on, `pos` the half of
 * it. Returns the id to put them behind ("-1" for the top), or null when it would not move.
 */
export function dropAfter(siblings: readonly string[], selected: ReadonlySet<string>, target: string, pos: "before" | "after"): string | null {
  if (selected.has(target)) return null;
  const rest = siblings.filter((id) => !selected.has(id));
  const k = rest.indexOf(target);
  if (k < 0) return null;
  const after = pos === "after" ? target : k === 0 ? "-1" : rest[k - 1]!;
  // A drop that leaves the order as it was is no move.
  const moved = siblings.filter((id) => selected.has(id));
  const without = siblings.filter((id) => !selected.has(id));
  const at = after === "-1" ? 0 : without.indexOf(after) + 1;
  const result = [...without.slice(0, at), ...moved, ...without.slice(at)];
  return result.every((id, i) => id === siblings[i]) ? null : after;
}

export type DropPlan =
  | { kind: "packages"; after: string }
  | { kind: "links"; after: string; destPackageId: string }
  /** The drop leaves the order as it is. */
  | { kind: "noop" }
  /** The drop is not allowed (packages onto a link, nothing dragged). */
  | { kind: "invalid" };

export interface DropInput {
  /** What is dragged, decided when the drag started. */
  dragged: "package" | "link";
  ids: ReadonlySet<string>;
  target: { kind: "pkg" | "link"; id: string; packageId: string };
  pos: "before" | "after" | "into";
  /** Package ids in list order. */
  order: readonly string[];
  children: (packageId: string) => readonly string[];
  /** Whether the target package shows its links. */
  targetExpanded: boolean;
}

/** Where a drop puts the dragged entries, as an action of docs/api.md, or why it does nothing. */
export function planDrop(d: DropInput): DropPlan {
  if (d.ids.size === 0) return { kind: "invalid" };
  if (d.dragged === "package") {
    // Packages only go between packages. A link row is no place for them.
    if (d.target.kind !== "pkg" || d.pos === "into") return { kind: "invalid" };
    const after = dropAfter(d.order, d.ids, d.target.id, d.pos);
    return after === null ? { kind: "noop" } : { kind: "packages", after };
  }
  const dest = d.target.packageId;
  const siblings = d.children(dest);
  const rest = siblings.filter((id) => !d.ids.has(id));
  let after: string;
  if (d.target.kind === "pkg") {
    // Onto a package: to the top of an open one, to the end of a closed one.
    after = d.targetExpanded ? "-1" : (rest[rest.length - 1] ?? "-1");
  } else {
    if (d.ids.has(d.target.id)) return { kind: "noop" };
    const k = rest.indexOf(d.target.id);
    if (k < 0) return { kind: "invalid" };
    after = d.pos === "after" ? d.target.id : k === 0 ? "-1" : rest[k - 1]!;
  }
  // Only entries that are in the package already can be a no-op: keep their order and see.
  if (d.ids.size === siblings.filter((id) => d.ids.has(id)).length) {
    const at = after === "-1" ? 0 : rest.indexOf(after) + 1;
    const moved = siblings.filter((id) => d.ids.has(id));
    const next = [...rest.slice(0, at), ...moved, ...rest.slice(at)];
    if (next.every((id, i) => id === siblings[i])) return { kind: "noop" };
  }
  return { kind: "links", after, destPackageId: dest };
}
