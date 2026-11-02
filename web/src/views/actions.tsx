// The actions of both lists: one definition per list, used by the context
// menu, the Actions menu and the action bar on small screens (docs/api.md,
// table "Actions").

import { post, get, isApiError } from "../api/client.ts";
import { PRIORITIES, type CleanupAction, type CleanupMode, type CleanupSelection, type ListName, type LinkVariant } from "../api/types.ts";
import { ConfirmDialog, PromptDialog } from "../components/Dialog.tsx";
import type { MenuItem } from "../components/Menu.tsx";
import { moveTarget, type MoveDir } from "../lib/move.ts";
import { plural, t, type Key } from "../i18n/index.ts";
import { live, pushToast, pushToastForError } from "../store/app.ts";
import { openDialog } from "../store/dialogs.ts";
import { describe, isEmpty, targetOf, type AnyStore, type Target } from "../store/target.ts";
import { UrlsDialog, VariantsDialog } from "./dialogs.tsx";
import { extractNow, openArchiveSettings } from "./Extraction.tsx";

export interface Body {
  linkIds?: string[];
  packageIds?: string[];
  [k: string]: unknown;
}

/** Runs an action. A failure becomes a message, and the list is loaded again, so that it catches up with JDownloader. */
export async function runAction<T = void>(list: ListName, action: string, body: Body = {}, store?: AnyStore): Promise<{ data: T } | null> {
  try {
    return { data: await post<T>(`/${list}/actions/${action}`, { linkIds: [], packageIds: [], ...body }) };
  } catch (e) {
    pushToastForError(e);
    if (isApiError(e) && e.code !== "network") store?.reloadSoon(150);
    return null;
  }
}

const ids = (tg: Target): Body => ({ linkIds: tg.linkIds, packageIds: tg.packageIds });

function confirm(opts: { title: string; lines: string[]; confirmLabel: string; danger?: boolean; run: () => Promise<unknown> | void }): void {
  openDialog((close) => (
    <ConfirmDialog title={opts.title} lines={opts.lines} confirmLabel={opts.confirmLabel} danger={opts.danger} onConfirm={async () => void (await opts.run())} onClose={close} />
  ));
}

// ---- single actions ---------------------------------------------------------

export function removeSelected(store: AnyStore): void {
  const tg = targetOf(store);
  if (isEmpty(tg)) return;
  const downloads = store.name === "downloads";
  confirm({
    title: t("act.remove"),
    lines: [t("confirm.remove", { what: describe(tg) }), downloads ? t("confirm.removeKeepsFiles") : t("confirm.removeFromGrabber")],
    confirmLabel: t("act.remove"),
    danger: true,
    run: () => runAction(store.name, "remove", ids(tg), store),
  });
}

export function renameSelected(store: AnyStore): void {
  const tg = targetOf(store);
  if (tg.packageIds.length + tg.linkIds.length !== 1) return;
  const pkg = tg.pkgs[0];
  const current = pkg ? pkg.name : tg.links[0]!.name;
  openDialog((close) => (
    <PromptDialog
      title={t("act.rename")}
      fields={[{ name: "name", label: t("field.name"), value: current, required: true }]}
      submitLabel={t("act.rename")}
      onClose={close}
      onSubmit={async (v) => {
        const name = v.name!.trim();
        const body = pkg ? { packageId: pkg.id, name } : { linkId: tg.links[0]!.id, name };
        await post(`/${store.name}/actions/${pkg ? "rename-package" : "rename-link"}`, body);
      }}
    />
  ));
}

function promptSimple(store: AnyStore, action: "directory" | "password", title: string, field: string, label: string, initial: string, required: boolean, password: boolean) {
  const tg = targetOf(store);
  if (isEmpty(tg)) return;
  openDialog((close) => (
    <PromptDialog
      title={title}
      intro={describe(tg)}
      fields={[{ name: field, label, value: initial, required, mono: !password }]}
      submitLabel={t("common.apply")}
      onClose={close}
      onSubmit={async (v) => {
        await post(`/${store.name}/actions/${action}`, { ...ids(tg), [field]: v[field] ?? "" });
      }}
    />
  ));
}

