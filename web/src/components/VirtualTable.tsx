import type { ComponentChildren, JSX } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { Icon } from "./Icon.tsx";
import type { Sort } from "../lib/rows.ts";

export interface Column {
  key: string;
  label: string;
  sortable?: boolean;
  align?: "end";
  /** The name of the grid area, see styles. */
  area: string;
}

interface Props {
  label: string;
  columns: Column[];
  rowCount: number;
  rowHeight: number;
  overscan?: number;
  sort: Sort | null;
  onSort: (col: string) => void;
  /** Index of the row that has the keyboard focus, or -1. */
  focusIndex: number;
  /** The id of the focused row's element, for aria-activedescendant. */
  focusId: string | undefined;
  /** The leading header cell (select-all box). */
  selectAll: ComponentChildren;
  /** Renders rows start..end-1. Each must be an element with role="row". */
  renderRows: (start: number, end: number) => ComponentChildren;
  onKeyDown: (e: KeyboardEvent) => void;
  onContextMenu: (e: MouseEvent) => void;
  listClass: string;
  empty?: ComponentChildren;
  /** Set to a number to bring that row index into view. */
  revealRequest: { index: number; n: number } | null;
}

type Vars = JSX.CSSProperties & Record<`--${string}`, string>;

/**
 * A grid of fixed-height rows of which only the visible ones exist in the DOM.
 * The rows sit between two spacers whose height is a custom property, so
 * scrolling a long list costs a handful of elements and no inline layout.
 */
export function VirtualTable(p: Props) {
  const box = useRef<HTMLDivElement>(null);
  const head = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState(0);
  const [height, setHeight] = useState(600);
  const [headH, setHeadH] = useState(30);
  const raf = useRef(0);
  const overscan = p.overscan ?? 8;

  useLayoutEffect(() => {
    const el = box.current!;
    const measure = () => {
      setHeight(el.clientHeight);
      setHeadH(head.current?.offsetHeight ?? 30);
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useEffect(() => () => cancelAnimationFrame(raf.current), []);

  const onScroll = () => {
    if (raf.current) return;
    raf.current = requestAnimationFrame(() => {
      raf.current = 0;
      setTop(box.current!.scrollTop);
    });
  };

  // Bring a requested row into view, below the sticky header.
  useEffect(() => {
    const r = p.revealRequest;
    const el = box.current;
    if (!r || !el || r.index < 0) return;
    const y = r.index * p.rowHeight;
    const viewTop = el.scrollTop;
    const viewH = el.clientHeight - headH;
    if (y < viewTop) el.scrollTop = y;
    else if (y + p.rowHeight > viewTop + viewH) el.scrollTop = y + p.rowHeight - viewH;
    setTop(el.scrollTop);
  }, [p.revealRequest]);

  const start = Math.max(0, Math.floor(top / p.rowHeight) - overscan);
  const end = Math.min(p.rowCount, Math.ceil((top + height) / p.rowHeight) + overscan);

  return (
    <div
      ref={box}
      class={`vt ${p.listClass}`}
      role="grid"
      aria-label={p.label}
      aria-rowcount={p.rowCount + 1}
      aria-colcount={p.columns.length + 1}
      aria-multiselectable="true"
      aria-activedescendant={p.focusId}
      tabIndex={0}
      onScroll={onScroll}
      onKeyDown={p.onKeyDown}
      onContextMenu={p.onContextMenu}
    >
      <div ref={head} class="vt-head" role="row" aria-rowindex={1}>
        <div class="vt-cell c-check" role="columnheader">{p.selectAll}</div>
        {p.columns.map((c) => {
          const active = p.sort?.col === c.key;
          return (
            <div
              key={c.key}
              class={`vt-cell c-${c.area}${c.align ? " end" : ""}${active ? " sorted" : ""}`}
              role="columnheader"
              aria-sort={active ? (p.sort!.dir === "asc" ? "ascending" : "descending") : undefined}
            >
              {c.sortable ? (
                <button type="button" class="sort-btn" onClick={() => p.onSort(c.key)}>
                  <span>{c.label}</span>
                  {active ? <Icon name={p.sort!.dir === "asc" ? "sortAsc" : "sortDesc"} size={12} /> : null}
                </button>
              ) : (
                c.label
              )}
            </div>
          );
        })}
      </div>
      <div class="vt-body" role="rowgroup">
        <div class="vt-spacer" style={{ "--h": `${start * p.rowHeight}px` } as Vars} aria-hidden="true" />
        {p.renderRows(start, end)}
        <div class="vt-spacer" style={{ "--h": `${Math.max(0, p.rowCount - end) * p.rowHeight}px` } as Vars} aria-hidden="true" />
      </div>
      {p.rowCount === 0 ? p.empty : null}
    </div>
  );
}
