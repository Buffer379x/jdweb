import { useEffect, useId, useRef, useState } from "preact/hooks";
import { get, post } from "../api/client.ts";
import { PRIORITIES, type FolderInfo } from "../api/types.ts";
import { dismissal } from "../lib/dismiss.ts";
import { ConfirmDialog, Dialog } from "../components/Dialog.tsx";
import { formatBytes } from "../lib/format.ts";
import { lang, plural, t, type Key } from "../i18n/index.ts";
import { pushToast, pushToastForError } from "../store/app.ts";
import { openDialog } from "../store/dialogs.ts";
import { stores } from "../store/stores.ts";

const MAX_TEXT_BYTES = 8 * 1024 * 1024;
const MAX_CONTAINER_BYTES = 40 * 1024 * 1024;
const CONTAINER_EXT = /\.(dlc|ccf|rsdf|metalink|meta4|nzb)$/i;

/** Folder and package name as a path, with the separator the folder already uses. */
export function joinPath(folder: string, name: string): string {
  const sep = folder.includes("\\") && !folder.includes("/") ? "\\" : "/";
  const base = folder.replace(/[\\/]+$/, "");
  return `${base}${sep}${name}`;
}

function readBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const fr = new FileReader();
    fr.onerror = () => reject(fr.error);
    fr.onload = () => {
      const url = String(fr.result);
      resolve(url.slice(url.indexOf(",") + 1));
    };
    fr.readAsDataURL(file);
  });
}

function countLinks(text: string): number {
  return text.split(/\r?\n/).filter((l) => l.trim() !== "").length;
}