export function changeDirectory(store: AnyStore): void {
  const tg = targetOf(store);
  const first = tg.pkgs[0] ?? store.model.pkgs.get(tg.links[0]?.packageId ?? "");
  promptSimple(store, "directory", t("act.directory"), "directory", t("field.directory"), first?.saveTo ?? "", true, false);
}

export function changePassword(store: AnyStore): void {
  const tg = targetOf(store);
  const first = tg.pkgs[0] ?? tg.links[0];
  promptSimple(store, "password", t("act.password"), "password", t("field.downloadPassword"), first?.downloadPassword ?? "", false, true);
}

export function newPackage(store: AnyStore): void {
  const tg = targetOf(store);
  if (isEmpty(tg)) return;
  const first = tg.pkgs[0] ?? store.model.pkgs.get(tg.links[0]?.packageId ?? "");
  openDialog((close) => (
    <PromptDialog
      title={t("act.newPackage")}
      intro={describe(tg)}
      fields={[
        { name: "name", label: t("field.packageName"), value: first?.name ?? "", required: true },
        { name: "directory", label: t("field.directory"), value: first?.saveTo ?? "", required: true, mono: true },
      ]}
      submitLabel={t("act.newPackage")}
      onClose={close}
      onSubmit={async (v) => {
        await post(`/${store.name}/actions/new-package`, { ...ids(tg), name: v.name!.trim(), directory: v.directory!.trim() });
      }}
    />
  ));
}

export async function showUrls(store: AnyStore): Promise<void> {
  const tg = targetOf(store);
  if (isEmpty(tg)) return;
  const res = await runAction<{ links: { id: string; url: string }[] }>(store.name, "urls", ids(tg), store);
  if (!res) return;
  openDialog((close) => <UrlsDialog urls={(res.data?.links ?? []).map((l) => l.url)} onClose={close} />);
}

export async function chooseVariant(store: AnyStore): Promise<void> {
  const tg = targetOf(store);
  const link = tg.links[0];
  if (!link) return;
  try {
    const res = await get<{ variants: LinkVariant[] }>(`/linkgrabber/variants/${link.id}`);
    openDialog((close) => <VariantsDialog linkId={link.id} name={link.name} variants={res.variants ?? []} current={(link as { variant?: LinkVariant | null }).variant?.id} onClose={close} />);
  } catch (e) {
    pushToastForError(e);
    store.reloadSoon(150);
  }
}

function setPriority(store: AnyStore, priority: string): void {
  const tg = targetOf(store);
  if (!isEmpty(tg)) void runAction(store.name, "priority", { ...ids(tg), priority }, store);
}

/** Moves the selection if it is one kind of entry in one place; returns false when it cannot be moved. */
export function moveSelected(store: AnyStore, dir: MoveDir): boolean {
  const tg = targetOf(store);
  if (store.reorderLocked.value || isEmpty(tg)) return false;
  if (tg.packageIds.length && tg.linkIds.length) return false;
  if (tg.packageIds.length) {
    const after = moveTarget(store.model.order, new Set(tg.packageIds), dir);
    if (after === null) return false;
    void runAction(store.name, "move-packages", { packageIds: tg.packageIds, afterPackageId: after }, store);
    return true;
  }
  const pkgIds = new Set(tg.links.map((l) => l.packageId));
  if (pkgIds.size !== 1) return false;
  const dest = [...pkgIds][0]!;
  const after = moveTarget(store.model.children.get(dest) ?? [], new Set(tg.linkIds), dir);
  if (after === null) return false;
  void runAction(store.name, "move-links", { linkIds: tg.linkIds, afterLinkId: after, destPackageId: dest }, store);
  return true;
}

function canMove(store: AnyStore, tg: Target): boolean {
  if (store.reorderLocked.value || isEmpty(tg)) return false;
  if (tg.packageIds.length && tg.linkIds.length) return false;
  if (tg.linkIds.length) return new Set(tg.links.map((l) => l.packageId)).size === 1;
  return true;
}

