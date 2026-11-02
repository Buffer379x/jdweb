// Small line icons on a 16 px grid. Square caps, no rounding: the look of the control desk.

export type IconName =
  | "play" | "pause" | "stop" | "plus" | "search" | "x" | "right" | "down" | "check" | "more"
  | "sun" | "moon" | "sortAsc" | "sortDesc" | "back" | "menu" | "grip" | "upload" | "details" | "gear";

const FILLED: Partial<Record<IconName, string>> = {
  play: "M4 2.5v11l9.5-5.5z",
  pause: "M3.5 2.5h3v11h-3zM9.5 2.5h3v11h-3z",
  stop: "M3.5 3.5h9v9h-9z",
  sortAsc: "M4 10l4-5 4 5z",
  sortDesc: "M4 6l4 5 4-5z",
  more: "M2.5 7h2.5v2H2.5zM6.75 7h2.5v2h-2.5zM11 7h2.5v2H11z",
  grip: "M5 3h2v2H5zM9 3h2v2H9zM5 7h2v2H5zM9 7h2v2H9zM5 11h2v2H5zM9 11h2v2H9z",
};

const STROKED: Partial<Record<IconName, string>> = {
  plus: "M8 2.5v11M2.5 8h11",
  x: "M3.5 3.5l9 9M12.5 3.5l-9 9",
  right: "M6 3l5 5-5 5",
  down: "M3 6l5 5 5-5",
  back: "M10 3L5 8l5 5",
  check: "M2.5 8.5l3.5 3.5 7.5-8",
  search: "M7 2.5a4.5 4.5 0 100 9 4.5 4.5 0 000-9zM10.5 10.5l3.5 3.5",
  sun: "M8 4.5a3.5 3.5 0 100 7 3.5 3.5 0 000-7zM8 1v2M8 13v2M1 8h2M13 8h2M3 3l1.4 1.4M11.6 11.6L13 13M3 13l1.4-1.4M11.6 4.4L13 3",
  moon: "M12.5 9.5A5.5 5.5 0 016.5 3.5a5.5 5.5 0 106 6z",
  menu: "M2 4h12M2 8h12M2 12h12",
  upload: "M8 11V2.5M4.5 6L8 2.5 11.5 6M2.5 13.5h11",
  details: "M2.5 2.5h11v11h-11zM9.5 2.5v11",
  gear: "M8 5.5a2.5 2.5 0 100 5 2.5 2.5 0 000-5zM8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2",
};

export function Icon({ name, size = 16 }: { name: IconName; size?: number }) {
  const f = FILLED[name];
  const s = STROKED[name];
  return (
    <svg class="icon" viewBox="0 0 16 16" width={size} height={size} aria-hidden="true" focusable="false">
      {f ? <path d={f} fill="currentColor" /> : null}
      {s ? <path d={s} fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="square" stroke-linejoin="miter" /> : null}
    </svg>
  );
}
