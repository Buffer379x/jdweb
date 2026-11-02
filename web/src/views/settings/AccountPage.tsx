// The account of JDWeb itself: password, two-factor sign-in with recovery codes, open sessions.

import { useId, useState } from "preact/hooks";
import { del, get, post } from "../../api/client.ts";
import { copyText, downloadText } from "../../components/clipboard.ts";
import { ConfirmDialog, Dialog } from "../../components/Dialog.tsx";
import { Lamp } from "../../components/Lamp.tsx";
import { LoadState } from "../../components/LoadState.tsx";
import { useGet } from "../../hooks/useLoad.ts";
import { describeAgent } from "../../lib/agent.ts";
import { formatDate } from "../../lib/format.ts";
import { qrDataUrl } from "../../lib/qr.ts";
import { lang, plural, t } from "../../i18n/index.ts";
import { pushToast, session } from "../../store/app.ts";
import { openDialog } from "../../store/dialogs.ts";
import { errorText, passwordErrorText } from "../../store/errors.ts";
import { PageHead, Section } from "./common.tsx";
import type { Session } from "../../api/types.ts";

const MIN_PASSWORD = 12;

export function AccountPage() {
  return (
    <>
      <PageHead title={t("set.account")} intro={t("account.intro", { user: session.value?.username ?? "" })} />
      <Section title={t("account.password")} id="acc-pw"><PasswordForm /></Section>
      <Section title={t("account.totp")} id="acc-totp"><TotpBox /></Section>
      <Section title={t("account.sessions")} id="acc-sessions"><Sessions /></Section>
    </>
  );
}

function PasswordForm() {
  const [cur, setCur] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const short = next.length > 0 && next.length < MIN_PASSWORD;
  const mismatch = again.length > 0 && next !== again;
  const can = cur !== "" && next.length >= MIN_PASSWORD && next === again && !busy;
  const submit = async (e: Event) => {
    e.preventDefault();
    if (!can) return;
    setBusy(true);
    setError("");
    setDone(false);
    try {
      await post("/password", { current: cur, new: next });
      setCur(""); setNext(""); setAgain("");
      setDone(true);
    } catch (err) {
      setError(passwordErrorText(err));
    }
    setBusy(false);
  };
  return (
    <form class="narrow-form" onSubmit={submit}>
      <label class="field">
        <span class="field-label">{t("account.currentPassword")}</span>
        <input class="input" type="password" autocomplete="current-password" value={cur} onInput={(e) => setCur(e.currentTarget.value)} />
      </label>
      <label class="field">
        <span class="field-label">{t("account.newPassword")}</span>
        <input class="input" type="password" autocomplete="new-password" value={next} aria-invalid={short ? "true" : undefined} onInput={(e) => setNext(e.currentTarget.value)} />
        <span class={`field-hint${short ? " bad" : ""}`}>{t("setup.passwordHint", { n: MIN_PASSWORD })}</span>
      </label>
      <label class="field">
        <span class="field-label">{t("setup.repeat")}</span>
        <input class="input" type="password" autocomplete="new-password" value={again} aria-invalid={mismatch ? "true" : undefined} onInput={(e) => setAgain(e.currentTarget.value)} />
        {mismatch ? <span class="field-hint bad">{t("setup.mismatch")}</span> : null}
      </label>
      <div class="form-msg" role="status" aria-live="polite">
        {error ? <p class="form-error">{error}</p> : null}
        {done ? <p class="st"><Lamp tone="run" />{t("account.passwordChanged")}</p> : null}
      </div>
      <button type="submit" class="btn pri" disabled={!can}>{t("account.changePassword")}</button>
      <p class="muted small">{t("account.passwordNote")}</p>
    </form>
  );
}

async function refreshSession(): Promise<void> {
  try {
    session.value = await get<Session>("/session");
  } catch {
    /* the 401 handler of the client deals with an ended session */
  }
}

function TotpBox() {
  const on = !!session.value?.totpEnabled;
  return (
    <div class="totp-box">
      <p class="st"><Lamp tone={on ? "run" : "muted"} /><span role="status">{on ? t("account.totpOn") : t("account.totpOff")}</span></p>
      <p class="muted">{on ? t("account.totpOnText") : t("account.totpOffText")}</p>
      {on ? (
        <button type="button" class="btn" onClick={() => openDialog((close) => <TotpDisableDialog onClose={close} />)}>{t("account.totpDisable")}</button>
      ) : (
        <button type="button" class="btn pri" onClick={() => openDialog((close) => <TotpEnrollDialog onClose={close} />)}>{t("account.totpEnable")}</button>
      )}
    </div>
  );
}

