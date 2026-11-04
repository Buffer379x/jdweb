// The frame around the lists: control desk, tabs, tool row, footer, banner.

import { useRef } from "preact/hooks";
import { post } from "../api/client.ts";
import { Icon } from "../components/Icon.tsx";
import { Lamp } from "../components/Lamp.tsx";
import { Menu, type MenuItem } from "../components/Menu.tsx";
import { formatBytes, formatEta, formatSpeed } from "../lib/format.ts";
import { lang, langChoice, plural, t, type Key } from "../i18n/index.ts";
import {
  captchaDialogOpen, chooseLang, chooseTheme, helpOpen, live, logout, navigate, pushToastForError, questionDialogOpen,
  route, session, status, themeChoice, webReachable, type Route,
} from "../store/app.ts";
import type { AnyStore } from "../store/target.ts";
import { signal } from "@preact/signals";
import { clearGrabber, runAction, toDownloads } from "./actions.tsx";
import { openActionMenu } from "./ListView.tsx";
import { openAddLinks } from "./AddLinks.tsx";
import { openCleanup } from "./dialogs.tsx";
import { openDetailsDialog } from "./Details.tsx";
import { prefs, updatePrefs } from "../store/app.ts";
import { wide } from "../store/viewport.ts";
import { setToggle, reconnectNow, setSpeedLimit } from "../store/jd.ts";
import { openSpeedLimit } from "./SpeedLimitDialog.tsx";
import { ExtractionIndicator } from "./Extraction.tsx";
import { jdRestarting, navigateSettings } from "../store/app.ts";

async function control(action: "start" | "stop" | "pause" | "resume"): Promise<void> {
  try {
    await post(`/controller/${action}`);
  } catch (e) {
    pushToastForError(e);
  }
}

export function ControlDesk() {
  const s = status.value;
  const c = s?.controller ?? "IDLE";
  const on = live.value;
  const startOn = c === "RUNNING";
  const pauseOn = c === "PAUSE";
  const stopOn = c === "IDLE" || c === "STOPPING";
  return (
    <div class="ctl" role="group" aria-label={t("ctl.group")}>
      <button type="button" class={startOn ? "on" : ""} aria-pressed={startOn} disabled={!on} aria-label={t("ctl.start")} title={`${t("ctl.start")} (Shift+S)`} onClick={() => control(c === "PAUSE" ? "resume" : "start")}>
        <Icon name="play" />
      </button>
      <button type="button" class={pauseOn ? "on" : ""} aria-pressed={pauseOn} disabled={!on} aria-label={t("ctl.pause")} title={`${t("ctl.pause")} (Shift+P)`} onClick={() => control(c === "PAUSE" ? "resume" : "pause")}>
        <Icon name="pause" />
      </button>
      <button type="button" class={stopOn ? "on" : ""} aria-pressed={stopOn} disabled={!on} aria-label={t("ctl.stop")} title={`${t("ctl.stop")} (Shift+S)`} onClick={() => control("stop")}>
        <Icon name="stop" />
      </button>
    </div>
  );
}

export function SpeedBlock() {
  const s = status.value;
  const isLive = live.value;
  const c = isLive ? (s?.controller ?? "IDLE") : "IDLE";
  const word = !isLive ? t("conn.offlineShort") : t(`ctl.state.${c}` as Key);
  const eta = isLive && c === "RUNNING" ? s?.downloads.eta : null;
  return (
    <div class="speed" aria-label={t("ctl.speed")}>
      <b class="mono">{formatSpeed(isLive ? (s?.speed ?? 0) : 0, lang.value)}</b>
      <small class="mono">
        {word}
        {eta != null ? ` · ${t("ctl.rest")} ${formatEta(eta)}` : ""}
      </small>
    </div>
  );
}

