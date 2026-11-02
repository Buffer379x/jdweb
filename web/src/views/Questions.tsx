// Questions of JDownloader (file exists, confirmations, ...): shown generically from the properties of the dialog.

import { useCallback, useEffect, useState } from "preact/hooks";
import { get, isApiError, post } from "../api/client.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Lamp } from "../components/Lamp.tsx";
import { answerFields, buildAnswer, dialogKind, hasCloseReason, splitProperties } from "../lib/questions.ts";
import { plural, t } from "../i18n/index.ts";
import { pushToastForError, questionDialogOpen, status } from "../store/app.ts";
import { errorText } from "../store/errors.ts";

interface QuestionInfo {
  id: string;
  type: string;
  properties: Record<string, string>;
  answer?: Record<string, string>;
}

export function QuestionsDialog() {
  const onClose = () => (questionDialogOpen.value = false);
  const [list, setList] = useState<QuestionInfo[] | null>(null);
  const [failed, setFailed] = useState<unknown>(null);
  const [selId, setSelId] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const count = status.value?.dialogs ?? 0;

  const load = useCallback(async (): Promise<QuestionInfo[]> => {
    try {
      const l = await get<QuestionInfo[]>("/dialogs");
      const arr = Array.isArray(l) ? l : [];
      setList(arr);
      setFailed(null);
      return arr;
    } catch (e) {
      setFailed(e);
      return [];
    }
  }, []);
  useEffect(() => { void load(); }, [load, count]);

  useEffect(() => {
    if (!list) return;
    if (list.length === 0) {
      const id = setTimeout(onClose, 600);
      return () => clearTimeout(id);
    }
    // JDownloader asks in the order of the list: the first one is the one that can be answered.
    if (!selId || !list.some((q) => q.id === selId)) setSelId(list[0]!.id);
  }, [list, selId]);

  const sel = list?.find((q) => q.id === selId) ?? null;
  const first = list?.[0]?.id;

  return (
    <Dialog title={t("dlg.title")} onClose={onClose} wide sticky>
      <p class="visually-hidden" role="status" aria-live="polite">{plural("dlg.waiting", count)}</p>
      {failed ? (
        <div class="state-box" role="alert">
          <p>{errorText(failed)}</p>
          <button type="button" class="btn" onClick={() => void load()}>{t("common.retry")}</button>
        </div>
      ) : list === null ? (
        <div class="state-box" role="status">{t("boot.loading")}</div>
      ) : list.length === 0 ? (
        <div class="state-box" role="status"><p>{t("dlg.none")}</p></div>
      ) : (
        <div class={`cap-split${list.length > 1 ? " multi" : ""}`}>
          {list.length > 1 ? (
            <ul class="cap-list" aria-label={t("dlg.list")}>
              {list.map((q, i) => {
                const p = splitProperties(q.properties ?? {});
                return (
                  <li key={q.id}>
                    <button type="button" class={`cap-item${q.id === selId ? " on" : ""}`} aria-pressed={q.id === selId} onClick={() => setSelId(q.id)}>
                      <Lamp tone={i === 0 ? "accent" : "muted"} />
                      <span class="cap-item-host" dir="auto">{p.title || dialogKind(q.type)}</span>
                      {i === 0 ? <span class="small muted">{t("dlg.next")}</span> : null}
                    </button>
                  </li>
                );
              })}
            </ul>
          ) : null}
          {sel ? (
            <QuestionView
              key={sel.id}
              item={sel}
              canAnswer={sel.id === first}
              notice={notice}
              onAnswered={() => { setNotice(""); void load(); }}
              onConflict={async () => {
                const l = await load();
                if (l[0]) setSelId(l[0].id);
                setNotice(t("dlg.conflict"));
              }}
              onGone={() => { setNotice(t("dlg.gone")); void load(); }}
            />
          ) : null}
        </div>
      )}
    </Dialog>
  );
}

function QuestionView({ item, canAnswer, notice, onAnswered, onConflict, onGone }: {
  item: QuestionInfo; canAnswer: boolean; notice: string; onAnswered: () => void; onConflict: () => void; onGone: () => void;
}) {
  const [info, setInfo] = useState<QuestionInfo>(item);
  const [values, setValues] = useState<Record<string, string | boolean>>({});
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let off = false;
    get<QuestionInfo>(`/dialogs/${item.id}`).then((d) => !off && setInfo(d)).catch((e) => {
      if (!off && isApiError(e) && (e.code === "jd_error" || e.code === "not_found")) onGone();
    });
    return () => { off = true; };
  }, [item.id]);

  const p = splitProperties(info.properties ?? {});
  const fields = answerFields(info.answer);
  const withReason = hasCloseReason(info.answer);

  const answer = async (reason: string) => {
    setBusy(true);
    try {
      await post(`/dialogs/${item.id}/answer`, { data: buildAnswer(reason, fields, values, info.answer) });
      onAnswered();
    } catch (e) {
      if (isApiError(e) && e.code === "conflict") onConflict();
      else if (isApiError(e) && (e.code === "jd_error" || e.code === "not_found")) onGone();
      else pushToastForError(e);
      setBusy(false);
    }
  };

  return (
    <div class="cap-view">
      <p class="muted small mono" dir="ltr" title={info.type}>{dialogKind(info.type)}</p>
      {notice ? <p class="note" role="status">{notice}</p> : null}
      {!canAnswer ? <p class="note" role="status">{t("dlg.notFirst")}</p> : null}
      {p.title ? <h3 class="q-title" dir="auto">{p.title}</h3> : null}
      {p.message ? <p class="q-message" dir="auto">{p.message}</p> : null}
      {p.rest.length > 0 ? (
        <dl class="kvs q-props">
          {p.rest.map(([k, v]) => (
            <div class="kv" key={k}><dt>{k}</dt><dd class="mono" dir="auto">{v}</dd></div>
          ))}
        </dl>
      ) : null}
      {fields.length > 0 ? (
        <div class="q-fields">
          {fields.map((f) =>
            f.kind === "bool" ? (
              <label key={f.name} class="check"><input type="checkbox" checked={values[f.name] === true} onChange={(e) => setValues({ ...values, [f.name]: e.currentTarget.checked })} /><span>{f.name}</span></label>
            ) : (
              <label key={f.name} class="field">
                <span class="field-label">{f.name}</span>
                <input class="input" type="text" inputMode={f.kind === "number" ? "numeric" : undefined} value={String(values[f.name] ?? "")} autocomplete="off" dir="auto" onInput={(e) => setValues({ ...values, [f.name]: e.currentTarget.value })} />
              </label>
            ),
          )}
        </div>
      ) : null}
      <div class="btn-row">
        <button type="button" class="btn pri" data-autofocus disabled={busy} onClick={() => answer("OK")}>{withReason ? t("dlg.ok") : t("dlg.answer")}</button>
        {withReason ? <button type="button" class="btn" disabled={busy} onClick={() => answer("CANCEL")}>{t("dlg.cancel")}</button> : null}
      </div>
    </div>
  );
}