function TotpDisableDialog({ onClose }: { onClose: () => void }) {
  const [pw, setPw] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const id = useId();
  const submit = async (e?: Event) => {
    e?.preventDefault();
    if (!pw || busy) return;
    setBusy(true);
    try {
      await post("/totp/disable", { password: pw });
      await refreshSession();
      pushToast("info", t("account.totpDisabled"));
      onClose();
    } catch (err) {
      setError(passwordErrorText(err));
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t("account.totpDisable")}
      onClose={onClose}
      footer={<><button type="button" class="btn" onClick={onClose}>{t("common.cancel")}</button><button type="submit" form={id} class="btn danger" disabled={!pw || busy}>{t("account.totpDisable")}</button></>}
    >
      <form id={id} onSubmit={submit}>
        <p class="confirm-more">{t("account.totpDisableText")}</p>
        <label class="field">
          <span class="field-label">{t("login.password")}</span>
          <input class="input" type="password" data-autofocus="" autocomplete="current-password" value={pw} onInput={(e) => setPw(e.currentTarget.value)} />
        </label>
        {error ? <p class="form-error" role="alert">{error}</p> : null}
      </form>
    </Dialog>
  );
}

interface Enroll {
  secret: string;
  uri: string;
  qrSvg: string;
}

function TotpEnrollDialog({ onClose }: { onClose: () => void }) {
  const [step, setStep] = useState<"password" | "code" | "codes">("password");
  const [pw, setPw] = useState("");
  const [code, setCode] = useState("");
  const [enroll, setEnroll] = useState<Enroll | null>(null);
  const [codes, setCodes] = useState<string[]>([]);
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const id = useId();

  const start = async (e?: Event) => {
    e?.preventDefault();
    if (!pw || busy) return;
    setBusy(true);
    setError("");
    try {
      setEnroll(await post<Enroll>("/totp/enroll", { password: pw }));
      setPw("");
      setStep("code");
    } catch (err) {
      setError(passwordErrorText(err));
    }
    setBusy(false);
  };
  const confirmCode = async (e?: Event) => {
    e?.preventDefault();
    if (!code.trim() || busy) return;
    setBusy(true);
    setError("");
    try {
      const r = await post<{ recoveryCodes: string[] }>("/totp/confirm", { code: code.trim() });
      setCodes(r.recoveryCodes ?? []);
      setStep("codes");
      await refreshSession();
    } catch (err) {
      setError(errorText(err));
    }
    setBusy(false);
  };
  const qr = enroll ? qrDataUrl(enroll.qrSvg) : null;
  const text = codes.join("\n");
  // The codes are shown once: closing before they are saved is not an accident that Escape may cause.
  const dismiss = step === "codes" && !saved ? () => {} : onClose;

  return (
    <Dialog
      title={t("account.totpEnable")}
      onClose={onClose}
      onDismiss={dismiss}
      sticky
      footer={
        step === "password" ? (
          <><button type="button" class="btn" onClick={onClose}>{t("common.cancel")}</button><button type="submit" form={id} class="btn pri" disabled={!pw || busy}>{t("common.next")}</button></>
        ) : step === "code" ? (
          <><button type="button" class="btn" onClick={onClose}>{t("common.cancel")}</button><button type="submit" form={id} class="btn pri" disabled={!code.trim() || busy}>{t("account.totpConfirm")}</button></>
        ) : (
          <button type="button" class="btn pri" disabled={!saved} onClick={onClose}>{t("common.done")}</button>
        )
      }
    >
      {step === "password" ? (
        <form id={id} onSubmit={start}>
          <p class="confirm-more">{t("account.enrollText")}</p>
          <label class="field">
            <span class="field-label">{t("login.password")}</span>
            <input class="input" type="password" data-autofocus="" autocomplete="current-password" value={pw} onInput={(e) => setPw(e.currentTarget.value)} />
          </label>
          {error ? <p class="form-error" role="alert">{error}</p> : null}
        </form>
      ) : step === "code" && enroll ? (
        <form id={id} onSubmit={confirmCode}>
          <p class="confirm-more">{t("account.scanText")}</p>
          <div class="qr-row">
            {qr ? <img class="qr" src={qr} alt={t("account.qrAlt")} width={192} height={192} /> : <p class="form-error" role="alert">{t("account.qrBad")}</p>}
            <div>
              <p class="field-label">{t("account.secret")}</p>
              <p class="mono secret-text" dir="ltr">{enroll.secret.replace(/(.{4})/g, "$1 ").trim()}</p>
              <button type="button" class="btn small" onClick={async () => pushToast("info", (await copyText(enroll.secret)) ? t("toast.copied") : t("toast.copyFailed"))}>{t("common.copy")}</button>
            </div>
          </div>
          <label class="field">
            <span class="field-label">{t("account.code")}</span>
            <input class="input mono narrow" inputMode="numeric" autocomplete="one-time-code" data-autofocus="" value={code} onInput={(e) => setCode(e.currentTarget.value)} />
          </label>
          {error ? <p class="form-error" role="alert">{error}</p> : null}
        </form>
      ) : (
        <div>
          <p class="confirm-lead">{t("account.codesTitle")}</p>
          <p class="confirm-more">{t("account.codesText", { n: codes.length })}</p>
          <ul class="recovery mono" aria-label={t("account.codesTitle")}>{codes.map((c) => <li key={c}>{c}</li>)}</ul>
          <div class="btn-row">
            <button type="button" class="btn" onClick={async () => pushToast("info", (await copyText(text)) ? t("toast.copied") : t("toast.copyFailed"))}>{t("common.copy")}</button>
            <button type="button" class="btn" onClick={() => downloadText("jdweb-recovery-codes.txt", `JDWeb\n${text}\n`)}>{t("account.codesDownload")}</button>
          </div>
          <label class="check"><input type="checkbox" data-autofocus="" checked={saved} onChange={(e) => setSaved(e.currentTarget.checked)} /><span>{t("account.codesSaved")}</span></label>
        </div>
      )}
    </Dialog>
  );
}

