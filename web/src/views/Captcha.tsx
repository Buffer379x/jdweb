// Waiting captchas: image captchas are solved here, interactive ones are explained and can be skipped.

import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { get, post } from "../api/client.ts";
import { isGone } from "../lib/gone.ts";
import { ConfirmDialog, Dialog } from "../components/Dialog.tsx";
import { Lamp } from "../components/Lamp.tsx";
import { useNow } from "../hooks/useNow.ts";
import { deadline, formatCountdown, secondsLeft } from "../lib/countdown.ts";
import { plural, t, type Key } from "../i18n/index.ts";
import { captchaDialogOpen, pushToastForError, status } from "../store/app.ts";
import { openDialog } from "../store/dialogs.ts";
import { errorText } from "../store/errors.ts";

interface CaptchaInfo {
  id: string;
  hoster: string;
  type: string;
  kind: "image" | "interactive";
  explain?: string;
  timeout: number;
  created: number;
  remaining: number;
}

interface CaptchaDetail extends CaptchaInfo {
  imageDataUrl?: string;
}

type Item = CaptchaInfo & { due: number | null };

export const SKIP_TYPES = ["SINGLE", "BLOCK_HOSTER", "BLOCK_PACKAGE", "BLOCK_ALL_CAPTCHAS", "REFRESH", "STOP_CURRENT_ACTION", "TIMEOUT"] as const;
type SkipType = (typeof SKIP_TYPES)[number];

export function CaptchaDialog() {
  const onClose = () => (captchaDialogOpen.value = false);
  const [items, setItems] = useState<Item[] | null>(null);
  const [failed, setFailed] = useState<unknown>(null);
  const [selId, setSelId] = useState<string | null>(null);
  const count = status.value?.captchas ?? 0;

  const load = useCallback(async () => {
    try {
      const list = await get<CaptchaInfo[]>("/captchas");
      const at = Date.now();
      setItems((Array.isArray(list) ? list : []).map((c) => ({ ...c, due: deadline(c, at) })));
      setFailed(null);
    } catch (e) {
      setFailed(e);
    }
  }, []);
  // Again whenever JDownloader says the number changed, and now and then for the remaining times.
  useEffect(() => { void load(); }, [load, count]);
  useEffect(() => {
    const id = setInterval(load, 20_000);
    return () => clearInterval(id);
  }, [load]);

  useEffect(() => {
    if (!items) return;
    if (items.length === 0) {
      // Everything is done: the dialog has nothing left to show.
      const id = setTimeout(onClose, 600);
      return () => clearTimeout(id);
    }
    if (!selId || !items.some((c) => c.id === selId)) setSelId(items[0]!.id);
  }, [items, selId]);

  const sel = items?.find((c) => c.id === selId) ?? null;
  const done = (id: string) => {
    setItems((cur) => (cur ?? []).filter((c) => c.id !== id));
    void load();
  };

  return (
    <Dialog title={t("cap.title")} onClose={onClose} wide sticky>
      <p class="visually-hidden" role="status" aria-live="polite">{plural("cap.waiting", count)}</p>
      {failed ? (
        <div class="state-box" role="alert">
          <p>{errorText(failed)}</p>
          <button type="button" class="btn" onClick={load}>{t("common.retry")}</button>
        </div>
      ) : items === null ? (
        <div class="state-box" role="status">{t("boot.loading")}</div>
      ) : items.length === 0 ? (
        <div class="state-box" role="status"><p>{t("cap.none")}</p></div>
      ) : (
        <div class={`cap-split${items.length > 1 ? " multi" : ""}`}>
          {items.length > 1 ? (
            <ul class="cap-list" aria-label={t("cap.list")}>
              {items.map((c) => (
                <li key={c.id}>
                  <button type="button" class={`cap-item${c.id === selId ? " on" : ""}`} aria-pressed={c.id === selId} onClick={() => setSelId(c.id)}>
                    <Lamp tone={c.kind === "image" ? "accent" : "wait"} />
                    <span class="cap-item-host" dir="auto">{c.hoster || t("cap.unknownHoster")}</span>
                    <span class="mono muted small"><Remaining due={c.due} /></span>
                  </button>
                </li>
              ))}
            </ul>
          ) : null}
          {sel ? <CaptchaView key={sel.id} item={sel} onDone={() => done(sel.id)} onGone={load} /> : null}
        </div>
      )}
    </Dialog>
  );
}

function Remaining({ due }: { due: number | null }) {
  const now = useNow(1000);
  const s = secondsLeft(due, now);
  return <>{s === null ? "–" : formatCountdown(s)}</>;
}

