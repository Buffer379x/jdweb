import { signal } from "@preact/signals";
import { de, type Key } from "./de.ts";
import { en } from "./en.ts";
import type { Lang } from "../lib/format.ts";

export type { Key } from "./de.ts";
export type { Lang } from "../lib/format.ts";

const dictionaries: Record<Lang, Record<Key, string>> = { de, en };

/** "auto" follows the browser language. */
export type LangChoice = Lang | "auto";

export function detectLang(languages: readonly string[] | undefined): Lang {
  for (const l of languages ?? []) {
    const base = l.toLowerCase().split("-")[0];
    if (base === "de") return "de";
    if (base === "en") return "en";
  }
  return "en";
}

export const langChoice = signal<LangChoice>("auto");
export const lang = signal<Lang>(detectLang(typeof navigator === "undefined" ? [] : navigator.languages ?? [navigator.language]));

export function setLangChoice(c: LangChoice): void {
  langChoice.value = c;
  lang.value = c === "auto" ? detectLang(navigator.languages ?? [navigator.language]) : c;
  document.documentElement.lang = lang.value;
}

export type Params = Record<string, string | number>;

export function format(text: string, params?: Params): string {
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (m, k: string) => (k in params ? String(params[k]) : m));
}

/** Translates a key. Reading `lang` makes the calling component follow language changes. */
export function t(key: Key, params?: Params): string {
  return format(dictionaries[lang.value][key], params);
}

type PluralBase<K> = K extends `${infer B}.one` ? B : never;
export type PluralKey = PluralBase<Key>;

/** Picks `<base>.one` for 1 and `<base>.other` otherwise; `{n}` is filled in. */
export function plural(base: PluralKey, n: number, params?: Params): string {
  const key = `${base}.${n === 1 ? "one" : "other"}` as Key;
  return format(dictionaries[lang.value][key], { n, ...params });
}
