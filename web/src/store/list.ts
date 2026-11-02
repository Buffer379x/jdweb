// One list (downloads or linkgrabber) with its view state. The data lives in
// a normalized model; every entry has its own signal, so a patch re-renders
// only the rows whose entry changed.

import { computed, signal, type ReadonlySignal, type Signal } from "@preact/signals";
import { get } from "../api/client.ts";
import type { ListName, PackageLike, LinkLike, Patch } from "../api/types.ts";
import { applyPatch, buildModel, emptyModel, type Model } from "../lib/listmodel.ts";
import { buildRows, filterWords, type Row, type Sort } from "../lib/rows.ts";
import { pruneSelection } from "../lib/selection.ts";
import { prefs, updatePrefs } from "./app.ts";
import { lang } from "../i18n/index.ts";

export interface ListSpec<P, L> {
  name: ListName;
  textOfPackage: (p: P) => string;
  textOfLink: (l: L) => string;
  sortValuePackage: (p: P, col: string) => string | number;
  sortValueLink: (l: L, col: string) => string | number;
  /** Fields of a patch that can change what the filter or the sort sees. */
  viewFields: ReadonlySet<string>;
}

function shallowEqual(a: object, b: object): boolean {
  const ka = Object.keys(a);
  if (ka.length !== Object.keys(b).length) return false;
  for (const k of ka) {
    const x = (a as Record<string, unknown>)[k];
    const y = (b as Record<string, unknown>)[k];
    if (x !== y && !(Array.isArray(x) && Array.isArray(y) && x.length === y.length && x.every((v, i) => v === y[i]))) {
      if (typeof x === "object" && x && typeof y === "object" && y && shallowEqual(x, y)) continue;
      return false;
    }
  }
  return true;
}

const MAX_EXPANDED_PREF = 1500;

export class ListStore<P extends PackageLike, L extends LinkLike> {
  readonly name: ListName;
  readonly spec: ListSpec<P, L>;
  model: Model<P, L> = emptyModel();

  readonly loaded = signal(false);
  readonly loadFailed = signal(false);
  /** Bumped when the set or order of entries may have changed. */
  readonly structVer = signal(0);
  /** Bumped when a patch changed something filter or sort look at. */
  readonly viewVer = signal(0);

  readonly expanded = signal<ReadonlySet<string>>(new Set());
  readonly filter = signal("");
  readonly sort = signal<Sort | null>(null);
  readonly selection = signal<ReadonlySet<string>>(new Set());
  readonly focusKey = signal<string | null>(null);
  readonly anchorKey = signal<string | null>(null);

  readonly rows: ReadonlySignal<Row[]>;
  /** Packages and links shown, for the empty state. */
  readonly total: ReadonlySignal<number>;

  private pkgSig = new Map<string, Signal<P>>();
  private linkSig = new Map<string, Signal<L>>();
  private pkgVer = new Map<string, Signal<number>>();
  private generation = 0;
  private queue: Patch[] = [];
  private loading = false;
  private reloadTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(spec: ListSpec<P, L>) {
    this.name = spec.name;
    this.spec = spec;
    this.rows = computed(() => {
      this.structVer.value;
      this.viewVer.value;
      lang.value; // the filter looks at translated state names
      return buildRows(this.model, {
        expanded: this.expanded.value,
        words: filterWords(this.filter.value),
        textOfPackage: spec.textOfPackage,
        textOfLink: spec.textOfLink,
        sort: this.sort.value,
        sortValuePackage: spec.sortValuePackage,
        sortValueLink: spec.sortValueLink,
      });
    });
    this.total = computed(() => {
      this.structVer.value;
      return this.model.pkgs.size;
    });
  }

  pkg(id: string): Signal<P> | undefined {
    const m = this.model.pkgs.get(id);
    if (!m) return undefined;
    let s = this.pkgSig.get(id);
    if (!s) this.pkgSig.set(id, (s = signal(m)));
    return s;
  }
  link(id: string): Signal<L> | undefined {
    const m = this.model.links.get(id);
    if (!m) return undefined;
    let s = this.linkSig.get(id);
    if (!s) this.linkSig.set(id, (s = signal(m)));
    return s;
  }
  /** Changes whenever a link of the package changed, for the segment bar. */
  childrenVersion(pkgId: string): Signal<number> {
    let s = this.pkgVer.get(pkgId);
    if (!s) this.pkgVer.set(pkgId, (s = signal(0)));
    return s;
  }

  /** Loads the whole list. Patches that arrive meanwhile wait and are applied after. */
  async load(): Promise<void> {
    const gen = ++this.generation;
    this.loading = true;
    try {
      const data = await get<{ packages: P[]; links: L[] }>(`/${this.name}`);
      if (gen !== this.generation) return;
      this.replace(data.packages ?? [], data.links ?? []);
      this.loaded.value = true;
      this.loadFailed.value = false;
    } catch {
      if (gen === this.generation) this.loadFailed.value = true;
    } finally {
      if (gen === this.generation) {
        this.loading = false;
        const q = this.queue;
        this.queue = [];
        for (const p of q) this.patch(p);
      }
    }
  }

