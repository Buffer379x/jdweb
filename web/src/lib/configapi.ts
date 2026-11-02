// The routes a setting row talks to. General settings and plugin settings have their own routes for every
// call, reveal included: JDownloader cannot name a plugin storage in config/get.

import { post } from "../api/client.ts";
import type { EditorKind } from "./editors.ts";
import { valueToText } from "./editors.ts";

export type Api = "config" | "plugins";

export function configPaths(api: Api): { set: string; reset: string; reveal: string } {
  return api === "config"
    ? { set: "/config", reset: "/config/reset", reveal: "/config/reveal" }
    : { set: "/plugins", reset: "/plugins/reset", reveal: "/plugins/reveal" };
}

export interface SettingRef {
  interface: string;
  storage: string;
  key: string;
}

/** Asks for the real value of a secret and returns the text an editor starts with. */
export async function revealText(api: Api, kind: EditorKind, ref: SettingRef): Promise<string> {
  const r = await post<{ value: unknown }>(configPaths(api).reveal, { interface: ref.interface, storage: ref.storage, key: ref.key });
  return valueToText(kind, r.value);
}
