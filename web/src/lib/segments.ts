// The segment bar of a package (Concept 11.6): one segment per link, and from
// 25 links on 24 segments that each stand for a range of links. An error in a
// range colors the whole segment as an error.

import type { LinkState } from "../api/types.ts";

export const MAX_SEGMENTS = 24;

export type SegKind = "none" | "run" | "wait" | "done" | "err";

export interface Seg {
  /** 0 to 1, how much of the segment is filled. */
  fill: number;
  kind: SegKind;
}

export interface SegLink {
  state: LinkState;
  bytesLoaded: number;
  bytesTotal: number;
}

/** How far a single link is, 0 to 1. A finished link is full whatever the bytes say. */
export function linkFill(l: SegLink): number {
  if (l.state === "finished") return 1;
  if (l.state === "extractError") return 1;
  if (l.state === "disabled") return 0;
  if (!(l.bytesTotal > 0) || !(l.bytesLoaded > 0)) return 0;
  return Math.min(1, l.bytesLoaded / l.bytesTotal);
}

function segmentOf(group: readonly SegLink[]): Seg {
  let sum = 0;
  let err = false;
  let run = false;
  let skip = false;
  let done = false;
  for (const l of group) {
    sum += linkFill(l);
    if (l.state === "extractError") err = true;
    else if (l.state === "running") run = true;
    else if (l.state === "skipped") skip = true;
    else if (l.state === "finished") done = true;
  }
  const fill = group.length ? sum / group.length : 0;
  let kind: SegKind = "none";
  if (err) kind = "err";
  else if (run) kind = "run";
  else if (skip) kind = "wait";
  else if (done) kind = "done";
  else if (fill > 0) kind = "wait";
  // A group with an error is full: the point is to see it.
  return { fill: err ? 1 : fill, kind };
}

/** The segments of a package, in link order. */
export function segments(links: readonly SegLink[]): Seg[] {
  const n = links.length;
  if (n === 0) return [];
  if (n <= MAX_SEGMENTS) return links.map((l) => segmentOf([l]));
  const out: Seg[] = [];
  for (let i = 0; i < MAX_SEGMENTS; i++) {
    const from = Math.floor((i * n) / MAX_SEGMENTS);
    const to = Math.floor(((i + 1) * n) / MAX_SEGMENTS);
    out.push(segmentOf(links.slice(from, to)));
  }
  return out;
}

export interface Summary {
  total: number;
  finished: number;
  errors: number;
  running: number;
  skipped: number;
  disabled: number;
  waiting: number;
}

/** Counts per state, for the aria-label and the status text of a package. */
export function summarize(links: readonly { state: LinkState }[]): Summary {
  const s: Summary = { total: links.length, finished: 0, errors: 0, running: 0, skipped: 0, disabled: 0, waiting: 0 };
  for (const l of links) {
    switch (l.state) {
      case "finished": s.finished++; break;
      case "extractError": s.errors++; break;
      case "running": s.running++; break;
      case "skipped": s.skipped++; break;
      case "disabled": s.disabled++; break;
      default: s.waiting++;
    }
  }
  return s;
}
