// Extraction: the panel in the footer with the queue, and the archive settings dialog.

import { useEffect, useRef, useState } from "preact/hooks";
import { isApiError, post, put } from "../api/client.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Lamp } from "../components/Lamp.tsx";
import { Segbar } from "../components/Segbar.tsx";
import type { Seg } from "../lib/segments.ts";
import { Icon } from "../components/Icon.tsx";
import { copyText } from "../components/clipboard.ts";
import { buildPatch, hasSecret, revealArchive, splitPasswords, type ArchiveEdit } from "../lib/archive.ts";
import { MASK } from "../lib/editors.ts";
import { canCancel, jobState, partCounts, partList, type ArchiveInfo, type ArchiveSettings, type QueueEntry } from "../lib/extraction.ts";
import { plural, t } from "../i18n/index.ts";
import { extractionOpen, live, pushToast, pushToastForError } from "../store/app.ts";
import { openDialog } from "../store/dialogs.ts";
import { errorText } from "../store/errors.ts";
import { queue, queueFailed, queueLoaded, refreshQueue } from "../store/extraction.ts";
import { targetOf, isEmpty, type AnyStore } from "../store/target.ts";

/** The button in the footer and, when open, the panel above it. */
export function ExtractionIndicator() {
  const q = queue.value;
  const open = extractionOpen.value;
  const btn = useRef<HTMLButtonElement>(null);
  const running = q.filter((e) => jobState(e.controllerStatus) === "running").length;
  const label = q.length === 0 ? t("ext.idle") : plural("ext.open", q.length);
  useEffect(() => {
    if (open) void refreshQueue();
  }, [open]);
  const close = () => {
    extractionOpen.value = false;
    btn.current?.focus();
  };
  return (
    <span class="extract-wrap">
      <button ref={btn} type="button" class="st extract-btn" aria-expanded={open} aria-haspopup="dialog" onClick={() => (extractionOpen.value = !open)}>
        <Lamp tone={running > 0 ? "run" : q.length > 0 ? "wait" : "muted"} />
        {label}
      </button>
      {open ? <ExtractionPanel onClose={close} /> : null}
    </span>
  );
}

function ExtractionPanel({ onClose }: { onClose: () => void }) {
  const q = queue.value;
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.focus();
    const down = (e: MouseEvent) => {
      const el = ref.current;
      if (el && !el.contains(e.target as Node) && !(e.target as HTMLElement).closest(".extract-btn")) onClose();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.querySelector(".scrim")) {
        e.preventDefault();
        onClose();
      }
    };
    document.addEventListener("mousedown", down, true);
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener("mousedown", down, true);
      document.removeEventListener("keydown", key);
    };
  }, []);
  const cancel = async (e: QueueEntry) => {
    try {
      await post("/extraction/cancel", { id: String(e.controllerId) });
    } catch (err) {
      // Finished meanwhile: nothing to cancel any more.
      if (!(isApiError(err) && err.code === "jd_error")) pushToastForError(err);
    }
    void refreshQueue();
  };
  return (
    <div
      ref={ref}
      class="popover extract-panel"
      role="dialog"
      aria-label={t("ext.panel")}
      tabIndex={-1}
    >
      <div class="popover-head">
        <h2>{t("ext.panel")}</h2>
        <button type="button" class="icon-btn" onClick={onClose} aria-label={t("common.close")}><Icon name="x" size={14} /></button>
      </div>
      {queueFailed.value && !queueLoaded.value ? (
        <div class="state-box" role="alert"><p>{t("ext.loadFailed")}</p><button type="button" class="btn small" onClick={() => void refreshQueue()}>{t("common.retry")}</button></div>
      ) : q.length === 0 ? (
        <p class="muted pad" role="status">{t("ext.empty")}</p>
      ) : (
        <ul class="jobs">
          {q.map((e) => {
            const js = jobState(e.controllerStatus);
            const pc = partCounts(e.states);
            return (
              <li key={e.archiveId + e.controllerId} class="job">
                <div class="job-head">
                  <span class="st"><Lamp tone={js === "running" ? "run" : "wait"} />
                    <b class="job-name" dir="auto" title={e.archiveName}>{e.archiveName}</b>
                  </span>
                  <span class="mono muted small">{e.type}</span>
                </div>
                <div class="job-body">
                  <span class="small">{js === "running" ? t("ext.running") : js === "queued" ? t("ext.queued") : t("ext.idleJob")}</span>
                  <PartsBar states={e.states} label={t("ext.parts", { done: pc.complete, total: pc.total })} />
                  <span class="mono small muted">{t("ext.parts", { done: pc.complete, total: pc.total })}{pc.missing ? `, ${t("ext.missing", { n: pc.missing })}` : ""}</span>
                  <button type="button" class="btn small" disabled={!live.value || !canCancel(e)} onClick={() => void cancel(e)} aria-label={`${t("ext.cancel")}: ${e.archiveName}`}>{t("ext.cancel")}</button>
                </div>
              </li>
            );
          })}
        </ul>
      )}
      {queueFailed.value && queueLoaded.value ? <p class="small bad pad" role="status">{t("ext.stale")}</p> : null}
    </div>
  );
}

