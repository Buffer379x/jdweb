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
