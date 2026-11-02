import { ConfigList } from "./ConfigList.tsx";
import { LoadState } from "../../components/LoadState.tsx";
import { useGet } from "../../hooks/useLoad.ts";
import type { ConfigEntry } from "../../lib/editors.ts";
import { t } from "../../i18n/index.ts";
import { PageHead } from "./common.tsx";

const entries = (raw: { entries: ConfigEntry[] }) => raw.entries ?? [];

/** Every setting of JDownloader, with the editor that fits its type. */
export function ExpertPage() {
  const load = useGet<ConfigEntry[]>("/config", entries as never);
  return (
    <>
      <PageHead title={t("set.expert")} intro={t("expert.intro")} />
      <LoadState load={load}>{(list) => <ConfigList entries={list} api="config" label={t("set.expert")} />}</LoadState>
    </>
  );
}

/** The settings page of one installed extension. */
export function ExtensionConfigPage({ iface, name }: { iface: string; name?: string }) {
  const load = useGet<ConfigEntry[]>(iface ? `/config?interface=${encodeURIComponent(iface)}&extensions=true` : null, entries as never);
  return (
    <>
      <PageHead title={name ?? t("set.extension")} intro={t("extension.intro")} />
      {!iface ? (
        <div class="state-box"><p>{t("extension.none")}</p></div>
      ) : (
        <LoadState load={load}>{(list) => <ConfigList entries={list} api="config" label={name ?? t("set.extension")} empty={t("extension.empty")} />}</LoadState>
      )}
    </>
  );
}