export function stopMark(store: AnyStore, remove: boolean): void {
  const tg = targetOf(store);
  if (remove) {
    void runAction(store.name, "stopmark-remove", {}, store);
    return;
  }
  const body = tg.pkgs[0] ? { packageId: tg.pkgs[0].id } : tg.links[0] ? { linkId: tg.links[0].id } : null;
  if (body) void runAction(store.name, "stopmark-set", body, store);
}

export function toDownloads(store: AnyStore, start: boolean): void {
  let tg = targetOf(store);
  if (isEmpty(tg) || store.selection.peek().size === 0) {
    // Nothing selected: all of the linkgrabber goes.
    tg = { ...tg, packageIds: [...store.model.order], linkIds: [] };
  }
  if (tg.packageIds.length === 0 && tg.linkIds.length === 0) return;
  void runAction(store.name, "to-downloads", { ...ids(tg), start }, store).then((r) => {
    if (r) pushToast("info", start ? t("toast.startedFromGrabber") : t("toast.movedToDownloads"));
  });
}

export function clearGrabber(store: AnyStore): void {
  const n = store.model.links.size;
  if (n === 0) return;
  confirm({
    title: t("act.clear"),
    lines: [t("confirm.clear", { what: plural("count.links", n) }), t("confirm.removeFromGrabber")],
    confirmLabel: t("act.clear"),
    danger: true,
    run: () => runAction(store.name, "clear", {}, store),
  });
}

export function resetSelected(store: AnyStore): void {
  const tg = targetOf(store);
  if (isEmpty(tg)) return;
  confirm({
    title: t("act.reset"),
    lines: [t("confirm.reset", { what: describe(tg) }), t("confirm.resetConsequence")],
    confirmLabel: t("act.reset"),
    danger: true,
    run: () => runAction(store.name, "reset", ids(tg), store),
  });
}

export interface CleanupRequest {
  action: CleanupAction;
  mode: CleanupMode;
  selection: CleanupSelection;
}

export function cleanupBody(tg: Target, r: CleanupRequest): Body {
  // Only ALL goes without ids (the server ignores them then); the other selections need at least one.
  const body: Body = r.selection === "ALL" ? { linkIds: [], packageIds: [] } : ids(tg);
  return { ...body, action: r.action, mode: r.mode, selection: r.selection };
}

// ---- the menu ------------------------------------------------------------------

type Def = {
  id: string;
  label: Key;
  hint?: string;
  danger?: boolean;
  separatorBefore?: boolean;
  show?: (tg: Target) => boolean;
  enabled: (tg: Target) => boolean;
  run?: (tg: Target) => void;
  children?: (store: AnyStore) => MenuItem[];
};

const any = (tg: Target) => !isEmpty(tg);
const one = (tg: Target) => tg.packageIds.length + tg.linkIds.length === 1;

const priorityItems = (store: AnyStore): MenuItem[] =>
  [...PRIORITIES].map((p) => ({ id: `prio-${p}`, label: t(`prio.${p}` as Key), onSelect: () => setPriority(store, p) }));

const moveItems = (store: AnyStore): MenuItem[] =>
  (["top", "up", "down", "bottom"] as const).map((d) => ({
    id: `move-${d}`,
    label: t(`move.${d}` as Key),
    disabled: !canMove(store, targetOf(store)),
    onSelect: () => void moveSelected(store, d),
  }));