interface SessionInfo {
  id: string;
  createdAt: string;
  lastSeenAt: string;
  expiresAt: string;
  userAgent: string;
  current: boolean;
}

const when = (iso: string) => formatDate(Date.parse(iso), lang.value);

function Sessions() {
  const load = useGet<SessionInfo[]>("/sessions");
  const others = (load.data ?? []).filter((s) => !s.current).length;
  return (
    <LoadState load={load}>
      {(list) => (
        <>
          <div class="table-wrap">
            <table class="data-table">
              <caption class="visually-hidden">{t("account.sessions")}</caption>
              <thead>
                <tr>
                  <th scope="col">{t("account.sess.device")}</th>
                  <th scope="col">{t("account.sess.created")}</th>
                  <th scope="col">{t("account.sess.lastSeen")}</th>
                  <th scope="col">{t("account.sess.expires")}</th>
                </tr>
              </thead>
              <tbody>
                {list.map((s) => (
                  <tr key={s.id}>
                    <td data-label={t("account.sess.device")} dir="auto" title={s.userAgent}>
                      {describeAgent(s.userAgent) || t("account.sess.unknown")}
                      {s.current ? <span class="cfg-tag current">{t("account.sess.current")}</span> : null}
                    </td>
                    <td data-label={t("account.sess.created")} class="mono">{when(s.createdAt)}</td>
                    <td data-label={t("account.sess.lastSeen")} class="mono">{when(s.lastSeenAt)}</td>
                    <td data-label={t("account.sess.expires")} class="mono">{when(s.expiresAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div class="btn-row">
            <button
              type="button"
              class="btn"
              disabled={others === 0}
              onClick={() => openDialog((close) => (
                <ConfirmDialog
                  title={t("account.signOutOthers")}
                  lines={[plural("account.signOutWhat", others), t("account.signOutConsequence")]}
                  confirmLabel={t("account.signOutOthers")}
                  danger
                  onConfirm={async () => { await del("/sessions"); load.reload(); }}
                  onClose={close}
                />
              ))}
            >
              {t("account.signOutOthers")}
            </button>
            {others === 0 ? <span class="muted small">{t("account.noOthers")}</span> : null}
          </div>
        </>
      )}
    </LoadState>
  );
}

