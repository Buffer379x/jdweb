// The settings people change often: folder, simultaneous downloads, speed limit, switches, archive passwords.

import { useEffect, useMemo, useState } from "preact/hooks";
import { post, put } from "../../api/client.ts";
import { LoadState } from "../../components/LoadState.tsx";
import { Switch } from "../../components/Switch.tsx";
import { useGet } from "../../hooks/useLoad.ts";
import { describeRate, type SpeedUnit } from "../../lib/units.ts";
import { edit, followStatus, formBytes, initialForm, isDirty, switchRequest } from "../../lib/speedlimit.ts";
import type { ConfigEntry } from "../../lib/editors.ts";
import { t, type Key } from "../../i18n/index.ts";
import { live, pushToast, pushToastForError, status } from "../../store/app.ts";
import { reconnectNow, setSpeedLimit, setToggle, type ToggleName } from "../../store/jd.ts";
import { ConfigRow, RowStates } from "./ConfigRow.tsx";
import { PageHead, Section } from "./common.tsx";

const GENERAL = "org.jdownloader.settings.GeneralSettings";
const LINKGRABBER = "org.jdownloader.gui.views.linkgrabber.addlinksdialog.LinkgrabberSettings";
const AUTO_START = "LinkgrabberAutoStartEnabled";
const FIELDS: { key: string; label: Key }[] = [
  { key: "DefaultDownloadFolder", label: "gen.folder" },
  { key: "MaxSimultaneDownloads", label: "gen.maxDownloads" },
  { key: "MaxSimultaneDownloadsPerHost", label: "gen.maxPerHost" },
];

export function GeneralPage() {
  const load = useGet<ConfigEntry[]>(`/config?interface=${GENERAL}`, ((r: { entries: ConfigEntry[] }) => r.entries ?? []) as never);
  return (
    <>
      <PageHead title={t("set.general")} intro={t("gen.intro")} />
      <LoadState load={load}>{(entries) => <GeneralBody entries={entries} />}</LoadState>
    </>
  );
}

function GeneralBody({ entries }: { entries: ConfigEntry[] }) {
  const states = useMemo(() => new RowStates(), [entries]);
  const general = entries.filter((e) => e.storage === "" && e.interface === GENERAL);
  return (
    <>
      <Section title={t("gen.downloads")} id="gen-downloads">
        <div class="cfg-form" role="list">
          {FIELDS.map((f) => {
            const e = general.find((x) => x.key === f.key);
            return e ? <ConfigRow key={f.key} entry={e} api="config" state={states.of(e, "config")} label={t(f.label)} flex showOrigin={false} /> : null;
          })}
          {FIELDS.every((f) => !general.find((x) => x.key === f.key)) ? <p class="muted">{t("gen.noFields")}</p> : null}
        </div>
        <SpeedLimit />
      </Section>
      <Section title={t("gen.switches")} id="gen-switches">
        <Switches />
      </Section>
      <Section title={t("gen.archives")} id="gen-archives">
        <ArchivePassword />
      </Section>
    </>
  );
}

function SpeedLimit() {
  const tg = status.value?.toggles;
  const [form, setForm] = useState(() => initialForm(tg?.limitBytes));
  const [busy, setBusy] = useState(false);
  // JDownloader's value changed (another browser): an untouched form follows it.
  useEffect(() => setForm((f) => followStatus(f, tg?.limitBytes)), [tg?.limitBytes]);
  const bytes = formBytes(form);
  const on = !!tg?.limit;
  const dirty = isDirty(form, tg?.limitBytes);
  const apply = async (enabled: boolean, b: number) => {
    setBusy(true);
    const ok = await setSpeedLimit(enabled, b);
    setBusy(false);
    if (ok) {
      pushToast("info", t("gen.limitSaved"));
      if (b > 0) setForm((f) => ({ ...f, edited: false })); // saved: the form is JDownloader's value again
    }
  };
  const flip = (v: boolean) => {
    const req = switchRequest(v, form, tg?.limitBytes);
    if (req.ok) void apply(req.enabled, req.bytesPerSecond);
    else pushToast("error", t("gen.limitInvalid"));
  };
  return (
    <div class="speed-limit">
      <Switch checked={on} disabled={!live.value || busy} onChange={flip} hint={on && tg && tg.limitBytes > 0 ? t("gen.limitNow", { value: describeRate(tg.limitBytes) }) : t("gen.limitOffHint")}>
        {t("gen.limit")}
      </Switch>
      <div class="speed-edit">
        <label class="field-inline">
          <span class="field-label">{t("gen.limitValue")}</span>
          <input class={`input mono narrow`} inputMode="decimal" value={form.value} aria-invalid={form.value !== "" && bytes === null ? "true" : undefined} autocomplete="off" disabled={!live.value} onInput={(e) => { const v = e.currentTarget.value; setForm((f) => edit(f, { value: v })); }} />
        </label>
        <label class="field-inline">
          <span class="field-label">{t("gen.limitUnit")}</span>
          <select class="input narrow" value={form.unit} disabled={!live.value} onChange={(e) => { const u = e.currentTarget.value as SpeedUnit; setForm((f) => edit(f, { unit: u })); }}>
            <option value="KB">KB/s</option>
            <option value="MB">MB/s</option>
          </select>
        </label>
        <button type="button" class="btn pri" disabled={!live.value || busy || !dirty} onClick={() => void apply(on, bytes!)}>{t("cfg.save")}</button>
      </div>
      {form.value !== "" && bytes === null ? <p class="field-hint bad" role="alert">{t("gen.limitInvalid")}</p> : null}
    </div>
  );
}

