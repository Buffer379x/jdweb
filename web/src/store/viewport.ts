import { signal } from "@preact/signals";

// The breakpoints of Concept 11.5.
const mqCompact = matchMedia("(max-width: 639px)");
const mqWide = matchMedia("(min-width: 1100px)");
const mqCoarse = matchMedia("(pointer: coarse)");

/** Under 640 px: two-line rows, tabs and actions at the bottom. */
export const compact = signal(mqCompact.matches);
/** From 1100 px: all columns, details as a panel. */
export const wide = signal(mqWide.matches);
export const coarse = signal(mqCoarse.matches);

mqCompact.addEventListener("change", (e) => (compact.value = e.matches));
mqWide.addEventListener("change", (e) => (wide.value = e.matches));
mqCoarse.addEventListener("change", (e) => (coarse.value = e.matches));

/** Row heights in pixels; the same numbers are in the stylesheet as --row-h. */
export const ROW_H = 34;
export const ROW_H_COMPACT = 56;
