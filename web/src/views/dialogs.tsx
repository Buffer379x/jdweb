import { useMemo, useRef, useState } from "preact/hooks";
import type { CleanupAction, CleanupMode, CleanupSelection, LinkVariant } from "../api/types.ts";
import { ConfirmDialog, Dialog } from "../components/Dialog.tsx";
import { cleanupCount } from "../lib/cleanup.ts";
import { plural, t, type Key } from "../i18n/index.ts";
import { pushToast } from "../store/app.ts";
import { openDialog } from "../store/dialogs.ts";
import { describe, targetOf, type AnyStore } from "../store/target.ts";
import { cleanupBody, runAction } from "./actions.tsx";

export function UrlsDialog({ urls, onClose }: { urls: string[]; onClose: () => void }) {
  const ta = useRef<HTMLTextAreaElement>(null);
  const copy = async () => {
    const text = urls.join("\n");
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // Plain http without clipboard access: select the text so that Ctrl+C works.
      ta.current?.select();
      try {
        document.execCommand("copy");
      } catch {
        return;
      }
    }
    pushToast("info", t("toast.copied"));
  };
  return (
    <Dialog
      title={t("act.urls")}
      onClose={onClose}
      wide
      footer={
        <>
          <button type="button" class="btn" onClick={onClose}>{t("common.close")}</button>
          <button type="button" class="btn pri" data-autofocus onClick={copy}>{t("common.copy")}</button>
        </>
      }
    >
      <p class="confirm-more">{plural("count.addresses", urls.length)}</p>
      <textarea ref={ta} class="input mono" rows={Math.min(14, Math.max(4, urls.length))} readOnly value={urls.join("\n")} dir="ltr" wrap="off" />
    </Dialog>
  );
}

export function VariantsDialog({ linkId, name, variants, onClose }: { linkId: string; name: string; variants: LinkVariant[]; onClose: () => void }) {
  const [sel, setSel] = useState(variants[0]?.id ?? "");
  const [busy, setBusy] = useState(false);
  const act = async (kind: "set-variant" | "add-variant") => {
    setBusy(true);
    const r = await runAction("linkgrabber", kind, { linkId, variantId: sel });
    if (r) onClose();
    else setBusy(false);
  };
  return (
    <Dialog
      title={t("act.variant")}
      onClose={onClose}
      footer={
        <>
          <button type="button" class="btn" onClick={onClose}>{t("common.cancel")}</button>
          <button type="button" class="btn" disabled={!sel || busy} onClick={() => act("add-variant")}>{t("variant.addCopy")}</button>
          <button type="button" class="btn pri" disabled={!sel || busy} onClick={() => act("set-variant")}>{t("variant.use")}</button>
        </>
      }
    >
      <p class="confirm-more" dir="auto" title={name}>{name}</p>
      {variants.length === 0 ? <p>{t("variant.none")}</p> : null}
      <div class="choices" role="radiogroup" aria-label={t("act.variant")}>
        {variants.map((v, i) => (
          <label key={v.id} class="choice">
            <input type="radio" name="variant" checked={sel === v.id} data-autofocus={i === 0 ? "" : undefined} onChange={() => setSel(v.id)} />
            <span dir="auto">{v.name}</span>
          </label>
        ))}
      </div>
    </Dialog>
  );
}

const ACTIONS: Record<"downloads" | "linkgrabber", CleanupAction[]> = {
  downloads: ["DELETE_FINISHED", "DELETE_FAILED", "DELETE_DISABLED", "DELETE_DUPE", "DELETE_ALL"],
  linkgrabber: ["DELETE_OFFLINE", "DELETE_DISABLED", "DELETE_DUPE", "DELETE_ALL"],
};
const MODES: CleanupMode[] = ["REMOVE_LINKS_ONLY", "REMOVE_LINKS_AND_RECYCLE_FILES", "REMOVE_LINKS_AND_DELETE_FILES"];
const SELECTIONS: CleanupSelection[] = ["ALL", "SELECTED", "UNSELECTED"];

