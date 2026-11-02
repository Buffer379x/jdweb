// The settings as a routed sub-layout: a column of pages on the left on a desktop, the list of pages on a phone.

import type { ComponentChildren } from "preact";
import { useEffect } from "preact/hooks";
import { Icon } from "../../components/Icon.tsx";
import { t, type Key } from "../../i18n/index.ts";
import { extensionInterface, navigateSettings, settingsPage, type SettingsPage } from "../../store/app.ts";
import { compact } from "../../store/viewport.ts";
import { useGet } from "../../hooks/useLoad.ts";
import type { ExtensionInfo } from "./Extensions.tsx";
import { AccountPage } from "./AccountPage.tsx";
import { AccountsPage, BasicAuthPage } from "./Accounts.tsx";
import { DiagnosticsPage } from "./Diagnostics.tsx";
import { ExpertPage, ExtensionConfigPage } from "./Expert.tsx";
import { ExtensionsPage } from "./Extensions.tsx";
import { GeneralPage } from "./General.tsx";
import { PluginsPage } from "./Plugins.tsx";
import { SystemPage } from "./System.tsx";

interface NavItem {
  page: SettingsPage;
  label: Key;
  hint: Key;
}

const NAV: NavItem[] = [
  { page: "general", label: "set.general", hint: "set.general.hint" },
  { page: "accounts", label: "set.accounts", hint: "set.accounts.hint" },
  { page: "basicauth", label: "set.basicauth", hint: "set.basicauth.hint" },
  { page: "plugins", label: "set.plugins", hint: "set.plugins.hint" },
  { page: "extensions", label: "set.extensions", hint: "set.extensions.hint" },
  { page: "expert", label: "set.expert", hint: "set.expert.hint" },
  { page: "system", label: "set.system", hint: "set.system.hint" },
  { page: "account", label: "set.account", hint: "set.account.hint" },
  { page: "diagnostics", label: "set.diagnostics", hint: "set.diagnostics.hint" },
];

function href(page: SettingsPage, iface?: string): string {
  return page === "extension" && iface ? `/settings/extension?interface=${encodeURIComponent(iface)}` : `/settings/${page}`;
}

function NavLink({ page, iface, children, current, sub }: { page: SettingsPage; iface?: string; children: ComponentChildren; current: boolean; sub?: boolean }) {
  return (
    <a
      href={href(page, iface)}
      class={`snav-link${current ? " on" : ""}${sub ? " sub" : ""}`}
      aria-current={current ? "page" : undefined}
      onClick={(e) => {
        if (e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        navigateSettings(page, iface);
      }}
    >
      {children}
    </a>
  );
}

export function SettingsView() {
  const page = settingsPage.value;
  const phone = compact.value;
  const exts = useGet<ExtensionInfo[]>("/extensions");
  const installed = (exts.data ?? []).filter((x) => x.installed && x.configInterface);
  // A desktop never shows an empty right side: /settings is General there.
  const effective: SettingsPage | "" = page === "" && !phone ? "general" : page;
  useEffect(() => {
    document.getElementById("settings-content")?.scrollTo?.({ top: 0 });
  }, [effective, extensionInterface.value]);

  const nav = (
    <nav class="snav" aria-label={t("set.nav")}>
      <ul>
        {NAV.map((n) => (
          <li key={n.page}>
            <NavLink page={n.page} current={effective === n.page}>
              <span class="snav-label">{t(n.label)}</span>
              {phone ? <span class="snav-hint">{t(n.hint)}</span> : null}
              {phone ? <Icon name="right" size={14} /> : null}
            </NavLink>
            {n.page === "extensions" && installed.length > 0 ? (
              <ul class="snav-sub" aria-label={t("set.extensionPages")}>
                {installed.map((x) => (
                  <li key={x.id}>
                    <NavLink page="extension" iface={x.configInterface} sub current={effective === "extension" && extensionInterface.value === x.configInterface}>
                      <span class="snav-label" dir="auto">{x.name}</span>
                      {phone ? <Icon name="right" size={14} /> : null}
                    </NavLink>
                  </li>
                ))}
              </ul>
            ) : null}
          </li>
        ))}
      </ul>
    </nav>
  );

  if (phone && effective === "") return <div class="settings"><div class="settings-home"><h1 class="page-title">{t("tab.settings")}</h1>{nav}</div></div>;

  let body;
  switch (effective) {
    case "general": body = <GeneralPage />; break;
    case "accounts": body = <AccountsPage />; break;
    case "basicauth": body = <BasicAuthPage />; break;
    case "plugins": body = <PluginsPage />; break;
    case "extensions": body = <ExtensionsPage reloadNav={exts.reload} />; break;
    case "extension": body = <ExtensionConfigPage iface={extensionInterface.value} name={installed.find((x) => x.configInterface === extensionInterface.value)?.name} />; break;
    case "expert": body = <ExpertPage />; break;
    case "system": body = <SystemPage />; break;
    case "account": body = <AccountPage />; break;
    case "diagnostics": body = <DiagnosticsPage />; break;
    default: body = <GeneralPage />;
  }
  return (
    <div class="settings">
      {phone ? null : nav}
      <div class={`settings-content${["expert", "extension", "plugins"].includes(effective) ? " fill" : ""}`} id="settings-content">
        {phone ? (
          <button type="button" class="btn back-btn" onClick={() => navigateSettings("")}>
            <Icon name="back" size={14} />
            {t("tab.settings")}
          </button>
        ) : null}
        {body}
      </div>
    </div>
  );
}
