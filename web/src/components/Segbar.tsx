import type { JSX } from "preact";
import type { Seg } from "../lib/segments.ts";

type Vars = JSX.CSSProperties & Record<`--${string}`, string>;

function pct(f: number): string {
  return `${Math.round(Math.max(0, Math.min(1, f)) * 1000) / 10}%`;
}

/**
 * The segment bar of a package. The fill of a segment is the custom property
 * --p, set through the CSSOM by Preact; the color is a class. No style attribute is written to the markup.
 */
export function Segbar({ segs, label }: { segs: readonly Seg[]; label: string }) {
  return (
    <span class="seg" role="img" aria-label={label}>
      {segs.map((s, i) => (
        <i key={i} class={`k-${s.kind}`} style={{ "--p": pct(s.fill) } as Vars} />
      ))}
    </span>
  );
}