/** Cleanup: what to remove, for which entries, and what happens to files. Removing links only is the default. */
export function CleanupDialog({ store, onClose }: { store: AnyStore; onClose: () => void }) {
  const list = store.name;
  const tg = useMemo(() => targetOf(store), []);
  const hasSel = store.selection.peek().size > 0;
  const [action, setAction] = useState<CleanupAction>(ACTIONS[list][0]!);
  const [mode, setMode] = useState<CleanupMode>("REMOVE_LINKS_ONLY");
  const [selection, setSelection] = useState<CleanupSelection>(hasSel ? "SELECTED" : "ALL");
  const [busy, setBusy] = useState(false);

  const count = cleanupCount(store.model, action, selection, hasSel ? tg.keys : new Set());
  const filesAffected = mode !== "REMOVE_LINKS_ONLY";
  const run = async () => {
    setBusy(true);
    const r = await runAction(list, "cleanup", cleanupBody(hasSel ? tg : { ...tg, linkIds: [], packageIds: [] }, { action, mode, selection }), store);
    if (r) onClose();
    else setBusy(false);
  };
  const submit = () => {
    if (!filesAffected) {
      void run();
      return;
    }
    openDialog((close) => (
      <ConfirmDialog
        title={t("cleanup.title")}
        lines={[
          count === null ? t("cleanup.confirmUnknown") : t("cleanup.confirmCount", { what: plural("count.links", count) }),
          t(`cleanup.mode.${mode}.effect` as Key),
        ]}
        confirmLabel={t("cleanup.run")}
        danger
        onConfirm={run}
        onClose={close}
      />
    ));
  };
  const selectionNeedsIds = selection === "SELECTED" && !hasSel;
  return (
    <Dialog
      title={t("cleanup.title")}
      onClose={onClose}
      footer={
        <>
          <button type="button" class="btn" onClick={onClose}>{t("common.cancel")}</button>
          <button type="button" class={`btn ${filesAffected ? "danger" : "pri"}`} disabled={busy || count === 0 || selectionNeedsIds} onClick={submit}>
            {t("cleanup.run")}
          </button>
        </>
      }
    >
      <fieldset class="group">
        <legend>{t("cleanup.what")}</legend>
        {ACTIONS[list].map((a) => (
          <label key={a} class="choice">
            <input type="radio" name="cl-action" checked={action === a} data-autofocus={a === action ? "" : undefined} onChange={() => setAction(a)} />
            <span>{t(`cleanup.action.${a}` as Key)}</span>
          </label>
        ))}
      </fieldset>
      <fieldset class="group">
        <legend>{t("cleanup.which")}</legend>
        {SELECTIONS.map((s) => (
          <label key={s} class="choice">
            <input type="radio" name="cl-sel" checked={selection === s} disabled={s !== "ALL" && !hasSel} onChange={() => setSelection(s)} />
            <span>{t(`cleanup.sel.${s}` as Key)}</span>
          </label>
        ))}
      </fieldset>
      <fieldset class="group">
        <legend>{t("cleanup.files")}</legend>
        {MODES.map((m) => (
          <label key={m} class={`choice${m === "REMOVE_LINKS_AND_DELETE_FILES" ? " danger" : ""}`}>
            <input type="radio" name="cl-mode" checked={mode === m} onChange={() => setMode(m)} />
            <span>{t(`cleanup.mode.${m}` as Key)}</span>
          </label>
        ))}
      </fieldset>
      <p class={`cleanup-summary${filesAffected ? " warn" : ""}`} aria-live="polite">
        {count === null ? t("cleanup.summaryUnknown") : t("cleanup.summary", { what: plural("count.links", count) })} {t(`cleanup.mode.${mode}.effect` as Key)}
      </p>
      {hasSel ? <p class="confirm-more">{t("cleanup.selectionNote", { what: describe(tg) })}</p> : null}
    </Dialog>
  );
}

export function openCleanup(store: AnyStore): void {
  openDialog((close) => <CleanupDialog store={store} onClose={close} />);
}

