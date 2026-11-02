import type { ComponentChildren, JSX } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

type Vars = JSX.CSSProperties & Record<`--${string}`, string>;

interface Props {
  label: string;
  count: number;
  rowHeight: number;
  overscan?: number;
  renderRow: (index: number) => ComponentChildren;
  class?: string;
}

/**
 * A list of rows of one fixed height of which only the visible ones are in the page. Two spacers hold the
 * place of the rest (their height is a custom property, no inline layout). Rows carry aria-posinset and
 * aria-setsize, so a screen reader knows where it is in the whole list.
 */
export function VirtualList({ label, count, rowHeight, overscan = 6, renderRow, class: cls }: Props) {
  const box = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState(0);
  const [height, setHeight] = useState(600);
  const raf = useRef(0);

  useLayoutEffect(() => {
    const el = box.current!;
    const measure = () => setHeight(el.clientHeight);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  useEffect(() => () => cancelAnimationFrame(raf.current), []);
  // A new filter starts at the top.
  useEffect(() => {
    if (box.current) box.current.scrollTop = 0;
    setTop(0);
  }, [count]);

  const onScroll = () => {
    if (raf.current) return;
    raf.current = requestAnimationFrame(() => {
      raf.current = 0;
      setTop(box.current!.scrollTop);
    });
  };
  const start = Math.max(0, Math.floor(top / rowHeight) - overscan);
  const end = Math.min(count, Math.ceil((top + height) / rowHeight) + overscan);
  const rows: ComponentChildren[] = [];
  for (let i = start; i < end; i++) rows.push(renderRow(i));
  return (
    <div ref={box} class={`vlist ${cls ?? ""}`} role="list" aria-label={label} onScroll={onScroll}>
      <div class="vt-spacer" style={{ "--h": `${start * rowHeight}px` } as Vars} aria-hidden="true" />
      {rows}
      <div class="vt-spacer" style={{ "--h": `${Math.max(0, count - end) * rowHeight}px` } as Vars} aria-hidden="true" />
    </div>
  );
}