/** One segment per part of the archive: complete parts are filled. */
function PartsBar({ states, label }: { states: Record<string, string> | null; label: string }) {
  const parts = partList(states);
  const segs: Seg[] = parts.slice(0, 24).map((p) => ({ kind: p.state === "MISSING" ? "err" : "done", fill: p.state === "COMPLETE" ? 1 : 0 }));
  return <Segbar segs={segs} label={label} />;
}

// ---- from the context menu of the downloads ------------------------------------------

export async function extractNow(store: AnyStore): Promise<void> {
  const tg = targetOf(store);
  if (isEmpty(tg)) return;
  try {
    const r = await post<{ started: Record<string, boolean> }>("/extraction/start", { linkIds: tg.linkIds, packageIds: tg.packageIds });
    const started = Object.values(r?.started ?? {}).filter(Boolean).length;
    pushToast("info", started > 0 ? plural("ext.started", started) : t("ext.nothingToStart"));
    void refreshQueue();
  } catch (e) {
    pushToastForError(e);
  }
}

export async function openArchiveSettings(store: AnyStore): Promise<void> {
  const tg = targetOf(store);
  if (isEmpty(tg)) return;
  try {
    const list = await post<ArchiveInfo[]>("/extraction/archives", { linkIds: tg.linkIds, packageIds: tg.packageIds });
    openDialog((close) => <ArchiveSettingsDialog archives={Array.isArray(list) ? list : []} onClose={close} />);
  } catch (e) {
    pushToastForError(e);
  }
}

function ArchiveSettingsDialog({ archives, onClose }: { archives: ArchiveInfo[]; onClose: () => void }) {
  const [idx, setIdx] = useState(0);
  const a = archives[idx];
  return (
    <Dialog title={t("arc.title")} onClose={onClose} wide>
      {archives.length === 0 || !a ? (
        <div class="state-box"><p>{t("arc.none")}</p><button type="button" class="btn" data-autofocus onClick={onClose}>{t("common.close")}</button></div>
      ) : (
        <>
          {archives.length > 1 ? (
            <label class="field">
              <span class="field-label">{t("arc.which")}</span>
              <select class="input" value={String(idx)} onChange={(e) => setIdx(Number(e.currentTarget.value))}>
                {archives.map((x, i) => <option key={x.archiveId} value={String(i)}>{x.archiveName}</option>)}
              </select>
            </label>
          ) : null}
          <ArchiveForm key={a.archiveId} archive={a} onClose={onClose} />
        </>
      )}
    </Dialog>
  );
}