function CaptchaView({ item, onDone, onGone }: { item: Item; onDone: () => void; onGone: () => void }) {
  const [detail, setDetail] = useState<CaptchaDetail | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [answer, setAnswer] = useState("");
  const [busy, setBusy] = useState(false);
  const [skipOpen, setSkipOpen] = useState(false);
  const [skip, setSkip] = useState<SkipType>("SINGLE");
  const [note, setNote] = useState("");
  const field = useRef<HTMLInputElement>(null);
  const now = useNow(1000);
  const left = secondsLeft(item.due, now);

  useEffect(() => {
    let cancelled = false;
    get<CaptchaDetail>(`/captchas/${item.id}`)
      .then((d) => !cancelled && setDetail(d))
      .catch((e) => {
        if (cancelled) return;
        if (isGone(e)) {
          setNote(t("cap.gone"));
          setTimeout(onGone, 800);
        } else setError(e);
      });
    return () => { cancelled = true; };
  }, [item.id]);

  useEffect(() => {
    if (detail?.kind === "image") field.current?.focus();
  }, [detail?.id]);

  // The time is up: JDownloader drops the captcha by itself, the list catches up.
  useEffect(() => {
    if (left === 0) {
      const id = setTimeout(onGone, 1500);
      return () => clearTimeout(id);
    }
  }, [left === 0]);

  const run = async (path: string, body: unknown) => {
    setBusy(true);
    try {
      await post(path, body);
      onDone();
    } catch (e) {
      if (isGone(e)) {
        setNote(t("cap.gone"));
        setTimeout(onGone, 800);
      } else {
        pushToastForError(e);
      }
      setBusy(false);
    }
  };
  const solve = (e: Event) => {
    e.preventDefault();
    if (answer.trim() === "" || busy) return;
    void run(`/captchas/${item.id}/solve`, { result: answer.trim() });
  };
  const doSkip = () => {
    const go = () => run(`/captchas/${item.id}/skip`, { type: skip });
    if (skip === "BLOCK_ALL_CAPTCHAS" || skip === "BLOCK_HOSTER") {
      openDialog((close) => (
        <ConfirmDialog
          title={t(`cap.skip.${skip}` as Key, { hoster: item.hoster })}
          lines={[t(`cap.skip.${skip}.effect` as Key, { hoster: item.hoster }), t("cap.skip.blockNote")]}
          confirmLabel={t("cap.skipRun")}
          danger
          onConfirm={go}
          onClose={close}
        />
      ));
    } else void go();
  };

  const d = detail;
  const interactive = (d ?? item).kind === "interactive";
  return (
    <div class="cap-view">
      <div class="cap-head">
        <div>
          <h3 class="cap-host" dir="auto">{item.hoster || t("cap.unknownHoster")}</h3>
          <p class="muted small mono" dir="auto">{item.type}</p>
        </div>
        <p class={`cap-timer mono${left !== null && left <= 15 ? " urgent" : ""}`} aria-label={t("cap.timeLeft")}>
          <span class="muted small">{t("cap.timeLeft")}</span>
          <b><Remaining due={item.due} /></b>
        </p>
      </div>
      {note ? <p class="note" role="status">{note}</p> : null}
      {error ? <p class="form-error" role="alert">{errorText(error)}</p> : null}
      {!d && !error && !note ? <p class="muted" role="status">{t("boot.loading")}</p> : null}
      {d && !interactive ? (
        <form onSubmit={solve} class="cap-form">
          {d.imageDataUrl ? <img class="cap-image" src={d.imageDataUrl} alt={t("cap.imageAlt", { hoster: item.hoster })} /> : null}
          <label class="field">
            <span class="field-label">{t("cap.answer")}</span>
            <input ref={field} class="input mono cap-input" value={answer} autocomplete="off" autocapitalize="off" spellcheck={false} disabled={busy} onInput={(e) => setAnswer(e.currentTarget.value)} />
          </label>
          <div class="btn-row">
            <button type="submit" class="btn pri" disabled={busy || answer.trim() === ""}>{t("cap.solve")}</button>
          </div>
        </form>
      ) : null}
      {d && interactive ? (
        <div class="cap-interactive">
          <p class="confirm-lead">{t("cap.interactive")}</p>
          <p class="confirm-more">{t("cap.interactiveText")}</p>
          {d.explain ? <p class="cap-explain" dir="auto"><span class="muted small">{t("cap.explain")}</span><br />{d.explain}</p> : null}
        </div>
      ) : null}
      <div class="cap-skip">
        <div class="btn-row">
          <button type="button" class="btn" disabled={busy} onClick={() => run(`/captchas/${item.id}/skip`, { type: "SINGLE" })}>{t("cap.skipOne")}</button>
          <button type="button" class="btn" aria-expanded={skipOpen} onClick={() => setSkipOpen(!skipOpen)}>{t("cap.skipMore")}</button>
        </div>
        {skipOpen ? (
          <fieldset class="group skip-group">
            <legend>{t("cap.skipHow")}</legend>
            {SKIP_TYPES.map((s) => (
              <label key={s} class="choice skip-choice">
                <input type="radio" name="skip" checked={skip === s} onChange={() => setSkip(s)} />
                <span>
                  <b>{t(`cap.skip.${s}` as Key, { hoster: item.hoster })}</b>
                  <small class="muted block">{t(`cap.skip.${s}.effect` as Key, { hoster: item.hoster })}</small>
                </span>
              </label>
            ))}
            <div class="btn-row"><button type="button" class="btn danger-text" disabled={busy} onClick={doSkip}>{t("cap.skipRun")}</button></div>
          </fieldset>
        ) : null}
      </div>
    </div>
  );
}

