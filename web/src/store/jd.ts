// Actions on JDownloader as a whole: the switches of the header menu, the speed limit, restart.

import { post } from "../api/client.ts";
import { t } from "../i18n/index.ts";
import type { Status } from "../api/types.ts";
import { jdRestarting, pushToast, pushToastForError, questionDialogOpen, status } from "./app.ts";
import { refreshStatus } from "./live.ts";

/** What JDownloader reports lags behind a click by a poll or two: show the new state at once, the next status confirms it. */
function showToggles(patch: Partial<Status["toggles"]>): void {
  const cur = status.peek();
  if (cur) status.value = { ...cur, toggles: { ...cur.toggles, ...patch } };
}

const TOGGLE_FIELD: Record<ToggleName, keyof Status["toggles"]> = { premium: "premium", clipboard: "clipboard", reconnect: "reconnect", "stop-after": "stopAfter" };

export type ToggleName = "premium" | "clipboard" | "reconnect" | "stop-after";

/** Sets a switch. `enabled` is sent, so a browser with an old view cannot flip it the wrong way. */
export async function setToggle(name: ToggleName, enabled: boolean): Promise<void> {
  try {
    const r = await post<{ enabled?: boolean; pending?: boolean } | undefined>(`/toggles/${name}`, { enabled });
    if (r && r.pending) {
      // Switching premium off: JDownloader asks for confirmation and does not answer until somebody does.
      pushToast("info", t("toggle.pending"));
      questionDialogOpen.value = true;
    } else showToggles({ [TOGGLE_FIELD[name]]: r?.enabled ?? enabled });
  } catch (e) {
    pushToastForError(e);
  }
  setTimeout(() => void refreshStatus(), 3000);
}

export async function setSpeedLimit(enabled: boolean, bytesPerSecond: number): Promise<boolean> {
  try {
    await post("/speedlimit", { enabled, bytesPerSecond });
    showToggles(bytesPerSecond > 0 ? { limit: enabled, limitBytes: bytesPerSecond } : { limit: enabled });
    setTimeout(() => void refreshStatus(), 3000);
    return true;
  } catch (e) {
    pushToastForError(e);
    return false;
  }
}

export async function reconnectNow(): Promise<void> {
  try {
    await post("/reconnect");
    pushToast("info", t("toggle.reconnectStarted"));
  } catch (e) {
    pushToastForError(e);
  }
}

/** Restart or exit: JDownloader is gone a moment later and comes back by itself (restart) or stays away (exit). */
export async function systemAction(action: "restart" | "exit" | "shutdown" | "hibernate" | "standby"): Promise<void> {
  try {
    await post(`/system/${action}`, action === "shutdown" ? {} : undefined);
    if (action === "restart") {
      jdRestarting.value = true;
      setTimeout(() => (jdRestarting.value = false), 180_000);
    }
    pushToast("info", t(`system.sent.${action}` as never));
  } catch (e) {
    pushToastForError(e);
  }
}
