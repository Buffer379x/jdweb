import { useRef, useState } from "preact/hooks";
import { signal } from "@preact/signals";
import type { Column } from "../components/VirtualTable.tsx";
import { VirtualTable } from "../components/VirtualTable.tsx";
import { Menu } from "../components/Menu.tsx";
import { dropAfter } from "../lib/move.ts";
import { isPkgKey, idOfKey, rangeKeys, toggled } from "../lib/selection.ts";
import { t, type Key } from "../i18n/index.ts";
import { live, helpOpen } from "../store/app.ts";
import { dialogStack } from "../store/dialogs.ts";
import { ROW_H, ROW_H_COMPACT, compact, coarse } from "../store/viewport.ts";
import type { AnyStore } from "../store/target.ts";
import type { ListStore } from "../store/list.ts";
import type { DlLink, DlPackage, LgLink, LgPackage } from "../api/types.ts";
import { actionItems, removeSelected, renameSelected, runAction } from "./actions.tsx";
import { DlLinkRow, DlPackageRow, LgLinkRow, LgPackageRow } from "./Rows.tsx";
import { openAddLinks } from "./AddLinks.tsx";
import { Icon } from "../components/Icon.tsx";

const DL_COLUMNS: Column[] = [
  { key: "name", label: "col.name", sortable: true, area: "name" },
  { key: "size", label: "col.size", sortable: true, area: "size", align: "end" },
  { key: "host", label: "col.host", sortable: true, area: "host" },
  { key: "status", label: "col.status", sortable: true, area: "status" },
  { key: "progress", label: "col.progress", sortable: true, area: "progress" },
] as unknown as Column[];
const LG_COLUMNS: Column[] = [
  { key: "name", label: "col.name", sortable: true, area: "name" },
  { key: "path", label: "col.path", sortable: true, area: "path" },
  { key: "size", label: "col.size", sortable: true, area: "size", align: "end" },
  { key: "host", label: "col.host", sortable: true, area: "host" },
  { key: "status", label: "col.availability", sortable: true, area: "status" },
] as unknown as Column[];

export const menuAt = signal<{ at: { x: number; y: number } | DOMRect } | null>(null);

/** Opens the action menu of a list at a point or below a button. */
export function openActionMenu(at: { x: number; y: number } | DOMRect): void {
  menuAt.value = { at };
}

// Drag state lives outside the render: it changes at 60 Hz.
let dragKeys: string[] = [];

