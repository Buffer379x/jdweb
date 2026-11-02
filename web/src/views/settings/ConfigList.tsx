// A long list of settings with a filter: only the visible rows exist in the page. The filter is client
// side over a precomputed line per entry (under a millisecond for 2,200 entries).

import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { Icon } from "../../components/Icon.tsx";
import { VirtualList } from "../../components/VirtualList.tsx";
import { filterIndexes, searchLine, type ConfigEntry } from "../../lib/editors.ts";
import { plural, t } from "../../i18n/index.ts";
import { announce } from "../../store/announce.ts";
import { compact } from "../../store/viewport.ts";
import { ConfigRow, RowStates, type Api } from "./ConfigRow.tsx";

export const CFG_ROW = 88;
export const CFG_ROW_COMPACT = 212;

export function ConfigList({ entries, api, label, empty }: { entries: ConfigEntry[]; api: Api; label: string; empty?: string }) {
  const [query, setQuery] = useState("");
  const lines = useMemo(() => entries.map(searchLine), [entries]);
  const states = useMemo(() => new RowStates(), [entries]);
  const shownIdx = useMemo(() => filterIndexes(lines, query), [lines, query]);
  const input = useRef<HTMLInputElement>(null);
  const countText = query ? t("cfg.matches", { n: shownIdx.length, total: entries.length }) : plural("cfg.count", entries.length);
  // The count of matches is told once typing pauses, not at every key and not when the page opens.
  const firstCount = useRef(true);
  useEffect(() => {
    if (firstCount.current) { firstCount.current = false; return; }
    const timer = setTimeout(() => announce(countText), 700);
    return () => clearTimeout(timer);
  }, [countText]);
  const rowH = compact.value ? CFG_ROW_COMPACT : CFG_ROW;
  return (
    <div class="cfg-list">
      <div class="cfg-filter">
        <label class="filter">
          <Icon name="search" size={14} />
          <input
            ref={input}
            type="search"
            class="input"
            value={query}
            placeholder={t("cfg.filterPlaceholder")}
            aria-label={t("cfg.filter")}
            autocomplete="off"
            spellcheck={false}
            onInput={(e) => setQuery(e.currentTarget.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setQuery("");
            }}
          />
        </label>
        <span class="mono muted cfg-count">{countText}</span>
      </div>
      {shownIdx.length === 0 ? (
        <div class="state-box">
          <p>{entries.length === 0 ? (empty ?? t("cfg.none")) : t("cfg.noMatch")}</p>
          {query ? <button type="button" class="btn" onClick={() => { setQuery(""); input.current?.focus(); }}>{t("cfg.clearFilter")}</button> : null}
        </div>
      ) : (
        <VirtualList
          label={label}
          count={shownIdx.length}
          rowHeight={rowH}
          class="cfg-vlist"
          renderRow={(i) => {
            const e = entries[shownIdx[i]!]!;
            return <ConfigRow key={`${e.storage}|${e.interface}|${e.key}`} entry={e} api={api} state={states.of(e, api)} index={i} count={shownIdx.length} />;
          }}
        />
      )}
    </div>
  );
}
