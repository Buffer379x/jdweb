// The settings people change often: folder, simultaneous downloads, speed limit, switches, archive passwords.

import { useMemo, useState } from "preact/hooks";
import { post } from "../../api/client.ts";
import { LoadState } from "../../components/LoadState.tsx";
import { Switch } from "../../components/Switch.tsx";
import { useGet } from "../../hooks/useLoad.ts";
import { parseDecimal, splitBytesPerSecond, toBytesPerSecond, type SpeedUnit } from "../../lib/units.ts";
import type { ConfigEntry } from "../../lib/editors.ts";
import { t, type Key } from "../../i18n/index.ts";
import { live, pushToast, pushToastForError, status } from "../../store/app.ts";
import { reconnectNow, setSpeedLimit, setToggle, type ToggleName } from "../../store/jd.ts";
import { ConfigRow, RowStates } from "./ConfigRow.tsx";
import { PageHead, Section } from "./common.tsx";

const GENERAL = "org.jdownloader.settings.GeneralSettings";
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
  const initial = splitBytesPerSecond(tg?.limitBytes && tg.limitBytes > 0 ? tg.limitBytes : 1024 * 1024);
  const [value, setValue] = useState(String(initial.value));
  const [unit, setUnit] = useState<SpeedUnit>(initial.unit);
  const [busy, setBusy] = useState(false);
  const n = parseDecimal(value);
  const bytes = n === null ? null : toBytesPerSecond(n, unit);
  const on = !!tg?.limit;
  const current = tg && tg.limitBytes > 0 ? splitBytesPerSecond(tg.limitBytes) : null;
  const dirty = bytes !== null && bytes !== tg?.limitBytes;
  const apply = async (enabled: boolean, b: number) => {
    setBusy(true);
    const ok = await setSpeedLimit(enabled, b);
    setBusy(false);
    if (ok) pushToast("info", t("gen.limitSaved"));
  };
  return (
    <div class="speed-limit">
      <Switch checked={on} disabled={!live.value || busy} onChange={(v) => void apply(v, 0)} hint={on && current ? t("gen.limitNow", { value: `${current.value} ${current.unit}/s` }) : t("gen.limitOffHint")}>
        {t("gen.limit")}
      </Switch>
      <div class="speed-edit">
        <label class="field-inline">
          <span class="field-label">{t("gen.limitValue")}</span>
          <input class={`input mono narrow`} inputMode="decimal" value={value} aria-invalid={n !== null && bytes === null ? "true" : value !== "" && n === null ? "true" : undefined} autocomplete="off" disabled={!live.value} onInput={(e) => setValue(e.currentTarget.value)} />
        </label>
        <label class="field-inline">
          <span class="field-label">{t("gen.limitUnit")}</span>
          <select class="input narrow" value={unit} disabled={!live.value} onChange={(e) => setUnit(e.currentTarget.value as SpeedUnit)}>
            <option value="KB">KB/s</option>
            <option value="MB">MB/s</option>
          </select>
        </label>
        <button type="button" class="btn pri" disabled={!live.value || busy || bytes === null || !dirty} onClick={() => void apply(on, bytes!)}>{t("cfg.save")}</button>
      </div>
      {value !== "" && bytes === null ? <p class="field-hint bad" role="alert">{t("gen.limitInvalid")}</p> : null}
    </div>
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
