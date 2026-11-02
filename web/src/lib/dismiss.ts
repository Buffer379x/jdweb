// What Escape, the X and Cancel do in a dialog with input.

export type Dismissal = "ignore" | "close" | "ask";

/** A submit in flight is not interrupted; input that was typed is asked about; otherwise the dialog closes. */
export function dismissal(dirty: boolean, busy: boolean): Dismissal {
  if (busy) return "ignore";
  return dirty ? "ask" : "close";
}