function ArchiveForm({ archive, onClose }: { archive: ArchiveInfo; onClose: () => void }) {
  const s = archive.settings;
  const [path, setPath] = useState(s?.extractPath ?? "");
  const [auto, setAuto] = useState(s?.autoExtract ?? true);
  const [rmLinks, setRmLinks] = useState(s?.removeDownloadLinksAfterExtraction ?? false);
  const [rmFiles, setRmFiles] = useState(s?.removeFilesAfterExtraction ?? false);
  const [finalPw, setFinalPw] = useState<string | null>(null);
  const [pws, setPws] = useState<string | null>(null);
  const [revealed, setRevealed] = useState(false);
  // What the form is compared with: the masked settings, and after "Show" the real ones. Without that the
  // revealed values would differ from the mask and look like an edit.
  const [base, setBase] = useState<ArchiveSettings | null>(s ?? null);
  const [clearFinal, setClearFinal] = useState(false);
  const [clearList, setClearList] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [extra, setExtra] = useState("");
  if (!s) {
    return <div class="state-box"><p>{t("arc.noSettings")}</p><button type="button" class="btn" data-autofocus onClick={onClose}>{t("common.close")}</button></div>;
  }
  const id = encodeURIComponent(archive.archiveId);
  const edit: ArchiveEdit = {
    extractPath: path, autoExtract: auto, removeDownloadLinksAfterExtraction: rmLinks, removeFilesAfterExtraction: rmFiles, finalPassword: finalPw, passwords: pws,
    clearFinalPassword: clearFinal, clearPasswords: clearList,
  };
  const patch = buildPatch(base ?? s, edit);
  const dirty = Object.keys(patch).length > 0;

  const reveal = async () => {
    setBusy(true);
    try {
      const r = await revealArchive(archive.archiveId, s);
      setBase(r.base);
      // Typed text stays; an untouched field shows what is stored.
      if (!clearFinal && !finalPw) setFinalPw(r.finalPassword);
      if (!clearList && !pws) setPws(r.passwords);
      setRevealed(true);
    } catch (e) {
      pushToastForError(e);
    }
    setBusy(false);
  };
  const save = async () => {
    setBusy(true);
    setError("");
    try {
      await put(`/extraction/archives/${id}`, patch);
      pushToast("info", t("arc.saved"));
      onClose();
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };
  const addPassword = async () => {
    if (extra === "") return;
    try {
      await post("/extraction/passwords", { password: extra });
      setExtra("");
      pushToast("info", t("gen.archiveAdded"));
    } catch (e) {
      pushToastForError(e);
    }
  };
  const masked = !revealed;
  const pwSet = hasSecret((base ?? s).finalPassword);
  const listSet = hasSecret((base ?? s).passwords);
  return (
    <div class="arc-form">
      <p class="muted small mono" dir="auto">{archive.archiveName} · {archive.type}</p>
      <label class="field">
        <span class="field-label">{t("arc.extractPath")}</span>
        <input class="input mono" value={path} dir="ltr" autocomplete="off" spellcheck={false} data-autofocus="" onInput={(e) => setPath(e.currentTarget.value)} />
      </label>
      <div class="field">
        <label class="field-label" for="arc-final">{t("arc.finalPassword")}</label>
        {clearFinal ? (
          <div class="cfg-secret">
            <input id="arc-final" class="input mono" readOnly value="" placeholder={t("arc.willRemove")} />
            <button type="button" class="btn small" onClick={() => setClearFinal(false)}>{t("arc.undo")}</button>
          </div>
        ) : finalPw === null ? (
          <div class="cfg-secret">
            <input id="arc-final" class="input mono" readOnly value={pwSet ? MASK : ""} placeholder={t("cfg.notSet")} aria-label={`${t("arc.finalPassword")}: ${pwSet ? t("cfg.secretSet") : t("cfg.notSet")}`} />
            <button type="button" class="btn small" onClick={() => setFinalPw("")}>{pwSet ? t("cfg.change") : t("cfg.set")}</button>
            {pwSet ? <button type="button" class="btn small" onClick={() => setClearFinal(true)}>{t("arc.remove")}</button> : null}
          </div>
        ) : (
          <div class="cfg-secret">
            <input id="arc-final" class="input mono" type={revealed ? "text" : "password"} value={finalPw} autocomplete="new-password" onInput={(e) => setFinalPw(e.currentTarget.value)} />
            {pwSet ? <button type="button" class="btn small" onClick={() => { setClearFinal(true); setFinalPw(null); }}>{t("arc.remove")}</button> : null}
          </div>
        )}
        {finalPw === "" && pwSet && !clearFinal ? <span class="field-hint">{t("arc.emptyIgnored")}</span> : null}
      </div>
      <div class="field">
        <label class="field-label" for="arc-list">{t("arc.passwords")}</label>
        {clearList ? (
          <div class="cfg-secret">
            <input id="arc-list" class="input mono" readOnly value="" placeholder={t("arc.willRemoveList")} />
            <button type="button" class="btn small" onClick={() => setClearList(false)}>{t("arc.undo")}</button>
          </div>
        ) : pws === null ? (
          <div class="cfg-secret">
            <input id="arc-list" class="input mono" readOnly value={listSet ? MASK : ""} placeholder={t("cfg.notSet")} aria-label={`${t("arc.passwords")}: ${listSet ? t("cfg.secretSet") : t("cfg.notSet")}`} />
            <button type="button" class="btn small" onClick={() => setPws("")}>{listSet ? t("arc.replaceList") : t("cfg.set")}</button>
            {listSet ? <button type="button" class="btn small" onClick={() => setClearList(true)}>{t("arc.removeList")}</button> : null}
          </div>
        ) : (
          <>
            <textarea id="arc-list" class="input mono" rows={4} spellcheck={false} dir="ltr" value={pws} onInput={(e) => setPws(e.currentTarget.value)} />
            {listSet ? <div class="btn-row"><button type="button" class="btn small" onClick={() => { setClearList(true); setPws(null); }}>{t("arc.removeList")}</button></div> : null}
          </>
        )}
        <span class="field-hint">{clearList ? "" : pws !== null && !revealed ? t("arc.replaceHint") : pws === "" && listSet ? t("arc.emptyIgnored") : t("arc.passwordsHint")}</span>
      </div>
      {masked && (pwSet || listSet) ? <div class="btn-row"><button type="button" class="btn small" disabled={busy} onClick={reveal}>{t("cfg.show")}</button></div> : null}
      {revealed ? <div class="btn-row"><button type="button" class="btn small" onClick={async () => pushToast("info", (await copyText(splitPasswords(pws ?? "").join("\n"))) ? t("toast.copied") : t("toast.copyFailed"))}>{t("common.copy")}</button></div> : null}
      <fieldset class="group">
        <legend>{t("arc.options")}</legend>
        <label class="check"><input type="checkbox" checked={auto} onChange={(e) => setAuto(e.currentTarget.checked)} /><span>{t("arc.autoExtract")}</span></label>
        <label class="check"><input type="checkbox" checked={rmLinks} onChange={(e) => setRmLinks(e.currentTarget.checked)} /><span>{t("arc.removeLinks")}</span></label>
        <label class="check"><input type="checkbox" checked={rmFiles} onChange={(e) => setRmFiles(e.currentTarget.checked)} /><span>{t("arc.removeFiles")}</span></label>
        {rmFiles ? <p class="field-hint bad">{t("arc.removeFilesWarn")}</p> : null}
      </fieldset>
      <div class="inline-form">
        <label class="field grow">
          <span class="field-label">{t("gen.archivePassword")}</span>
          <input class="input mono" type="password" autocomplete="new-password" value={extra} onInput={(e) => setExtra(e.currentTarget.value)} />
        </label>
        <button type="button" class="btn" disabled={extra === ""} onClick={addPassword}>{t("gen.archiveAdd")}</button>
      </div>
      {error ? <p class="form-error" role="alert">{error}</p> : null}
      <div class="dialog-actions">
        <button type="button" class="btn" onClick={onClose}>{t("common.cancel")}</button>
        <button type="button" class="btn pri" disabled={!dirty || busy} onClick={save}>{t("cfg.save")}</button>
      </div>
    </div>
  );
}