export function AddLinksDialog({ onClose }: { onClose: () => void }) {
  const [text, setText] = useState("");
  const [pkgName, setPkgName] = useState("");
  const [folder, setFolder] = useState("");
  const [sub, setSub] = useState(false);
  const [extractPw, setExtractPw] = useState("");
  const [dlPw, setDlPw] = useState("");
  const [autostart, setAutostart] = useState(false);
  const [autoExtract, setAutoExtract] = useState(true);
  const [deep, setDeep] = useState(false);
  const [overwrite, setOverwrite] = useState(false);
  const [priority, setPriority] = useState("DEFAULT");
  const [file, setFile] = useState<File | null>(null);
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [busy, setBusy] = useState(false);
  const [drag, setDrag] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const listId = useId();
  const formId = useId();

  useEffect(() => {
    get<{ folders: FolderInfo[] }>("/linkgrabber/folders")
      .then((r) => {
        setFolders(r.folders ?? []);
        setFolder((cur) => cur || r.folders?.[0]?.path || "");
      })
      .catch(() => {});
  }, []);

  const dirty = text.trim() !== "" || file !== null || pkgName.trim() !== "" || extractPw !== "" || dlPw !== "";
  // Input is not thrown away by a stray Escape: ask first.
  const tryClose = () => {
    const d = dismissal(dirty, busy);
    if (d === "ignore") return;
    if (d === "close") return onClose();
    openDialog((close) => (
      <ConfirmDialog
        title={t("add.discardTitle")}
        lines={[t("add.discardText")]}
        confirmLabel={t("add.discard")}
        danger
        onConfirm={onClose}
        onClose={close}
      />
    ));
  };

  const textBytes = new TextEncoder().encode(text).length;
  const tooBig = textBytes > MAX_TEXT_BYTES || (file !== null && file.size > MAX_CONTAINER_BYTES);
  const can = (text.trim() !== "" || file !== null) && !tooBig && !busy;
  const known = folders.find((f) => f.path === folder);

  const take = (f: File | undefined) => {
    if (!f) return;
    if (CONTAINER_EXT.test(f.name)) {
      setFile(f);
    } else if (f.size <= 2 * 1024 * 1024) {
      void f.text().then((s) => setText((cur) => (cur ? `${cur.replace(/\s+$/, "")}\n${s}` : s)));
    }
  };

  const submit = async (e?: Event) => {
    e?.preventDefault();
    if (!can) return;
    setBusy(true);
    try {
      if (text.trim() !== "") {
        const body: Record<string, unknown> = { links: text.split(/\r?\n/).map((l) => l.trim()).filter(Boolean) };
        if (pkgName.trim()) body.packageName = pkgName.trim();
        if (folder.trim()) body.destinationFolder = sub && pkgName.trim() ? joinPath(folder.trim(), pkgName.trim()) : folder.trim();
        if (extractPw) body.extractPassword = extractPw;
        if (dlPw) body.downloadPassword = dlPw;
        // Only what differs from the defaults is sent, the rest stays as JDownloader has it.
        if (autostart) body.autostart = true;
        if (!autoExtract) body.autoExtract = false;
        if (deep) body.deepDecrypt = true;
        if (overwrite) body.overwritePackagizerRules = true;
        if (priority !== "DEFAULT") body.priority = priority;
        const n = countLinks(text);
        await post("/linkgrabber/add", body);
        // The links are with JDownloader now. If the container fails below, a retry must not send them twice.
        setText("");
        pushToast("info", plural("toast.linksSent", n));
        stores.linkgrabber.reloadSoon(600);
      }
      if (file) {
        await post("/linkgrabber/container", { name: file.name, contentBase64: await readBase64(file) });
        setFile(null);
        pushToast("info", t("toast.containerSent"));
        stores.linkgrabber.reloadSoon(600);
      }
      onClose();
    } catch (err) {
      pushToastForError(err);
      setBusy(false);
    }
  };

  return (
    <Dialog
      title={t("add.title")}
      wide
      sticky
      onClose={onClose}
      onDismiss={tryClose}
      locked={busy}
      footer={
        <>
          <span class="foot-hint">{t("add.submitHint")}</span>
          <button type="button" class="btn" disabled={busy} onClick={tryClose}>{t("common.cancel")}</button>
          <button type="submit" form={formId} class="btn pri" disabled={!can}>{t("add.submit")}</button>
        </>
      }
    >
      <form id={formId} class="add-form" onSubmit={submit} onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && submit(e)}>
        <label class="field">
          <span class="field-label">{t("add.links")}</span>
          <textarea
            class={`input mono links-input${drag ? " drop" : ""}`}
            rows={6}
            value={text}
            data-autofocus
            placeholder={t("add.linksPlaceholder")}
            spellcheck={false}
            wrap="off"
            dir="ltr"
            onInput={(e) => setText(e.currentTarget.value)}
            onDragOver={(e) => {
              e.preventDefault();
              setDrag(true);
            }}
            onDragLeave={() => setDrag(false)}
            onDrop={(e) => {
              e.preventDefault();
              setDrag(false);
              take(e.dataTransfer?.files?.[0]);
            }}
          />
          <span class="field-hint">
            {text.trim() ? plural("add.lineCount", countLinks(text)) : t("add.dropHint")}
            {textBytes > MAX_TEXT_BYTES ? <b class="bad"> {t("add.tooBig")}</b> : null}
          </span>
        </label>

        <div class="field">
          <span class="field-label">{t("add.container")}</span>
          <div class="file-row">
            <input ref={fileInput} type="file" accept=".dlc,.ccf,.rsdf,.metalink,.meta4,.nzb,.txt,text/plain" class="visually-hidden" tabIndex={-1} aria-label={t("add.container")} onChange={(e) => take(e.currentTarget.files?.[0])} />
            <button type="button" class="btn" onClick={() => fileInput.current?.click()}>{t("add.chooseFile")}</button>
            {file ? (
              <span class="file-name" dir="auto" title={file.name}>
                {file.name} <span class="muted mono">{formatBytes(file.size, lang.value)}</span>
                <button type="button" class="link-btn" onClick={() => setFile(null)}>{t("add.removeFile")}</button>
              </span>
            ) : (
              <span class="muted">{t("add.containerHint")}</span>
            )}
            {file && file.size > MAX_CONTAINER_BYTES ? <b class="bad">{t("add.tooBig")}</b> : null}
          </div>
        </div>

        <div class="grid2">
          <label class="field">
            <span class="field-label">{t("field.packageName")}</span>
            <input class="input" value={pkgName} dir="auto" autocomplete="off" onInput={(e) => setPkgName(e.currentTarget.value)} placeholder={t("add.packageNameHint")} />
          </label>
          <label class="field">
            <span class="field-label">{t("add.priority")}</span>
            <select class="input" value={priority} onChange={(e) => setPriority(e.currentTarget.value)}>
              {PRIORITIES.map((p) => <option key={p} value={p}>{t(`prio.${p}` as Key)}</option>)}
            </select>
          </label>
        </div>

        <label class="field">
          <span class="field-label">{t("field.directory")}</span>
          <input class="input mono" list={listId} value={folder} spellcheck={false} autocomplete="off" dir="ltr" onInput={(e) => setFolder(e.currentTarget.value)} placeholder={t("add.folderHint")} />
          <datalist id={listId}>
            {folders.map((f) => <option key={f.path} value={f.path} />)}
          </datalist>
          <span class="field-hint">
            {known && known.free > 0 ? t("add.free", { free: formatBytes(known.free, lang.value), size: formatBytes(known.size, lang.value) }) : t("add.folderNote")}
          </span>
        </label>
        <label class="check">
          <input type="checkbox" checked={sub} onChange={(e) => setSub(e.currentTarget.checked)} />
          <span>{t("add.subfolder")}</span>
        </label>

        <div class="grid2">
          <label class="field">
            <span class="field-label">{t("add.extractPassword")}</span>
            <input class="input" value={extractPw} autocomplete="off" spellcheck={false} onInput={(e) => setExtractPw(e.currentTarget.value)} />
          </label>
          <label class="field">
            <span class="field-label">{t("field.downloadPassword")}</span>
            <input class="input" value={dlPw} autocomplete="off" spellcheck={false} onInput={(e) => setDlPw(e.currentTarget.value)} />
          </label>
        </div>

        <fieldset class="checks">
          <label class="check"><input type="checkbox" checked={autostart} onChange={(e) => setAutostart(e.currentTarget.checked)} /><span>{t("add.autostart")}</span></label>
          <label class="check"><input type="checkbox" checked={autoExtract} onChange={(e) => setAutoExtract(e.currentTarget.checked)} /><span>{t("add.autoExtract")}</span></label>
          <label class="check"><input type="checkbox" checked={deep} onChange={(e) => setDeep(e.currentTarget.checked)} /><span>{t("add.deepDecrypt")}</span></label>
          <label class="check"><input type="checkbox" checked={overwrite} onChange={(e) => setOverwrite(e.currentTarget.checked)} /><span>{t("add.overwriteRules")}</span></label>
        </fieldset>
      </form>
    </Dialog>
  );
}

export function openAddLinks(): void {
  openDialog((close) => <AddLinksDialog onClose={close} />);
}