/** JDownloader's own switch: links that are moved to the download list start by themselves. JDWeb never changes it alone. */
function AutoStart() {
  const load = useGet<boolean | null>(
    `/config?interface=${LINKGRABBER}`,
    ((r: { entries: ConfigEntry[] }) => {
      const e = (r.entries ?? []).find((x) => x.key === AUTO_START && x.storage === "");
      return e ? e.value === true : null;
    }) as never,
  );
  const [busy, setBusy] = useState(false);
  if (load.data === undefined || load.data === null) return null;
  const change = async (v: boolean) => {
    setBusy(true);
    try {
      await put("/config", { interface: LINKGRABBER, storage: "", key: AUTO_START, value: v });
      load.setData(v);
    } catch (err) {
      pushToastForError(err);
    }
    setBusy(false);
  };
  return (
    <Switch checked={load.data} disabled={!live.value || busy} onChange={(v) => void change(v)} hint={t("gen.autoStart.hint")}>
      {t("gen.autoStart")}
    </Switch>
  );
}

function Switches() {
  const tg = status.value?.toggles;
  const on = live.value;
  const sw = (name: ToggleName, checked: boolean, label: Key, hint: Key) => (
    <Switch checked={checked} disabled={!on} onChange={(v) => void setToggle(name, v)} hint={t(hint)}>{t(label)}</Switch>
  );
  return (
    <div class="switch-list">
      <AutoStart />
      {sw("premium", !!tg?.premium, "toggle.premium", "toggle.premium.hint")}
      {sw("clipboard", !!tg?.clipboard, "toggle.clipboard", "toggle.clipboard.hint")}
      {sw("reconnect", !!tg?.reconnect, "toggle.reconnect", "toggle.reconnect.hint")}
      {sw("stop-after", !!tg?.stopAfter, "toggle.stopAfter", "toggle.stopAfter.hint")}
      <div>
        <button type="button" class="btn" disabled={!on} onClick={() => void reconnectNow()}>{t("toggle.reconnectNow")}</button>
      </div>
    </div>
  );
}

function ArchivePassword() {
  const [pw, setPw] = useState("");
  const [busy, setBusy] = useState(false);
  const add = async (e: Event) => {
    e.preventDefault();
    if (pw === "" || busy) return;
    setBusy(true);
    try {
      await post("/extraction/passwords", { password: pw });
      setPw("");
      pushToast("info", t("gen.archiveAdded"));
    } catch (err) {
      pushToastForError(err);
    }
    setBusy(false);
  };
  return (
    <form class="inline-form" onSubmit={add}>
      <label class="field grow">
        <span class="field-label">{t("gen.archivePassword")}</span>
        <input class="input mono" type="password" value={pw} autocomplete="new-password" disabled={!live.value} onInput={(e) => setPw(e.currentTarget.value)} />
        <span class="field-hint">{t("gen.archiveHint")}</span>
      </label>
      <button type="submit" class="btn pri" disabled={!live.value || pw === "" || busy}>{t("gen.archiveAdd")}</button>
    </form>
  );
}
