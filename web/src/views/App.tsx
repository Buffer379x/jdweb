import { effect } from "@preact/signals";
import { useEffect } from "preact/hooks";
import { Toasts } from "../components/Toasts.tsx";
import { formatEta, formatPercent, formatSpeed, percent } from "../lib/format.ts";
import { lang, t } from "../i18n/index.ts";
import { captchaDialogOpen, checkSession, extractionOpen, helpOpen, jdRestarting, live, prefs, pushToast, questionDialogOpen, route, session, setupRequired, status, navigate, pushToastForError } from "../store/app.ts";
import { dialogStack } from "../store/dialogs.ts";
import { stores } from "../store/stores.ts";
import { ensureList, startLive } from "../store/live.ts";
import { post } from "../api/client.ts";
import { LoginView, SetupView } from "./Auth.tsx";
import { ActionBar, Banner, ControlDesk, Footbar, SpeedBlock, Tabs, Toolbar, UserMenu, Waiting } from "./Chrome.tsx";
import { ListView } from "./ListView.tsx";
import { SettingsView } from "./settings/Layout.tsx";
import { CaptchaDialog } from "./Captcha.tsx";
import { QuestionsDialog } from "./Questions.tsx";
import { useFirstRun } from "./FirstRun.tsx";
import { startQueuePolling } from "../store/extraction.ts";
import { HelpDialog } from "./Help.tsx";
import { openAddLinks } from "./AddLinks.tsx";
import { DetailsPanel } from "./Details.tsx";
import { wide } from "../store/viewport.ts";
import { removeSelected } from "./actions.tsx";

function inField(el: EventTarget | null): boolean {
  const e = el as HTMLElement | null;
  return !!e && (e.tagName === "INPUT" || e.tagName === "TEXTAREA" || e.tagName === "SELECT" || e.isContentEditable);
}

async function ctl(action: string): Promise<void> {
  try {
    await post(`/controller/${action}`);
  } catch (e) {
    pushToastForError(e);
  }
}

/** Keys of Concept 11.7 that work everywhere. The keys of the list are on the list. */
function onGlobalKey(e: KeyboardEvent): void {
  if (e.defaultPrevented || dialogStack.value.length || inField(e.target)) return;
  if (e.ctrlKey || e.metaKey || e.altKey) return;
  // A menu or the key overview that is open keeps the keys to itself.
  if (helpOpen.value || document.querySelector(".menu-layer, .scrim")) return;
  const s = status.value;
  if (e.key === "?") {
    helpOpen.value = true;
  } else if (e.shiftKey && (e.key === "S" || e.key === "P")) {
    if (!live.value) return;
    const c = s?.controller ?? "IDLE";
    e.preventDefault();
    if (e.key === "S") void ctl(c === "RUNNING" || c === "PAUSE" ? "stop" : "start");
    else void ctl(c === "PAUSE" ? "resume" : "pause");
  } else if (!e.shiftKey) {
    if (e.key === "1") navigate("downloads");
    else if (e.key === "2") navigate("linkgrabber");
    else if (e.key === "3") navigate("settings");
    else if (e.key === "/") {
      const f = document.getElementById("filter");
      if (f) {
        e.preventDefault();
        f.focus();
      }
    } else if ((e.key === "a" || e.key === "A") && live.value) {
      e.preventDefault(); // or the letter would be typed into the field that the dialog focuses
      openAddLinks();
    } else if (e.key === "Delete" && live.value && route.value !== "settings") {
      removeSelected(route.value === "downloads" ? stores.downloads : stores.linkgrabber);
    }
  }
}

/** The live progress in the title of the tab: "56 % · 12,4 MB/s". */
effect(() => {
  const s = status.value;
  const l = lang.value;
  let title = "JDWeb";
  if (session.value && s) {
    const parts: string[] = [];
    if (live.value && s.downloads.running > 0) {
      parts.push(formatPercent(percent(s.downloads.bytesLoaded, s.downloads.bytesTotal), l), formatSpeed(s.speed, l));
      if (s.downloads.eta != null) parts.push(formatEta(s.downloads.eta));
    }
    title = (s.captchas > 0 ? `(${s.captchas}) ` : "") + (parts.length ? `${parts.join(" · ")} – JDWeb` : "JDWeb");
  }
  document.title = title;
});

// JDownloader came back after a restart that was asked for here.
let sawDown = false;
effect(() => {
  const c = status.value?.connection;
  if (!jdRestarting.value) {
    sawDown = false;
    return;
  }
  if (c && c !== "connected") sawDown = true;
  else if (c === "connected" && sawDown) {
    sawDown = false;
    jdRestarting.value = false;
    pushToast("info", t("sys.backAgain"));
  }
});

function Shell() {
  const r = route.value;
  useFirstRun();
  useEffect(() => startQueuePolling(() => extractionOpen.peek()), []);
  const store = r === "linkgrabber" ? stores.linkgrabber : stores.downloads;
  useEffect(() => {
    if (r !== "settings") ensureList(r);
  }, [r]);
  return (
    <div class="app">
      <header class="top">
        <div class="brand">
          <img src="/jdweb-mark.svg" alt="" width={30} height={30} />
          <span class="word">JD<span>Web</span></span>
        </div>
        <ControlDesk />
        <SpeedBlock />
        <Tabs />
        <Waiting />
        <UserMenu />
      </header>
      <Banner />
      {r === "settings" ? (
        <main class="main"><SettingsView /></main>
      ) : (
        <main class="main" id="main">
          <Toolbar store={store} />
          <div class="split">
            <ListView store={store} />
            {wide.value && prefs.value.details ? <DetailsPanel store={store} /> : null}
          </div>
          <ActionBar store={store} />
        </main>
      )}
      <Footbar />
    </div>
  );
}

export function App() {
  useEffect(() => {
    void checkSession();
    addEventListener("keydown", onGlobalKey);
    return () => removeEventListener("keydown", onGlobalKey);
  }, []);
  const sess = session.value;
  useEffect(() => {
    if (!sess) return;
    startLive();
  }, [!!sess]);
  let body;
  if (sess === undefined || (sess === null && setupRequired.value === undefined)) body = <div class="boot" role="status">{t("boot.loading")}</div>;
  else if (sess === null) body = setupRequired.value ? <SetupView /> : <LoginView />;
  else body = <Shell />;
  return (
    <>
      {body}
      {session.value && captchaDialogOpen.value ? <CaptchaDialog /> : null}
      {session.value && questionDialogOpen.value ? <QuestionsDialog /> : null}
      {dialogStack.value.map((d) => <div key={d.id}>{d.render()}</div>)}
      {helpOpen.value ? <HelpDialog onClose={() => (helpOpen.value = false)} /> : null}
      <Toasts />
    </>
  );
}
