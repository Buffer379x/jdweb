import { useState } from "preact/hooks";
import { post } from "../../api/client.ts";
import { LoadState } from "../../components/LoadState.tsx";
import { Lamp } from "../../components/Lamp.tsx";
import { useGet } from "../../hooks/useLoad.ts";
import { t } from "../../i18n/index.ts";
import { live, navigateSettings, pushToast, pushToastForError } from "../../store/app.ts";
import { PageHead } from "./common.tsx";

export interface ExtensionInfo {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  installed: boolean;
  iconKey: string;
  configInterface: string;
}

export function ExtensionsPage({ reloadNav }: { reloadNav: () => void }) {
  const load = useGet<ExtensionInfo[]>("/extensions");
  const [busy, setBusy] = useState<string | null>(null);
  const run = async (x: ExtensionInfo, action: "install" | "enable" | "disable") => {
    setBusy(x.id);
    try {
      await post(`/extensions/${action}`, { id: x.id });
      if (action === "install") pushToast("info", t("ext.installed", { name: x.name }));
    } catch (e) {
      pushToastForError(e);
    }
    setBusy(null);
    load.reload();
    reloadNav();
  };
  return (
    <>
      <PageHead title={t("set.extensions")} intro={t("ext.intro")} />
      <LoadState load={load}>
        {(list) =>
          list.length === 0 ? (
            <div class="state-box"><p>{t("ext.none")}</p></div>
          ) : (
            <ul class="cards">
              {list.map((x) => (
                <li key={x.id} class="card">
                  <div class="card-main">
                    <h2 class="card-title" dir="auto">{x.name}</h2>
                    <p class="muted" dir="auto">{x.description}</p>
                    <p class="st small">
                      <Lamp tone={x.installed ? (x.enabled ? "run" : "muted") : "muted"} />
                      {!x.installed ? t("ext.state.available") : x.enabled ? t("ext.state.enabled") : t("ext.state.disabled")}
                    </p>
                  </div>
                  <div class="card-actions">
                    {!x.installed ? (
                      <button type="button" class="btn pri" disabled={!live.value || busy === x.id} onClick={() => run(x, "install")}>{t("ext.install")}</button>
                    ) : (
                      <>
                        <button type="button" class="btn" disabled={!live.value || busy === x.id} onClick={() => run(x, x.enabled ? "disable" : "enable")}>
                          {x.enabled ? t("ext.disable") : t("ext.enable")}
                        </button>
                        {x.configInterface ? (
                          <a class="btn" href={`/settings/extension?interface=${encodeURIComponent(x.configInterface)}`} onClick={(e) => { e.preventDefault(); navigateSettings("extension", x.configInterface); }}>
                            {t("ext.configure")}
                          </a>
                        ) : null}
                      </>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )
        }
      </LoadState>
    </>
  );
}
