import { t } from "../i18n/index.ts";

/** Placeholder. The settings, accounts, diagnostics and the captcha dialog replace this view. */
export function SettingsView() {
  return (
    <section class="settings-stub">
      <h1>{t("tab.settings")}</h1>
      <p>{t("settings.soon")}</p>
    </section>
  );
}
