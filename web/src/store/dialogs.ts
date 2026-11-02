import { signal } from "@preact/signals";
import type { ComponentChild } from "preact";

export interface DialogEntry {
  id: number;
  render: () => ComponentChild;
}

export const dialogStack = signal<DialogEntry[]>([]);
let seq = 0;

/** Opens a modal dialog. `render` gets the function that closes it. Returns that function too. */
export function openDialog(render: (close: () => void) => ComponentChild): () => void {
  const id = ++seq;
  const close = () => {
    dialogStack.value = dialogStack.value.filter((d) => d.id !== id);
  };
  dialogStack.value = [...dialogStack.value, { id, render: () => render(close) }];
  return close;
}

export function closeAllDialogs(): void {
  dialogStack.value = [];
}
