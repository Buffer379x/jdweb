import { useMemo, useState } from "preact/hooks";
import { HosterIcon } from "../../components/HosterIcon.tsx";
import { LoadState } from "../../components/LoadState.tsx";
import { Icon } from "../../components/Icon.tsx";
import { useGet } from "../../hooks/useLoad.ts";
import type { ConfigEntry } from "../../lib/editors.ts";
import { plural, t } from "../../i18n/index.ts";
import { compact } from "../../store/viewport.ts";
import { ConfigList } from "./ConfigList.tsx";
import { PageHead } from "./common.tsx";

interface PluginInfo {
  displayName: string;
  className: string;
  pattern: string;
  version: string;
}

interface Data {
  plugins: PluginInfo[];
  entries: ConfigEntry[];
}

export function PluginsPage() {
  const plugins = useGet<PluginInfo[]>("/plugins", ((r: { plugins: PluginInfo[] }) => r.plugins ?? []) as never);
  const cfg = useGet<ConfigEntry[]>("/plugins/settings", ((r: { entries: ConfigEntry[] }) => r.entries ?? []) as never);
  const loading = { data: plugins.data && cfg.data ? { plugins: plugins.data, entries: cfg.data } : undefined, error: plugins.error ?? cfg.error, loading: plugins.loading || cfg.loading, reload: () => { plugins.reload(); cfg.reload(); }, setData: () => {} };
  return (
    <>
      <PageHead title={t("set.plugins")} intro={t("plugins.intro")} />
      <LoadState load={loading}>{(d: Data) => <PluginBrowser data={d} />}</LoadState>
    </>
  );
}

function PluginBrowser({ data }: { data: Data }) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const byPlugin = useMemo(() => {
    const m = new Map<string, ConfigEntry[]>();
    for (const e of data.entries) {
      const l = m.get(e.storage);
      if (l) l.push(e);
      else m.set(e.storage, [e]);
    }
    return m;
  }, [data]);
  // Plugins with settings first, the rest after them.
  const list = useMemo(() => {
    const q = query.trim().toLowerCase();
    return data.plugins
      .filter((p) => !q || p.displayName.toLowerCase().includes(q) || p.className.toLowerCase().includes(q))
      .sort((a, b) => (byPlugin.has(b.displayName) ? 1 : 0) - (byPlugin.has(a.displayName) ? 1 : 0) || a.displayName.localeCompare(b.displayName));
  }, [data, query, byPlugin]);
  const sel = selected ? data.plugins.find((p) => p.displayName === selected) : null;
  const entries = selected ? (byPlugin.get(selected) ?? []) : [];
  const phone = compact.value;

  const picker = (
    <div class="plugin-pick">
      <label class="filter">
        <Icon name="search" size={14} />
        <input type="search" class="input" value={query} placeholder={t("plugins.filterPlaceholder")} aria-label={t("plugins.filter")} autocomplete="off" spellcheck={false} onInput={(e) => setQuery(e.currentTarget.value)} />
      </label>
      <p class="muted small" role="status" aria-live="polite">{plural("plugins.count", list.length)}</p>
      <ul class="plugin-list">
        {list.map((p) => {
          const n = byPlugin.get(p.displayName)?.length ?? 0;
          return (
            <li key={p.className + p.displayName}>
              <button type="button" class={`plugin-item${selected === p.displayName ? " on" : ""}`} aria-pressed={selected === p.displayName} onClick={() => setSelected(p.displayName)}>
                <HosterIcon host={p.displayName} />
                <span class="plugin-name" dir="auto">{p.displayName}</span>
                <span class="mono muted small">{n > 0 ? plural("plugins.settings", n) : ""}</span>
              </button>
            </li>
          );
        })}
        {list.length === 0 ? <li class="muted small pad">{t("plugins.none")}</li> : null}
      </ul>
    </div>
  );
  const detail = sel ? (
    <div class="plugin-detail">
      {phone ? <button type="button" class="btn back-btn" onClick={() => setSelected(null)}><Icon name="back" size={14} />{t("set.plugins")}</button> : null}
      <h2 class="sect-title" dir="auto">{sel.displayName}</h2>
      <p class="muted small mono">{sel.className} · {t("plugins.version")} {sel.version}</p>
      <ConfigList entries={entries} api="plugins" label={sel.displayName} empty={t("plugins.noSettings")} />
    </div>
  ) : (
    <div class="state-box"><p>{t("plugins.choose")}</p></div>
  );
  if (phone) return selected ? detail : picker;
  return <div class="plugin-split">{picker}{detail}</div>;
}
