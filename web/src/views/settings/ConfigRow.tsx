// One setting of JDownloader with an editor chosen from its type, a save button, a reset button and a visible state.
// Secrets show a mask; the real value only comes after a click on "Show" and is never kept in the list.

import { signal } from "@preact/signals";
import { useEffect } from "preact/hooks";
import { post, put } from "../../api/client.ts";
import { announce } from "../../store/announce.ts";
import { ConfirmDialog } from "../../components/Dialog.tsx";
import { Icon } from "../../components/Icon.tsx";
import {
  MASK, editorFor, holdsMask, parseText, sameValue, secretIsSet, shortInterface, valueToText,
  type ConfigEntry, type EditorKind, type ParseError,
} from "../../lib/editors.ts";
import { configPaths, revealText, type Api } from "../../lib/configapi.ts";
import { t, type Key } from "../../i18n/index.ts";
import { live, pushToastForError } from "../../store/app.ts";
import { openDialog } from "../../store/dialogs.ts";
import { errorDetail, errorText } from "../../store/errors.ts";

export type { Api };

export function entryId(e: ConfigEntry, api: Api): string {
  return `${api}|${e.storage}|${e.interface}|${e.key}`;
}

type Status = "idle" | "saving" | "saved" | "error";

/** What a row remembers while it is scrolled out of the page. */
export class RowState {
  /** The value JDownloader has now (masked for a secret). */
  value;
  /** The text being edited; null when nothing is being edited. */
  draft = signal<string | null>(null);
  status = signal<Status>("idle");
  error = signal("");
  /** The secret is shown in clear (after its reveal). */
  revealed = signal(false);
  /** The text a secret started with (what was revealed, or empty for a new value): equal to it is no change. */
  baseline = signal("");
  constructor(e: ConfigEntry) {
    this.value = signal<unknown>(e.value);
  }
}

export class RowStates {
  private map = new Map<string, RowState>();
  of(e: ConfigEntry, api: Api): RowState {
    const id = entryId(e, api);
    let s = this.map.get(id);
    if (!s) this.map.set(id, (s = new RowState(e)));
    return s;
  }
}

const PARSE_ERRORS: Record<ParseError, Key> = {
  int: "cfg.err.int", long: "cfg.err.long", double: "cfg.err.double", list: "cfg.err.list", listItems: "cfg.err.listItems",
  listInts: "cfg.err.listInts", enum: "cfg.err.enum", json: "cfg.err.json", empty: "cfg.err.empty",
};

interface RowProps {
  entry: ConfigEntry;
  api: Api;
  state: RowState;
  /** A friendly name instead of the key (the general page). */
  label?: string;
  /** Free height instead of the fixed row of the long list. */
  flex?: boolean;
  /** Shows the interface and plugin below the key. */
  showOrigin?: boolean;
  index?: number;
  count?: number;
}

