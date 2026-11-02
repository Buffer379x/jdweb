// One polite live region for the whole app (components/Announcer.tsx). A list of rows must not carry a live
// region per row: a screen reader would watch hundreds of them. Whoever has news calls announce().

import { signal } from "@preact/signals";

export const announcement = signal("");

let flip = false;

/** Says a short text politely. The same text twice in a row is still read: a trailing space alternates. */
export function announce(text: string): void {
  flip = !flip;
  announcement.value = flip ? text : `${text} `;
}