const COMMON_TOP: Def[] = [];
const COMMON_MID: Def[] = [
  { id: "enable", label: "act.enable", enabled: any, run: (tg) => void runAction(tg.list, "enable", ids(tg), tg.store) },
  { id: "disable", label: "act.disable", enabled: any, run: (tg) => void runAction(tg.list, "disable", ids(tg), tg.store) },
  { id: "priority", label: "act.priority", enabled: any, children: priorityItems, separatorBefore: true },
  { id: "rename", label: "act.rename", hint: "F2", enabled: one, run: (tg) => renameSelected(tg.store) },
  { id: "directory", label: "act.directory", enabled: any, run: (tg) => changeDirectory(tg.store) },
  { id: "password", label: "act.password", enabled: any, run: (tg) => changePassword(tg.store) },
  { id: "newPackage", label: "act.newPackage", enabled: (tg) => tg.linkIds.length > 0 || tg.packageIds.length > 0, run: (tg) => newPackage(tg.store) },
  { id: "move", label: "act.move", enabled: (tg) => canMove(tg.store, tg), children: moveItems },
  { id: "split", label: "act.split", enabled: (tg) => tg.packageIds.length > 0, run: (tg) => void runAction(tg.list, "split-by-hoster", ids(tg), tg.store), separatorBefore: true },
  { id: "checkOnline", label: "act.checkOnline", enabled: any, run: (tg) => void runAction(tg.list, "check-online", ids(tg), tg.store) },
  { id: "urls", label: "act.urls", enabled: any, run: (tg) => void showUrls(tg.store) },
];
const REMOVE: Def = { id: "remove", label: "act.remove", hint: "Del", danger: true, enabled: any, run: (tg) => removeSelected(tg.store), separatorBefore: true };

const DOWNLOAD_ONLY_TOP: Def[] = [
  { id: "force", label: "act.force", enabled: any, run: (tg) => void runAction(tg.list, "force", ids(tg), tg.store) },
  { id: "resume", label: "act.resume", enabled: any, run: (tg) => void runAction(tg.list, "resume", ids(tg), tg.store) },
  { id: "unskip", label: "act.unskip", enabled: any, run: (tg) => void runAction(tg.list, "unskip", ids(tg), tg.store) },
  { id: "reset", label: "act.reset", enabled: any, run: (tg) => resetSelected(tg.store) },
  { id: "extractNow", label: "act.extractNow", enabled: any, run: (tg) => void extractNow(tg.store), separatorBefore: true },
  { id: "archiveSettings", label: "act.archiveSettings", enabled: any, run: (tg) => void openArchiveSettings(tg.store) },
];
const DOWNLOAD_STOPMARK: Def[] = [
  { id: "stopmarkSet", label: "act.stopmarkSet", enabled: one, run: (tg) => stopMark(tg.store, false), separatorBefore: true },
  { id: "stopmarkRemove", label: "act.stopmarkRemove", enabled: () => true, run: (tg) => stopMark(tg.store, true) },
];
const GRABBER_TOP: Def[] = [
  { id: "toDownloads", label: "act.toDownloads", enabled: () => true, run: (tg) => toDownloads(tg.store, false) },
  { id: "toDownloadsStart", label: "act.toDownloadsStart", enabled: () => true, run: (tg) => toDownloads(tg.store, true) },
];
const GRABBER_VARIANT: Def[] = [
  { id: "variant", label: "act.variant", enabled: (tg) => tg.links.length === 1 && tg.packageIds.length === 0 && !!(tg.links[0] as { variants?: boolean }).variants, run: (tg) => void chooseVariant(tg.store), separatorBefore: true },
];

const DEFS: Record<ListName, Def[]> = {
  downloads: [...COMMON_TOP, ...DOWNLOAD_ONLY_TOP, ...COMMON_MID, ...DOWNLOAD_STOPMARK, REMOVE],
  linkgrabber: [...GRABBER_TOP, ...COMMON_MID.map((d) => (d.id === "enable" ? { ...d, separatorBefore: true } : d)), ...GRABBER_VARIANT, REMOVE],
};

/** The items of the menu for what is selected right now. Everything is off while JDownloader is not reachable. */
export function actionItems(store: AnyStore): MenuItem[] {
  const tg = targetOf(store);
  const on = live.value;
  const empty = isEmpty(tg);
  return DEFS[store.name].map((d): MenuItem => {
    const needsTarget = d.id !== "toDownloads" && d.id !== "toDownloadsStart" && d.id !== "stopmarkRemove";
    const disabled = !on || (needsTarget && empty) || !d.enabled(tg);
    return {
      id: d.id,
      label: t(d.label),
      hint: d.hint,
      danger: d.danger,
      separatorBefore: d.separatorBefore,
      disabled,
      children: d.children ? d.children(store) : undefined,
      onSelect: d.run ? () => d.run!(targetOf(store)) : undefined,
    };
  });
}
