import { useRef, useState } from "preact/hooks";
import { signal } from "@preact/signals";
import type { Column } from "../components/VirtualTable.tsx";
import { VirtualTable } from "../components/VirtualTable.tsx";
import { Menu } from "../components/Menu.tsx";
import { planDrop, type DropPlan } from "../lib/move.ts";
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

// Drag state lives outside the render: it changes at 60 Hz. What is dragged is fixed when the drag starts.
let drag: { kind: "package" | "link"; ids: Set<string> } | null = null;

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
          // The matches are added to the selection; what the filter hides is not touched.
          store.selection.value = new Set([...store.selection.peek(), ...keysOf()]);
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
    const keys = [...store.selection.peek()];
    const pk = keys.filter(isPkgKey).length;
    if (pk !== 0 && pk !== keys.length) {
      // Packages and links together have no common place to go to.
      drag = null;
      return e.preventDefault();
    }
    drag = { kind: pk ? "package" : "link", ids: new Set(keys.map(idOfKey)) };
    e.dataTransfer?.setData("application/x-jdweb-rows", String(keys.length));
    if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
  };
  /** What a drop at the pointer would do. Only moves that change something are offered. */
  const dropAt = (e: DragEvent): { key: string; pos: "before" | "after" | "into"; plan: DropPlan } | null => {
    if (!drag) return null;
    const hit = rowOf(e.target);
    const row = hit && rows[hit.index];
    if (!row) return null;
    const el = (e.target as HTMLElement).closest<HTMLElement>("[data-key]")!;
    const r = el.getBoundingClientRect();
    const half: "before" | "after" = e.clientY < r.top + r.height / 2 ? "before" : "after";
    const pos = drag.kind === "link" && row.kind === "pkg" ? "into" : half;
    const plan = planDrop({
      dragged: drag.kind,
      ids: drag.ids,
      target: { kind: row.kind, id: row.id, packageId: row.packageId },
      pos,
      order: store.model.order,
      children: (id) => store.model.children.get(id) ?? [],
      targetExpanded: store.expanded.peek().has(row.id),
    });
    return plan.kind === "packages" || plan.kind === "links" ? { key: row.key, pos, plan } : null;
  };
  const onDragOver = (e: DragEvent) => {
    const d = dropAt(e);
    if (!d) {
      if (drop) setDrop(null);
      return; // no preventDefault: the pointer shows that this is no place to drop
    }
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    if (drop?.key !== d.key || drop.pos !== d.pos) setDrop({ key: d.key, pos: d.pos });
  };
  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    const d = dropAt(e);
    const ids = drag ? [...drag.ids] : [];
    drag = null;
    setDrop(null);
    if (!d) return;
    if (d.plan.kind === "packages") void runAction(store.name, "move-packages", { packageIds: ids, afterPackageId: d.plan.after }, store);
    else if (d.plan.kind === "links") void runAction(store.name, "move-links", { linkIds: ids, afterLinkId: d.plan.after, destPackageId: d.plan.destPackageId }, store);
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
      onDragEnd={() => { drag = null; setDrop(null); }}
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
              // Only the rows that are shown: what the filter hides keeps its state.
              const next = new Set(store.selection.peek());
              for (const k of keysOf()) allSelected ? next.delete(k) : next.add(k);
              store.selection.value = next;
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