export function ListView({ store }: { store: AnyStore }) {
  const rows = store.rows.value;
  const sel = store.selection.value;
  const focusKey = store.focusKey.value;
  const expanded = store.expanded.value;
  const locked = store.reorderLocked.value;
  const isLive = live.value;
  const rowH = compact.value ? ROW_H_COMPACT : ROW_H;
  const [reveal, setReveal] = useState<{ index: number; n: number } | null>(null);
  const [drop, setDrop] = useState<{ key: string; pos: "before" | "after" | "into" } | null>(null);
  const revealSeq = useRef(0);
  const isDl = store.name === "downloads";

  const focusIndex = focusKey ? rows.findIndex((r) => r.key === focusKey) : -1;
  const keysOf = () => rows.map((r) => r.key);

  const setFocus = (idx: number, extend = false) => {
    if (idx < 0 || idx >= rows.length) return;
    const key = rows[idx]!.key;
    if (extend) {
      const anchor = store.anchorKey.peek() ?? focusKey ?? key;
      const ai = rows.findIndex((r) => r.key === anchor);
      store.selection.value = new Set(rangeKeys(keysOf(), ai < 0 ? idx : ai, idx));
      if (!store.anchorKey.peek()) store.anchorKey.value = anchor;
    }
    store.focusKey.value = key;
    setReveal({ index: idx, n: ++revealSeq.current });
  };

  const selectOnly = (key: string) => {
    store.selection.value = new Set([key]);
    store.anchorKey.value = key;
    store.focusKey.value = key;
  };

  const rowOf = (el: EventTarget | null): { key: string; index: number } | null => {
    const r = (el as HTMLElement | null)?.closest?.<HTMLElement>("[data-key]");
    if (!r) return null;
    const key = r.dataset.key!;
    return { key, index: rows.findIndex((x) => x.key === key) };
  };

  const onClick = (e: MouseEvent) => {
    const target = e.target as HTMLElement;
    const hit = rowOf(target);
    if (!hit) return;
    if (target.closest("[data-toggle]") && isPkgKey(hit.key)) {
      store.toggleExpanded(idOfKey(hit.key));
      store.focusKey.value = hit.key;
      return;
    }
    const additive = e.ctrlKey || e.metaKey || !!target.closest("[data-check]") || (coarse.value && store.selection.peek().size > 0);
    if (e.shiftKey) {
      setFocus(hit.index, true);
    } else if (additive) {
      store.selection.value = toggled(store.selection.peek(), hit.key);
      store.anchorKey.value = hit.key;
      store.focusKey.value = hit.key;
    } else {
      selectOnly(hit.key);
    }
  };

  const onDblClick = (e: MouseEvent) => {
    const hit = rowOf(e.target);
    if (hit && isPkgKey(hit.key)) store.toggleExpanded(idOfKey(hit.key));
  };

  const onContext = (e: MouseEvent) => {
    e.preventDefault();
    const hit = rowOf(e.target);
    if (hit && !store.selection.peek().has(hit.key)) selectOnly(hit.key);
    openActionMenu({ x: e.clientX, y: e.clientY });
  };

  const onKeyDown = (e: KeyboardEvent) => {
    if ((e.target as HTMLElement).closest("input,textarea,select")) return;
    const mod = e.ctrlKey || e.metaKey;
    const cur = focusIndex < 0 ? 0 : focusIndex;
    const page = Math.max(1, Math.floor((document.querySelector(".vt")?.clientHeight ?? 400) / rowH) - 1);
    switch (e.key) {
      case "ArrowDown": e.preventDefault(); setFocus(focusIndex < 0 ? 0 : Math.min(rows.length - 1, cur + 1), e.shiftKey); break;
      case "ArrowUp": e.preventDefault(); setFocus(Math.max(0, cur - 1), e.shiftKey); break;
      case "PageDown": e.preventDefault(); setFocus(Math.min(rows.length - 1, cur + page), e.shiftKey); break;
      case "PageUp": e.preventDefault(); setFocus(Math.max(0, cur - page), e.shiftKey); break;
      case "Home": e.preventDefault(); setFocus(0, e.shiftKey); break;
      case "End": e.preventDefault(); setFocus(rows.length - 1, e.shiftKey); break;
      case "ArrowRight": {
        const r = rows[focusIndex];
        if (!r) break;
        e.preventDefault();
        if (r.kind === "pkg") {
          if (!expanded.has(r.id)) store.setExpanded(r.id, true);
          else if (rows[focusIndex + 1]?.packageId === r.id && rows[focusIndex + 1]?.kind === "link") setFocus(focusIndex + 1);
        }
        break;
      }
      case "ArrowLeft": {
        const r = rows[focusIndex];
        if (!r) break;
        e.preventDefault();
        if (r.kind === "pkg") store.setExpanded(r.id, false);
        else setFocus(rows.findIndex((x) => x.key === `p${r.packageId}`));
        break;
      }
      case " ": {
        const r = rows[focusIndex];
        if (!r) break;
        e.preventDefault();
        store.selection.value = toggled(store.selection.peek(), r.key);
        store.anchorKey.value = r.key;
        break;
      }
      case "Enter": {
        const r = rows[focusIndex];
        if (r?.kind === "pkg") {
          e.preventDefault();
          store.toggleExpanded(r.id);
        }
        break;
      }
      case "Escape":
        if (store.selection.peek().size) store.selection.value = new Set();
        break;
      case "Delete":
        if (isLive) { e.preventDefault(); removeSelected(store); }
        break;
      case "F2":
        if (isLive) { e.preventDefault(); renameSelected(store); }
        break;
      case "a":
      case "A":
        if (mod) {
          e.preventDefault();
          store.selection.value = new Set(keysOf());
        }
        break;
      case "ContextMenu": {
        e.preventDefault();
        const el = document.getElementById(focusKey ? `r-${focusKey}` : "");
        const r = (el ?? document.querySelector(".vt"))!.getBoundingClientRect();
        if (focusKey && !store.selection.peek().size) store.selection.value = new Set([focusKey]);
        openActionMenu({ x: r.left + 40, y: r.top + r.height });
        break;
      }
    }
  };

  // ---- drag and drop (mouse; touch uses the Move menu) ---------------------------
  const canDrag = !locked && isLive && !coarse.value;
  const onDragStart = (e: DragEvent) => {
    const hit = rowOf(e.target);
    if (!hit || !canDrag) return e.preventDefault();
    if (!store.selection.peek().has(hit.key)) selectOnly(hit.key);
    dragKeys = [...store.selection.peek()];
    const pk = dragKeys.filter(isPkgKey).length;
    if (pk !== 0 && pk !== dragKeys.length) {
      dragKeys = [];
      return e.preventDefault();
    }
    e.dataTransfer?.setData("application/x-jdweb-rows", String(dragKeys.length));
    if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
  };
  const dropInfo = (e: DragEvent) => {
    if (!dragKeys.length) return null;
    const hit = rowOf(e.target);
    if (!hit) return null;
    const row = rows[hit.index];
    if (!row) return null;
    const el = (e.target as HTMLElement).closest<HTMLElement>("[data-key]")!;
    const r = el.getBoundingClientRect();
    const pos: "before" | "after" = e.clientY < r.top + r.height / 2 ? "before" : "after";
    const draggingPkgs = isPkgKey(dragKeys[0]!);
    if (draggingPkgs) return { row, pos, key: row.key };
    if (row.kind === "pkg") return { row, pos: "into" as const, key: row.key };
    return { row, pos, key: row.key };
  };
  const onDragOver = (e: DragEvent) => {
    const d = dropInfo(e);
    if (!d) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    if (drop?.key !== d.key || drop.pos !== d.pos) setDrop({ key: d.key, pos: d.pos });
  };
  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    const d = dropInfo(e);
    setDrop(null);
    if (!d) return;
    const ids = new Set(dragKeys.map(idOfKey));
    dragKeys = [];
    if (d.pos === "into") return void moveInto(d.row.id, ids);
    if (d.row.kind === "pkg" && !isPkgKey([...ids][0] ? `p${[...ids][0]}` : "") ) return;
    if (store.model.pkgs.has([...ids][0]!) && d.row.kind === "pkg") {
      const after = dropAfter(store.model.order, ids, d.row.id, d.pos as "before" | "after");
      if (after !== null) void runAction(store.name, "move-packages", { packageIds: [...ids], afterPackageId: after }, store);
    } else if (d.row.kind === "link") {
      const dest = d.row.packageId;
      const sibs = store.model.children.get(dest) ?? [];
      // Links of other packages are moved into this one: they are not in `sibs`, so "after" is computed on the rest.
      const after = dropAfter([...sibs, ...[...ids].filter((i) => !sibs.includes(i))], ids, d.row.id, d.pos as "before" | "after") ?? (ids.has(d.row.id) ? null : "-1");
      if (after !== null) void runAction(store.name, "move-links", { linkIds: [...ids], afterLinkId: after, destPackageId: dest }, store);
    }
  };
  const moveInto = (pkgId: string, ids: Set<string>) => {
    const kids = store.model.children.get(pkgId) ?? [];
    const after = store.expanded.peek().has(pkgId) ? "-1" : (kids.filter((k) => !ids.has(k)).pop() ?? "-1");
    void runAction(store.name, "move-links", { linkIds: [...ids], afterLinkId: after, destPackageId: pkgId }, store);
  };

  const columns = (isDl ? DL_COLUMNS : LG_COLUMNS).map((c) => ({ ...c, label: t(c.label as Key) }));
  const onSort = (col: string) => {
    const cur = store.sort.peek();
    store.sort.value = cur?.col !== col ? { col, dir: "asc" } : cur.dir === "asc" ? { col, dir: "desc" } : null;
  };

  const allSelected = rows.length > 0 && rows.every((r) => sel.has(r.key));
  const renderRows = (start: number, end: number) => {
    const out = [];
    for (let i = start; i < end; i++) {
      const r = rows[i]!;
      const frame = {
        rowKey: r.key,
        kind: r.kind,
        index: i,
        selected: sel.has(r.key),
        focused: r.key === focusKey,
        expanded: r.kind === "pkg" && (expanded.has(r.id) || (store.filter.value !== "" && rows[i + 1]?.packageId === r.id && rows[i + 1]?.kind === "link")),
        drop: drop?.key === r.key ? drop.pos : undefined,
        draggable: canDrag,
      };
      if (isDl) {
        const s = store as unknown as ListStore<DlPackage, DlLink>;
        out.push(r.kind === "pkg" ? <DlPackageRow key={r.key} store={s} id={r.id} frame={frame} /> : <DlLinkRow key={r.key} store={s} id={r.id} frame={frame} />);
      } else {
        const s = store as unknown as ListStore<LgPackage, LgLink>;
        out.push(r.kind === "pkg" ? <LgPackageRow key={r.key} store={s} id={r.id} frame={frame} /> : <LgLinkRow key={r.key} store={s} id={r.id} frame={frame} />);
      }
    }
    return out;
  };

  const empty = !store.loaded.value ? (
    <div class="empty" role="status">{store.loadFailed.value ? t("list.loadFailed") : t("list.loading")}</div>
  ) : store.total.value === 0 ? (
    <div class="empty">
      <p>{isDl ? t("empty.downloads") : t("empty.linkgrabber")}</p>
      <button type="button" class="btn pri" onClick={openAddLinks}>{t("add.open")} <kbd>A</kbd></button>
    </div>
  ) : store.filter.value ? (
    <div class="empty"><p>{t("empty.filter")}</p></div>
  ) : null;

  const menu = menuAt.value;
  return (
    <div
      class="list-wrap"
      onClick={onClick}
      onDblClick={onDblClick}
      onDragStart={onDragStart}
      onDragOver={onDragOver}
      onDragLeave={() => setDrop(null)}
      onDragEnd={() => { dragKeys = []; setDrop(null); }}
      onDrop={onDrop}
    >
      <VirtualTable
        label={isDl ? t("tab.downloads") : t("tab.linkgrabber")}
        columns={columns}
        rowCount={rows.length}
        rowHeight={rowH}
        sort={store.sort.value}
        onSort={onSort}
        focusIndex={focusIndex}
        focusId={focusIndex >= 0 ? `r-${focusKey}` : undefined}
        selectAll={
          <span
            class={`chk${allSelected ? " on" : ""}`}
            role="checkbox"
            aria-checked={allSelected}
            aria-label={t("list.selectAll")}
            tabIndex={-1}
            onClick={(e) => {
              e.stopPropagation();
              store.selection.value = allSelected ? new Set() : new Set(keysOf());
            }}
          >
            {allSelected ? <Icon name="check" size={12} /> : null}
          </span>
        }
        renderRows={renderRows}
        onKeyDown={onKeyDown}
        onContextMenu={onContext}
        listClass={`list-${store.name}`}
        empty={empty}
        revealRequest={reveal}
      />
      {menu && !dialogStack.value.length && !helpOpen.value ? (
        <Menu items={actionItems(store)} at={menu.at} title={t("menu.actions")} onClose={() => (menuAt.value = null)} />
      ) : null}
    </div>
  );
}

