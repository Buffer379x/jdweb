import { useState } from "preact/hooks";
import { get, post, put } from "../../api/client.ts";
import { Lamp } from "../../components/Lamp.tsx";
import { LoadState } from "../../components/LoadState.tsx";
import { useGet } from "../../hooks/useLoad.ts";
import { addressOf, problems, settleConnection, worst, type Check, type Report } from "../../lib/diag.ts";
import { has, t, type Key } from "../../i18n/index.ts";
import { live, pushToast, pushToastForError, status } from "../../store/app.ts";
import { errorDetail, errorText } from "../../store/errors.ts";
import { refreshStatus } from "../../store/live.ts";
import { PageHead, Section } from "./common.tsx";

/** The text of a check: the translation of its message key, or the English detail of an unknown key. */
export function checkText(c: Check): string {
  if (has(c.messageKey)) return t(c.messageKey as Key, { detail: c.detail, address: addressOf(c.detail), url: status.value?.jd.url ?? "" });
  return c.detail;
}

export function checkTitle(c: Check): string {
  const k = `diag.title.${c.id}`;
  return has(k) ? t(k as Key) : c.id;
}

function statusWord(s: Check["status"]): string {
  return s === "ok" ? t("diag.status.ok") : s === "warn" ? t("diag.status.warn") : t("diag.status.fail");
}

export function CheckList({ checks }: { checks: Check[] }) {
  return (
    <ul class="diag-checks">
      {checks.map((c) => (
        <li key={c.id} class={`check-item ${c.status}`}>
          <div class="check-head">
            <Lamp tone={c.status === "ok" ? "run" : c.status === "warn" ? "wait" : "err"} />
            <h3 class="check-title">{checkTitle(c)}</h3>
            <span class={`check-status ${c.status}`}>{statusWord(c.status)}</span>
          </div>
          <p class="check-text" dir="auto">{checkText(c)}</p>
        </li>
      ))}
    </ul>
  );
}

export async function acknowledge(): Promise<boolean> {
  try {
    await post("/diagnostics/acknowledge");
    return true;
  } catch (e) {
    pushToastForError(e);
    return false;
  }
}

export function DiagnosticsPage() {
  const load = useGet<Report>("/diagnostics");
  return (
    <>
      <PageHead
        title={t("set.diagnostics")}
        intro={t("diag.intro")}
        actions={<button type="button" class="btn" disabled={load.loading} onClick={load.reload}>{t("diag.recheck")}</button>}
      />
      <LoadState load={load}>
        {(r) => {
          const bad = problems(r);
          return (
            <>
              <Section title={t("diag.report")} id="diag-report">
                <p class={`diag-summary ${worst(r)}`} role="status">{worst(r) === "ok" ? t("diag.allOk") : worst(r) === "warn" ? t("diag.someWarn") : t("diag.someFail")}</p>
                <CheckList checks={r.checks} />
                {bad.length > 0 ? (
                  r.acknowledged ? (
                    <p class="st muted"><Lamp tone="run" />{t("diag.acknowledged")}</p>
                  ) : (
                    <div class="ack-box">
                      <p>{t("diag.ackText")}</p>
                      <button type="button" class="btn pri" onClick={async () => { if (await acknowledge()) { pushToast("info", t("diag.ackDone")); load.reload(); } }}>{t("diag.ack")}</button>
                    </div>
                  )
                ) : null}
              </Section>
              <Section title={t("conn.title")} id="diag-conn"><ConnectionEditor onChanged={load.reload} /></Section>
            </>
          );
        }}
      </LoadState>
    </>
  );
}

interface Conn {
  url: string;
  source: "ui" | "env" | "default";
  connection: string;
  detail: string;
}

function ConnectionEditor({ onChanged }: { onChanged: () => void }) {
  const load = useGet<Conn>("/connection");
  return <LoadState load={load}>{(c) => <ConnectionForm c={c} onChanged={() => { load.reload(); onChanged(); }} />}</LoadState>;
}

function ConnectionForm({ c, onChanged }: { c: Conn; onChanged: () => void }) {
  const [url, setUrl] = useState(c.url);
  const [busy, setBusy] = useState(false);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<Conn | null>(null);
  const save = async (e: Event) => {
    e.preventDefault();
    if (busy || url.trim() === "") return;
    setBusy(true);
    setError("");
    try {
      const first = await put<Conn>("/connection", { url: url.trim() });
      setUrl(first.url);
      setChecking(true);
      // "syncing" is not an answer yet: wait for what JDownloader says at the new address.
      const r = { ...first, ...(await settleConnection(first, () => get<Conn>("/connection"), (ms) => new Promise((res) => setTimeout(res, ms)))) };
      setResult(r);
      await refreshStatus();
      onChanged();
    } catch (err) {
      setError([errorText(err), errorDetail(err)].filter(Boolean).join(" "));
    }
    setBusy(false);
    setChecking(false);
  };
  const shown = result ?? c;
  const ok = shown.connection === "connected" || shown.connection === "syncing";
  return (
    <form onSubmit={save} class="conn-form">
      <p class="muted">{t("conn.intro")}</p>
      <label class="field">
        <span class="field-label">{t("conn.url")}</span>
        <input class="input mono" value={url} inputMode="url" autocomplete="off" spellcheck={false} dir="ltr" aria-invalid={error ? "true" : undefined} aria-describedby="conn-msg" disabled={!status.value} onInput={(e) => setUrl(e.currentTarget.value)} />
        <span class="field-hint">{t(`conn.source.${shown.source}` as Key)}</span>
      </label>
      <div class="btn-row">
        <button type="submit" class="btn pri" disabled={busy || url.trim() === "" || url.trim() === shown.url}>{busy ? t("cfg.saving") : t("conn.save")}</button>
      </div>
      <div id="conn-msg" role="status" aria-live="polite" class="conn-msg">
        {error ? <p class="form-error" role="alert">{error}</p> : null}
        {!error && checking ? <p class="muted">{t("conn.checking")}</p> : null}
        {!error && !checking && result ? (
          <p class="st"><Lamp tone={ok ? "run" : "err"} /><span dir="auto">{ok ? t("conn.worked") : (result.detail || t("conn.failed"))}</span></p>
        ) : null}
      </div>
      {!live.value && !result ? <p class="muted small">{t("conn.offlineNow")}</p> : null}
    </form>
  );
}

