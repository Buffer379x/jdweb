import { useRef } from "preact/hooks";
import { signal } from "@preact/signals";
import { Icon } from "../components/Icon.tsx";
import { Menu, type MenuItem } from "../components/Menu.tsx";
import { langChoice, t, type Key } from "../i18n/index.ts";
import { chooseLang, chooseTheme, themeChoice } from "../store/app.ts";

const open = signal<DOMRect | null>(null);

/** Theme and language for the pages before the sign-in. */
export function UserMenuLite() {
  const btn = useRef<HTMLButtonElement>(null);
  const items: MenuItem[] = [
    { id: "theme", label: t("menu.theme"), children: (["system", "dark", "light"] as const).map((c) => ({ id: `th-${c}`, label: t(`theme.${c}` as Key), checked: themeChoice.value === c, onSelect: () => chooseTheme(c) })) },
    { id: "lang", label: t("menu.language"), children: (["auto", "de", "en"] as const).map((c) => ({ id: `lg-${c}`, label: c === "auto" ? t("lang.auto") : c === "de" ? "Deutsch" : "English", checked: langChoice.value === c, onSelect: () => chooseLang(c) })) },
  ];
  return (
    <>
      <button ref={btn} type="button" class="usr auth-usr" aria-haspopup="menu" aria-label={t("menu.user")} onClick={() => (open.value = btn.current!.getBoundingClientRect())}>
        <Icon name="menu" />
      </button>
      {open.value ? <Menu items={items} at={open.value} onClose={() => (open.value = null)} /> : null}
    </>
  );
}
