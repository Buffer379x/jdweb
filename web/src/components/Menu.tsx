import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { t } from "../i18n/index.ts";
import { Icon } from "./Icon.tsx";

export interface MenuItem {
  id: string;
  label: string;
  /** Shown right-aligned, for keys. */
  hint?: string;
  disabled?: boolean;
  danger?: boolean;
  checked?: boolean;
  children?: MenuItem[];
  separatorBefore?: boolean;
  onSelect?: () => void;
}

interface MenuProps {
  items: MenuItem[];
  /** Screen position of the click, or the rectangle of the button that opened it. */
  at: { x: number; y: number } | DOMRect;
  onClose: () => void;
  title?: string;
}

/**
 * A menu for the context click, the Actions button and (as a sheet from the
 * bottom edge) touch screens. Submenus replace the list in place. Position is set through the CSSOM.
 */
export function Menu({ items, at, onClose, title }: MenuProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [path, setPath] = useState<MenuItem[]>([]);
  const current = path.length ? path[path.length - 1]!.children! : items;

  useLayoutEffect(() => {
    const el = ref.current!;
    const pad = 6;
    const r = el.getBoundingClientRect();
    const isRect = "width" in at;
    let x = isRect ? at.left : at.x;
    let y = isRect ? at.bottom + 2 : at.y;
    if (x + r.width > innerWidth - pad) x = Math.max(pad, (isRect ? at.right : x) - r.width);
    if (y + r.height > innerHeight - pad) y = Math.max(pad, (isRect ? at.top - 2 : y) - r.height);
    el.style.setProperty("left", `${Math.max(pad, x)}px`);
    el.style.setProperty("top", `${Math.max(pad, y)}px`);
  }, [at, path]);

  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    const down = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    const blur = () => onClose();
    document.addEventListener("mousedown", down, true);
    addEventListener("blur", blur);
    addEventListener("resize", blur);
    return () => {
      document.removeEventListener("mousedown", down, true);
      removeEventListener("blur", blur);
      removeEventListener("resize", blur);
      if (prev && document.contains(prev)) prev.focus();
    };
  }, []);

  useEffect(() => {
    ref.current?.querySelector<HTMLElement>('[role^="menuitem"]:not([aria-disabled="true"])')?.focus();
  }, [path]);

  const onKey = (e: KeyboardEvent) => {
    const els = [...ref.current!.querySelectorAll<HTMLElement>('[role^="menuitem"]')];
    const i = els.indexOf(document.activeElement as HTMLElement);
    const go = (n: number) => {
      for (let k = 1; k <= els.length; k++) {
        const el = els[(i + n * k + els.length * 2) % els.length]!;
        if (el.getAttribute("aria-disabled") !== "true") {
          el.focus();
          return;
        }
      }
    };
    switch (e.key) {
      case "ArrowDown": e.preventDefault(); go(1); break;
      case "ArrowUp": e.preventDefault(); go(-1); break;
      case "Home": e.preventDefault(); els[0]?.focus(); break;
      case "End": e.preventDefault(); els[els.length - 1]?.focus(); break;
      case "ArrowLeft":
        if (path.length) { e.preventDefault(); setPath(path.slice(0, -1)); }
        break;
      case "Escape":
        e.preventDefault();
        e.stopPropagation();
        if (path.length) setPath(path.slice(0, -1));
        else onClose();
        break;
      case "Tab": e.preventDefault(); go(e.shiftKey ? -1 : 1); break;
    }
  };

  const choose = (it: MenuItem) => {
    if (it.disabled) return;
    if (it.children) {
      setPath([...path, it]);
      return;
    }
    onClose();
    it.onSelect?.();
  };

  return (
    <div class="menu-layer">
      <div class="menu-scrim" onMouseDown={onClose} />
      <div ref={ref} class="menu" role="menu" aria-label={title} onKeyDown={onKey}>
        {path.length ? (
          <button type="button" role="menuitem" class="menu-item back" onClick={() => setPath(path.slice(0, -1))}>
            <Icon name="back" size={14} />
            <span>{path[path.length - 1]!.label}</span>
          </button>
        ) : null}
        {current.map((it) => (
          <div key={it.id}>
            {it.separatorBefore ? <hr class="menu-sep" /> : null}
            <button
              type="button"
              role={it.checked !== undefined ? "menuitemcheckbox" : "menuitem"}
              aria-checked={it.checked}
              aria-disabled={it.disabled ? "true" : undefined}
              aria-haspopup={it.children ? "menu" : undefined}
              class={`menu-item${it.danger ? " danger" : ""}`}
              onClick={() => choose(it)}
            >
              <span class="menu-check">{it.checked ? <Icon name="check" size={13} /> : null}</span>
              <span class="menu-label" dir="auto">{it.label}</span>
              {it.hint ? <kbd>{it.hint}</kbd> : null}
              {it.children ? <Icon name="right" size={13} /> : null}
            </button>
          </div>
        ))}
        {current.length === 0 ? <div class="menu-empty">{t("menu.empty")}</div> : null}
      </div>
    </div>
  );
}