  private replace(packages: P[], links: L[]): void {
    const next = buildModel(packages, links);
    // Keep the old objects of unchanged entries, so that their rows stay as they are.
    for (const [id, p] of next.pkgs) {
      const old = this.model.pkgs.get(id);
      if (old && shallowEqual(old, p)) next.pkgs.set(id, old);
    }
    for (const [id, l] of next.links) {
      const old = this.model.links.get(id);
      if (old && shallowEqual(old, l)) next.links.set(id, old);
    }
    this.model = next;
    for (const [id, s] of this.pkgSig) {
      const m = next.pkgs.get(id);
      if (!m) this.pkgSig.delete(id);
      else if (s.peek() !== m) s.value = m;
    }
    for (const [id, s] of this.linkSig) {
      const m = next.links.get(id);
      if (!m) this.linkSig.delete(id);
      else if (s.peek() !== m) s.value = m;
    }
    for (const [id, s] of this.pkgVer) {
      if (!next.pkgs.has(id)) this.pkgVer.delete(id);
      else s.value++;
    }
    // Selection and expanded state only name entries that exist.
    const sel = pruneSelection(this.selection.peek(), next);
    if (sel !== this.selection.peek()) this.selection.value = sel;
    if (this.focusKey.peek() && !this.hasKey(this.focusKey.peek()!)) this.focusKey.value = null;
    if (this.anchorKey.peek() && !this.hasKey(this.anchorKey.peek()!)) this.anchorKey.value = null;
    this.pruneExpanded();
    this.structVer.value++;
  }

  hasKey(key: string): boolean {
    return key.charCodeAt(0) === 112 ? this.model.pkgs.has(key.slice(1)) : this.model.links.has(key.slice(1));
  }

  private pruneExpanded(): void {
    const ex = this.expanded.peek();
    let out: Set<string> | undefined;
    for (const id of ex) {
      if (!this.model.pkgs.has(id)) (out ??= new Set(ex)).delete(id);
    }
    if (out) this.expanded.value = out;
  }

  /** Applies a patch event, or holds it back while the list is loading or not loaded. */
  patch(p: Patch): boolean {
    if (this.loading) {
      this.queue.push(p);
      return true;
    }
    if (!this.loaded.peek()) return true;
    const res = applyPatch(this.model, p);
    for (const id of res.changedPackages) {
      const s = this.pkgSig.get(id);
      const m = this.model.pkgs.get(id);
      if (s && m) s.value = m;
    }
    for (const id of res.changedLinks) {
      const m = this.model.links.get(id);
      if (!m) continue;
      const s = this.linkSig.get(id);
      if (s) s.value = m;
      const v = this.pkgVer.get(m.packageId);
      if (v) v.value++;
    }
    if (res.fields.size && (this.filter.peek() || this.sort.peek())) {
      for (const f of res.fields) {
        if (this.spec.viewFields.has(f)) {
          this.viewVer.value++;
          break;
        }
      }
    }
    if (res.unknown) this.reloadSoon();
    return !res.unknown;
  }

  /** Loads the list again after a short pause; many structure events become one load. */
  reloadSoon(ms = 300): void {
    clearTimeout(this.reloadTimer);
    this.reloadTimer = setTimeout(() => void this.load(), ms);
  }

  // ---- view state -----------------------------------------------------------

  setExpanded(id: string, open: boolean): void {
    const ex = new Set(this.expanded.peek());
    if (open) ex.add(id);
    else ex.delete(id);
    this.expanded.value = ex;
    this.saveExpanded();
  }
  toggleExpanded(id: string): void {
    this.setExpanded(id, !this.expanded.peek().has(id));
  }
  expandAll(open: boolean): void {
    this.expanded.value = open ? new Set(this.model.order) : new Set();
    this.saveExpanded();
  }

  loadExpandedFromPrefs(): void {
    const ids = prefs.peek().expanded?.[this.name];
    if (Array.isArray(ids)) this.expanded.value = new Set(ids.filter((x) => typeof x === "string"));
  }
  private saveExpanded(): void {
    const ids = [...this.expanded.peek()].slice(-MAX_EXPANDED_PREF);
    updatePrefs({ expanded: { ...prefs.peek().expanded, [this.name]: ids } });
  }

  /** Drag and drop moves entries in the real order, so it is off when the view shows another one. */
  readonly reorderLocked = computed(() => this.filter.value.trim() !== "" || this.sort.value !== null);

  reset(): void {
    this.model = emptyModel();
    this.pkgSig.clear();
    this.linkSig.clear();
    this.pkgVer.clear();
    this.loaded.value = false;
    this.selection.value = new Set();
    this.focusKey.value = null;
    this.structVer.value++;
  }
}