export function Tabs() {
  const s = status.value;
  const r = route.value;
  const tab = (id: Route, label: Key, count?: number) => (
    <a
      key={id}
      href={`/${id}`}
      class={r === id ? "on" : ""}
      aria-current={r === id ? "page" : undefined}
      onClick={(e) => {
        if (e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        navigate(id);
      }}
    >
      <span>{t(label)}</span>
      {count !== undefined ? <em class="mono">{count}</em> : null}
    </a>
  );
  return (
    <nav class="tabs" aria-label={t("nav.label")}>
      {tab("downloads", "tab.downloads", s?.downloads.links)}
      {tab("linkgrabber", "tab.linkgrabber", s?.linkgrabber.links)}
      {tab("settings", "tab.settings")}
    </nav>
  );
}

/** The indicators for what waits for you: captchas and questions of JDownloader. */
export function Waiting() {
  const s = status.value;
  if (!s) return null;
  return (
    <>
      <span class="visually-hidden" role="status" aria-live="polite">
        {s.captchas > 0 ? plural("cap.waiting", s.captchas) : ""} {s.dialogs > 0 ? plural("dlg.waiting", s.dialogs) : ""}
      </span>
      {s.captchas > 0 ? (
        <button type="button" class="cap" aria-label={plural("cap.waiting", s.captchas)} onClick={() => (captchaDialogOpen.value = true)}>
          <Lamp tone="accent" pulse />
          <span class="cap-n">{s.captchas}</span>
          <span class="cap-t">{plural("cap.waiting", s.captchas)}</span>
        </button>
      ) : null}
      {s.dialogs > 0 ? (
        <button type="button" class="cap" aria-label={plural("dlg.waiting", s.dialogs)} onClick={() => (questionDialogOpen.value = true)}>
          <Lamp tone="accent" />
          <span class="cap-n">{s.dialogs}</span>
          <span class="cap-t">{plural("dlg.waiting", s.dialogs)}</span>
        </button>
      ) : null}
    </>
  );
}

const userMenu = signal<DOMRect | null>(null);

export function UserMenu() {
  const btn = useRef<HTMLButtonElement>(null);
  const tg = status.value?.toggles;
  const on = live.value;
  const items: MenuItem[] = [
    { id: "premium", label: t("toggle.premium"), checked: !!tg?.premium, disabled: !on, onSelect: () => void setToggle("premium", !tg?.premium) },
    { id: "clipboard", label: t("toggle.clipboard"), checked: !!tg?.clipboard, disabled: !on, onSelect: () => void setToggle("clipboard", !tg?.clipboard) },
    { id: "reconnect", label: t("toggle.reconnect"), checked: !!tg?.reconnect, disabled: !on, onSelect: () => void setToggle("reconnect", !tg?.reconnect) },
    { id: "stopAfter", label: t("toggle.stopAfter"), checked: !!tg?.stopAfter, disabled: !on, onSelect: () => void setToggle("stop-after", !tg?.stopAfter) },
    {
      id: "limit", label: t("toggle.limit"), checked: !!tg?.limit, disabled: !on,
      onSelect: () => (tg?.limit ? void setSpeedLimit(false, 0) : tg && tg.limitBytes > 0 ? void setSpeedLimit(true, 0) : openSpeedLimit()),
    },
    { id: "limitEdit", label: t("toggle.limitEdit"), disabled: !on, onSelect: openSpeedLimit },
    { id: "reconnectNow", label: t("toggle.reconnectNow"), disabled: !on, onSelect: () => void reconnectNow() },
    { id: "settings", label: t("tab.settings"), hint: "3", separatorBefore: true, onSelect: () => navigate("settings") },
    {
      id: "theme", separatorBefore: true, label: t("menu.theme"),
      children: (["system", "dark", "light"] as const).map((c) => ({ id: `theme-${c}`, label: t(`theme.${c}` as Key), checked: themeChoice.value === c, onSelect: () => chooseTheme(c) })),
    },
    {
      id: "lang", label: t("menu.language"),
      children: (["auto", "de", "en"] as const).map((c) => ({ id: `lang-${c}`, label: c === "auto" ? t("lang.auto") : c === "de" ? "Deutsch" : "English", checked: langChoice.value === c, onSelect: () => chooseLang(c) })),
    },
    { id: "keys", label: t("menu.keys"), hint: "?", onSelect: () => (helpOpen.value = true) },
    { id: "logout", label: t("menu.logout", { user: session.value?.username ?? "" }), separatorBefore: true, onSelect: () => void logout() },
  ];
  return (
    <>
      <button
        ref={btn}
        type="button"
        class="usr"
        aria-haspopup="menu"
        aria-label={t("menu.user")}
        title={session.value?.username}
        onClick={() => (userMenu.value = btn.current!.getBoundingClientRect())}
      >
        <Icon name="menu" />
      </button>
      {userMenu.value ? <Menu items={items} at={userMenu.value} title={t("menu.user")} onClose={() => (userMenu.value = null)} /> : null}
    </>
  );
}

export function Banner() {
  const s = status.value;
  if (!webReachable.value) {
    return (
      <div class="banner err" role="alert">
        <Lamp tone="err" />
        <div><b>{t("banner.web")}</b> <span>{t("banner.webDetail")}</span></div>
      </div>
    );
  }
  if (!s || s.connection === "connected") return null;
  if (s.connection === "syncing") {
    return (
      <div class="banner info" role="status">
        <Lamp tone="info" />
        <div><b>{t("conn.syncing")}</b></div>
      </div>
    );
  }
  const mis = s.connection === "misconfigured";
  const restarting = jdRestarting.value && !mis;
  return (
    <div class={`banner ${restarting ? "info" : "err"}`} role={restarting ? "status" : "alert"}>
      <Lamp tone={restarting ? "info" : "err"} />
      <div>
        <b>{restarting ? t("banner.restarting") : mis ? t("banner.misconfigured") : t("banner.offline")}</b>{" "}
        {restarting ? (
          <span>{t("banner.restartingHint")}</span>
        ) : (
          <>
            <span dir="auto">{s.connectionDetail || (mis ? t("banner.misconfiguredHint") : t("banner.offlineHint"))}</span>
            {!mis && s.connectionDetail ? <span> {t("banner.offlineHint")}</span> : null}
            {mis ? <span class="banner-more"> {t("banner.settingsHint")}</span> : null}{" "}
            {mis || s.connection === "offline" ? (
              <a class="banner-link" href="/settings/diagnostics" onClick={(e) => { e.preventDefault(); navigateSettings("diagnostics"); }}>{t("banner.toDiagnostics")}</a>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
}

export function Toolbar({ store }: { store: AnyStore }) {
  const isDl = store.name === "downloads";
  const on = live.value;
  const actionsBtn = useRef<HTMLButtonElement>(null);
  const n = store.selection.value.size;
  const s = status.value;
  return (
    <div class="tools" role="toolbar" aria-label={t("tools.label")}>
      <label class="filter">
        <Icon name="search" size={14} />
        <input
          id="filter"
          type="search"
          class="input"
          value={store.filter.value}
          placeholder={isDl ? t("filter.placeholderDl") : t("filter.placeholderLg")}
          aria-label={t("filter.label")}
          dir="auto"
          autocomplete="off"
          spellcheck={false}
          onInput={(e) => (store.filter.value = e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              store.filter.value = "";
              e.currentTarget.blur();
            }
          }}
        />
      </label>
      {n > 0 ? <span class="sel-count mono" aria-live="polite">{plural("list.selected", n)}</span> : null}
      <button ref={actionsBtn} type="button" class="btn menu-btn" disabled={!on} aria-haspopup="menu" onClick={() => openActionMenu(actionsBtn.current!.getBoundingClientRect())}>
        {t("tools.actions")}
      </button>
      {!isDl && s?.linkgrabber.crawling ? (
        <button type="button" class="btn" disabled={!on} onClick={() => void runAction("linkgrabber", "abort", {}, store)}>{t("tools.abort")}</button>
      ) : null}
      {!isDl ? <button type="button" class="btn" disabled={!on || store.total.value === 0} onClick={() => clearGrabber(store)}>{t("act.clear")}</button> : null}
      <button
        type="button"
        class="btn"
        aria-pressed={wide.value ? !!prefs.value.details : undefined}
        onClick={() => (wide.value ? updatePrefs({ details: !prefs.value.details }) : openDetailsDialog(store))}
      >
        <Icon name="details" size={14} />
        {t("tools.details")}
      </button>
      <button type="button" class="btn" disabled={!on} onClick={() => openCleanup(store)}>{t("tools.cleanup")}</button>
      <button type="button" class="btn" disabled={!on} onClick={openAddLinks}>
        <Icon name="plus" size={13} />
        {t("add.open")}
      </button>
      {!isDl ? (
        <button type="button" class="btn pri" disabled={!on || store.total.value === 0} onClick={() => toDownloads(store, false)}>
          {n > 0 ? t("tools.toDownloads") : t("tools.allToDownloads")}
        </button>
      ) : null}
    </div>
  );
}

/** Action bar for touch screens: the same menu as the context click. */
export function ActionBar({ store }: { store: AnyStore }) {
  const n = store.selection.value.size;
  if (n === 0) return null;
  return (
    <div class="actionbar" role="toolbar" aria-label={t("tools.label")}>
      <span class="mono">{plural("list.selected", n)}</span>
      <button type="button" class="btn pri" onClick={(e) => openActionMenu(e.currentTarget.getBoundingClientRect())}>{t("tools.actions")}</button>
      <button type="button" class="btn" onClick={() => (store.selection.value = new Set())}>{t("common.clear")}</button>
    </div>
  );
}

export function Footbar() {
  const s = status.value;
  const r = route.value;
  const lg = r === "linkgrabber";
  const l = lang.value;
  const conn = s?.connection ?? "offline";
  const tone = conn === "connected" ? "run" : conn === "syncing" ? "info" : "err";
  return (
    <footer class="foot">
      {s && !lg ? (
        <>
          <span>{t("foot.packages")} <b>{s.downloads.packages}</b></span>
          <span>{t("foot.links")} <b>{s.downloads.links}</b></span>
          <span>{t("foot.total")} <b>{formatBytes(s.downloads.bytesTotal, l)}</b></span>
          <span class="opt">{t("foot.loaded")} <b>{formatBytes(s.downloads.bytesLoaded, l)}</b></span>
          <span class="opt">{t("foot.running")} <b>{live.value ? s.downloads.running : 0}</b></span>
          {live.value && s.downloads.eta != null && s.downloads.running > 0 ? <span class="opt">{t("ctl.rest")} <b>{formatEta(s.downloads.eta)}</b></span> : null}
        </>
      ) : null}
      {s && lg ? (
        <>
          <span>{t("foot.packages")} <b>{s.linkgrabber.packages}</b></span>
          <span>{t("foot.links")} <b>{s.linkgrabber.links}</b></span>
          <span>{t("foot.total")} <b>{formatBytes(s.linkgrabber.bytesTotal, l)}</b></span>
        </>
      ) : null}
      <span class="r">
        {s ? (
          <span class="st">
            <Lamp tone={s.linkgrabber.crawling ? "info" : "muted"} />
            {s.linkgrabber.crawling ? t("foot.crawling") : t("foot.crawlIdle")}
          </span>
        ) : null}
        {s ? <ExtractionIndicator /> : null}
        <span class="st">
          <Lamp tone={tone} />
          {t(`conn.${conn}` as Key)}
        </span>
      </span>
    </footer>
  );
}

