// JDownloader's own switch LinkgrabberAutoStartEnabled, as the settings page uses it.

import type { ConfigEntry } from "./editors.ts";

export const LINKGRABBER = "org.jdownloader.gui.views.linkgrabber.addlinksdialog.LinkgrabberSettings";
export const AUTO_START = "LinkgrabberAutoStartEnabled";

/** The value of the switch in a config answer, or null when JDownloader does not list it. */
export function autoStartFrom(r: { entries?: ConfigEntry[] }): boolean | null {
  const e = (r.entries ?? []).find((x) => x.key === AUTO_START && x.storage === "");
  return e ? e.value === true : null;
}

/**
 * Sets the switch. The shown value changes only after JDownloader accepted it; a refusal goes to `fail` and the
 * switch stays where it was. Returns whether it worked.
 */
export async function applyAutoStart(
  put: (path: string, body: unknown) => Promise<unknown>,
  show: (v: boolean) => void,
  fail: (err: unknown) => void,
  v: boolean,
): Promise<boolean> {
  try {
    await put("/config", { interface: LINKGRABBER, storage: "", key: AUTO_START, value: v });
  } catch (err) {
    fail(err);
    return false;
  }
  show(v);
  return true;
}
