import { useState } from "preact/hooks";
import { get, isApiError, post } from "../api/client.ts";
import type { Session } from "../api/types.ts";
import { t } from "../i18n/index.ts";
import { checkSession, loadPrefs, session, setupRequired } from "../store/app.ts";
import { errorText } from "../store/errors.ts";
import { UserMenuLite } from "./Lite.tsx";

const MIN_PASSWORD = 12;

function Frame({ title, intro, children }: { title: string; intro: string; children: preact.ComponentChildren }) {
  return (
    <main class="auth">
      <div class="auth-panel">
        <div class="auth-brand">
          <img src="/jdweb-mark.svg" alt="" width={44} height={44} />
          <span class="word">JD<span>Web</span></span>
        </div>
        <h1>{title}</h1>
        <p class="auth-intro">{intro}</p>
        {children}
      </div>
      <UserMenuLite />
    </main>
  );
}

async function enter(username: string, password: string, totp?: string): Promise<void> {
  await post("/login", totp ? { username, password, totp } : { username, password }, { ownUnauthorized: true });
  await loadPrefs();
  session.value = await get<Session>("/session");
}

export function SetupView() {
  const [token, setToken] = useState("");
  const [user, setUser] = useState("admin");
  const [pw, setPw] = useState("");
  const [pw2, setPw2] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const short = pw.length > 0 && pw.length < MIN_PASSWORD;
  const mismatch = pw2.length > 0 && pw !== pw2;
  const can = token.trim() !== "" && user.trim() !== "" && pw.length >= MIN_PASSWORD && pw === pw2 && !busy;
  const submit = async (e: Event) => {
    e.preventDefault();
    if (!can) return;
    setBusy(true);
    setErr("");
    try {
      await post("/setup", { token: token.trim(), username: user.trim(), password: pw }, { ownUnauthorized: true });
      setupRequired.value = false;
      await enter(user.trim(), pw);
    } catch (x) {
      setErr(isApiError(x) && x.code === "bad_request" ? t("setup.badToken") : errorText(x));
      setBusy(false);
      if (isApiError(x) && x.code === "conflict") void checkSession();
    }
  };
  return (
    <Frame title={t("setup.title")} intro={t("setup.intro")}>
      <form onSubmit={submit} class="auth-form">
        <label class="field">
          <span class="field-label">{t("setup.token")}</span>
          <input class="input mono" value={token} data-autofocus autofocus autocomplete="off" spellcheck={false} onInput={(e) => setToken(e.currentTarget.value)} />
          <span class="field-hint">{t("setup.tokenHint")}</span>
        </label>
        <label class="field">
          <span class="field-label">{t("login.username")}</span>
          <input class="input" value={user} autocomplete="username" onInput={(e) => setUser(e.currentTarget.value)} />
          <span class="field-hint">{t("setup.userHint")}</span>
        </label>
        <label class="field">
          <span class="field-label">{t("login.password")}</span>
          <input class="input" type="password" value={pw} autocomplete="new-password" aria-invalid={short} onInput={(e) => setPw(e.currentTarget.value)} />
          <span class={`field-hint${short ? " bad" : ""}`}>{t("setup.passwordHint", { n: MIN_PASSWORD })}</span>
        </label>
        <label class="field">
          <span class="field-label">{t("setup.repeat")}</span>
          <input class="input" type="password" value={pw2} autocomplete="new-password" aria-invalid={mismatch} onInput={(e) => setPw2(e.currentTarget.value)} />
          {mismatch ? <span class="field-hint bad">{t("setup.mismatch")}</span> : null}
        </label>
        <div class="form-error" role="alert">{err}</div>
        <button type="submit" class="btn pri wide" disabled={!can}>{t("setup.submit")}</button>
      </form>
    </Frame>
  );
}

export function LoginView() {
  const [user, setUser] = useState("");
  const [pw, setPw] = useState("");
  const [totp, setTotp] = useState("");
  const [needTotp, setNeedTotp] = useState(false);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (e: Event) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setErr("");
    try {
      await enter(user.trim(), pw, needTotp ? totp.trim() : undefined);
    } catch (x) {
      if (isApiError(x) && x.code === "totp_required") {
        setNeedTotp(true);
      } else if (isApiError(x) && x.code === "unauthorized") {
        setErr(needTotp ? t("login.failedTotp") : t("login.failed"));
      } else {
        setErr(errorText(x));
      }
      setBusy(false);
    }
  };
  return (
    <Frame title={t("login.title")} intro={t("login.intro")}>
      <form onSubmit={submit} class="auth-form">
        <label class="field">
          <span class="field-label">{t("login.username")}</span>
          <input class="input" value={user} autofocus data-autofocus autocomplete="username" onInput={(e) => setUser(e.currentTarget.value)} disabled={needTotp} />
        </label>
        <label class="field">
          <span class="field-label">{t("login.password")}</span>
          <input class="input" type="password" value={pw} autocomplete="current-password" onInput={(e) => setPw(e.currentTarget.value)} disabled={needTotp} />
        </label>
        {needTotp ? (
          <label class="field">
            <span class="field-label">{t("login.totp")}</span>
            <input class="input mono" value={totp} autofocus inputMode="numeric" autocomplete="one-time-code" spellcheck={false} onInput={(e) => setTotp(e.currentTarget.value)} />
            <span class="field-hint">{t("login.totpHint")}</span>
          </label>
        ) : null}
        <div class="form-error" role="alert">{err}</div>
        <button type="submit" class="btn pri wide" disabled={busy || user.trim() === "" || pw === "" || (needTotp && totp.trim() === "")}>
          {t("login.submit")}
        </button>
      </form>
    </Frame>
  );
}
