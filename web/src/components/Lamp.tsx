import type { Tone } from "../store/linkinfo.ts";

/** A small square lamp. The text next to it carries the meaning, the color only supports it. */
export function Lamp({ tone, pulse }: { tone: Tone | "accent"; pulse?: boolean }) {
  return <i class={`lamp tone-${tone}${pulse ? " pulse" : ""}`} aria-hidden="true" />;
}