export function ConfigRow({ entry: e, api, state: rs, label, flex, showOrigin = true, index, count }: RowProps) {
  const kind: EditorKind = editorFor(e);
  const cur = rs.value.value;
  const draft = rs.draft.value;
  const status = rs.status.value;
  const secret = e.secret;
  const isSet = secret ? secretIsSet(cur) : true;
  const editing = draft !== null;
  const text = editing ? draft : valueToText(kind, secret ? "" : cur);
  const parsed = editing ? parseText(kind, text, e) : null;
  const invalid = parsed !== null && !parsed.ok;
  const unchanged = parsed !== null && parsed.ok && (secret ? text === rs.baseline.value : sameValue(parsed.value, cur));
  const dirty = editing && !invalid && !unchanged;
  const atDefault = !secret && sameValue(cur, e.defaultValue);
  const busy = status === "saving";
  const id = `cfg-${entryId(e, api).replace(/[^\w]/g, "_")}`;
  const errId = `${id}-err`;
  const on = live.value;

  // A saved/unsaved word fades after a while so the list stays calm.
  useEffect(() => {
    if (status !== "saved") return;
    const timer = setTimeout(() => (rs.status.value = "idle"), 4000);
    return () => clearTimeout(timer);
  }, [status]);

  const edit = (v: string) => {
    rs.draft.value = v;
    if (rs.status.value !== "saving") {
      rs.status.value = "idle";
      rs.error.value = "";
    }
  };

  const save = async () => {
    if (!parsed || !parsed.ok || busy) return;
    if (holdsMask(parsed.value)) {
      rs.status.value = "error";
      rs.error.value = t("cfg.err.mask");
      return;
    }
    rs.status.value = "saving";
    try {
      await put(configPaths(api).set, { interface: e.interface, storage: e.storage, key: e.key, value: parsed.value });
      rs.value.value = secret ? (Array.isArray(parsed.value) ? (parsed.value.length ? [MASK] : []) : parsed.value === "" ? "" : MASK) : parsed.value;
      rs.draft.value = null;
      rs.revealed.value = false;
      rs.baseline.value = "";
      rs.status.value = "saved";
      announce(`${label ?? e.key}: ${t("cfg.saved")}`);
    } catch (err) {
      rs.status.value = "error";
      rs.error.value = [errorText(err), errorDetail(err)].filter(Boolean).join(" ");
      pushToastForError(err);
    }
  };

  const reveal = async () => {
    rs.status.value = "saving";
    try {
      const shownText = await revealText(api, kind, e);
      rs.baseline.value = shownText;
      rs.draft.value = shownText;
      rs.revealed.value = true;
      rs.status.value = "idle";
    } catch (err) {
      rs.status.value = "idle";
      pushToastForError(err);
    }
  };

  const hide = () => {
    rs.draft.value = null;
    rs.revealed.value = false;
    rs.baseline.value = ""; // the revealed value is forgotten
    rs.status.value = "idle";
    rs.error.value = "";
  };

  const reset = () =>
    openDialog((close) => (
      <ConfirmDialog
        title={t("cfg.reset")}
        lines={[
          t("cfg.resetWhat", { key: label ?? e.key }),
          secret ? t("cfg.resetSecret") : t("cfg.resetTo", { value: shown(e.defaultValue, kind) }),
        ]}
        confirmLabel={t("cfg.reset")}
        danger
        onConfirm={async () => {
          await post(configPaths(api).reset, { interface: e.interface, storage: e.storage, key: e.key });
          rs.value.value = secret ? (kind === "list" ? [] : "") : e.defaultValue;
          rs.draft.value = null;
          rs.revealed.value = false;
          rs.baseline.value = "";
          rs.status.value = "saved";
          announce(`${label ?? e.key}: ${t("cfg.saved")}`);
        }}
        onClose={close}
      />
    ));

  const aria = { id, "aria-invalid": invalid ? ("true" as const) : undefined, "aria-describedby": invalid || rs.error.value ? errId : undefined };
  let editor;
  if (secret && !editing) {
    editor = (
      <div class="cfg-secret">
        <input class="input mono" type="text" readOnly value={isSet ? MASK : ""} placeholder={t("cfg.notSet")} aria-label={`${label ?? e.key}: ${isSet ? t("cfg.secretSet") : t("cfg.notSet")}`} />
        {isSet ? <button type="button" class="btn small" disabled={!on || busy} onClick={reveal}>{t("cfg.show")}</button> : null}
        <button type="button" class="btn small" disabled={!on} onClick={() => { const start = kind === "list" ? "[]" : ""; rs.baseline.value = start; edit(start); }}>{isSet ? t("cfg.change") : t("cfg.set")}</button>
      </div>
    );
  } else if (kind === "bool") {
    editor = (
      <label class="cfg-bool">
        <input {...aria} type="checkbox" role="switch" checked={editing ? text === "true" : !!cur} disabled={!on || busy} onChange={(ev) => edit(ev.currentTarget.checked ? "true" : "false")} />
        <span>{(editing ? text === "true" : !!cur) ? t("cfg.on") : t("cfg.off")}</span>
      </label>
    );
  } else if (kind === "enum") {
    const options = e.enumOptions ?? [];
    const known = options.some((o) => o[0] === text);
    editor = (
      <select {...aria} class="input" value={text} disabled={!on || busy} onChange={(ev) => edit(ev.currentTarget.value)}>
        {!known ? <option value={text}>{text || t("cfg.notSet")}</option> : null}
        {options.map((o) => <option key={o[0]} value={o[0]}>{o[1] ? `${o[1]} (${o[0]})` : o[0]}</option>)}
      </select>
    );
  } else if (kind === "list" || kind === "json") {
    editor = (
      <textarea {...aria} class="input mono cfg-area" rows={2} spellcheck={false} dir="ltr" wrap="off" value={text} disabled={!on || busy} onInput={(ev) => edit(ev.currentTarget.value)} />
    );
  } else {
    const numeric = kind === "int" || kind === "long" || kind === "double";
    editor = (
      <input {...aria} class={`input${numeric || kind === "string" ? " mono" : ""}`} type={secret && !rs.revealed.value ? "password" : "text"} inputMode={numeric ? (kind === "double" ? "decimal" : "numeric") : undefined}
        autocomplete="off" spellcheck={false} dir={numeric ? "ltr" : "auto"} value={text} disabled={!on || busy} onInput={(ev) => edit(ev.currentTarget.value)} />
    );
  }

  let stateText = "";
  let stateTone = "";
  if (status === "saving") { stateText = t("cfg.saving"); stateTone = "info"; }
  else if (status === "error") { stateText = rs.error.value; stateTone = "err"; }
  else if (invalid && parsed && !parsed.ok) { stateText = t(PARSE_ERRORS[parsed.error]); stateTone = "err"; }
  else if (dirty) { stateText = t("cfg.unsaved"); stateTone = "accent"; }
  else if (status === "saved") { stateText = t("cfg.saved"); stateTone = "run"; }

  // A mistake is told to a screen reader when it appears (or its text changes), not at every key.
  const errorText_ = stateTone === "err" ? stateText : "";
  useEffect(() => {
    if (errorText_) announce(`${label ?? e.key}: ${errorText_}`);
  }, [errorText_]);

  return (
    <div
      class={`cfg-row${flex ? " flex" : ""}${dirty ? " dirty" : ""}`}
      role="listitem"
      aria-posinset={index !== undefined ? index + 1 : undefined}
      aria-setsize={count}
    >
      <div class="cfg-info">
        <label class="cfg-key" for={id} title={`${e.key}\n${e.interface}${e.storage ? `\n${e.storage}` : ""}`}>
          <span class={label ? "" : "mono"} dir="auto">{label ?? e.key}</span>
          {!secret && !atDefault ? <span class="cfg-tag" title={t("cfg.changedHint")}>{t("cfg.changed")}</span> : null}
        </label>
        {showOrigin && !label ? <span class="cfg-origin mono">{shortInterface(e.interface)}{e.storage && api === "plugins" ? ` · ${e.storage}` : ""}</span> : null}
        {e.docs ? <span class="cfg-docs" title={e.docs} dir="auto">{e.docs}</span> : null}
      </div>
      <div class="cfg-edit">{editor}</div>
      <div class="cfg-act">
        <span class={`cfg-state tone-text-${stateTone}`} id={errId} title={stateText || undefined}>{stateText}</span>
        <div class="cfg-btns">
          {dirty ? (
            <>
              <button type="button" class="btn small pri" disabled={busy || !on} onClick={save}>{t("cfg.save")}</button>
              <button type="button" class="btn small" disabled={busy} onClick={secret ? hide : () => { rs.draft.value = null; rs.status.value = "idle"; }}>{t("common.cancel")}</button>
            </>
          ) : secret && editing ? (
            <button type="button" class="btn small" disabled={busy} onClick={hide}>{rs.revealed.value ? t("cfg.hide") : t("common.cancel")}</button>
          ) : (
            <button type="button" class="btn small" disabled={!on || busy || (!secret && atDefault)} onClick={reset} aria-label={`${t("cfg.reset")}: ${label ?? e.key}`}>
              <Icon name="back" size={12} />{t("cfg.reset")}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function shown(v: unknown, kind: EditorKind): string {
  if (v === null || v === undefined || v === "") return t("cfg.empty");
  const s = kind === "list" || kind === "json" ? JSON.stringify(v) : String(v);
  return s.length > 120 ? `${s.slice(0, 120)}…` : s;
}
