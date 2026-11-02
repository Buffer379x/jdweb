import type { ComponentChildren } from "preact";
import type { DlLink, DlPackage, LgLink, LgPackage } from "../api/types.ts";
import { HosterIcon } from "../components/HosterIcon.tsx";
import { Icon } from "../components/Icon.tsx";
import { Lamp } from "../components/Lamp.tsx";
import { Segbar } from "../components/Segbar.tsx";
import { formatBytes, formatEta, formatPercent, formatSpeed, percent } from "../lib/format.ts";
import { segments, summarize } from "../lib/segments.ts";
import { displayText } from "../lib/text.ts";
import { lang, plural, t } from "../i18n/index.ts";
import { live } from "../store/app.ts";
import { dlLinkStatus, dlPackageStatus, lgLinkStatus, lgPackageStatus, type StateText } from "../store/linkinfo.ts";
import type { ListStore } from "../store/list.ts";

export interface FrameProps {
  rowKey: string;
  kind: "pkg" | "link";
  index: number;
  selected: boolean;
  focused: boolean;
  expanded?: boolean;
  drop?: "before" | "after" | "into";
  draggable: boolean;
  disabled?: boolean;
  children: ComponentChildren;
}

/** The row element with its roles; the cells are the children. */
export function Frame(p: FrameProps) {
  return (
    <div
      id={`r-${p.rowKey}`}
      class={`row ${p.kind}${p.selected ? " sel" : ""}${p.focused ? " focus" : ""}${p.disabled ? " off" : ""}${p.drop ? ` drop-${p.drop}` : ""}`}
      role="row"
      aria-rowindex={p.index + 2}
      aria-selected={p.selected}
      aria-expanded={p.kind === "pkg" ? !!p.expanded : undefined}
      aria-level={p.kind === "pkg" ? 1 : 2}
      data-key={p.rowKey}
      draggable={p.draggable}
    >
      <div class="cell c-check" role="gridcell">
        <span class={`chk${p.selected ? " on" : ""}`} data-check>
          {p.selected ? <Icon name="check" size={12} /> : null}
        </span>
      </div>
      {p.children}
    </div>
  );
}

function Status({ s }: { s: StateText }) {
  return (
    <span class="st" title={s.title}>
      <Lamp tone={s.tone} />
      <span class={s.mono ? "mono" : undefined} dir="auto">{s.text}</span>
    </span>
  );
}

function Hosts({ hosts }: { hosts: string[] }) {
  if (hosts.length === 0) return <span class="muted">{"–"}</span>;
  if (hosts.length === 1) return <HostCell host={hosts[0]!} />;
  return <span class="host many" title={hosts.join(", ")}>{t("host.many", { n: hosts.length })}</span>;
}

function HostCell({ host }: { host: string }) {
  return (
    <span class="host" title={host}>
      <HosterIcon host={host} />
      <span class="host-text" dir="auto">{host}</span>
    </span>
  );
}

function Name({ name, indent, twisty, expanded, count, priority }: { name: string; indent?: boolean; twisty?: boolean; expanded?: boolean; count?: number; priority?: string }) {
  return (
    <div class={`cell c-name${indent ? " indent" : ""}`} role={twisty ? "rowheader" : "gridcell"}>
      {twisty ? (
        <button type="button" class="twisty" data-toggle tabIndex={-1} aria-label={expanded ? t("row.collapse") : t("row.expand")}>
          <Icon name={expanded ? "down" : "right"} size={13} />
        </button>
      ) : null}
      <span class="name" title={displayText(name)} dir="auto">{displayText(name)}</span>
      {priority && priority !== "DEFAULT" ? <span class="prio" title={t("field.priority")}>{t(`prio.${priority}` as never)}</span> : null}
      {count !== undefined ? <span class="count mono">{count}</span> : null}
    </div>
  );
}

function SizeCell({ bytes }: { bytes: number }) {
  return <div class="cell c-size num" role="gridcell">{formatBytes(bytes, lang.value)}</div>;
}

// ---- downloads ---------------------------------------------------------------

export function DlPackageRow({ store, id, frame }: { store: ListStore<DlPackage, DlLink>; id: string; frame: Omit<FrameProps, "children"> }) {
  const p = store.pkg(id)?.value;
  const ver = store.childrenVersion(id).value; // follow the links of the package
  if (!p) return null;
  void ver;
  const isLive = live.value;
  // While JDownloader is away nothing runs, whatever the last state says.
  const kids = (store.model.children.get(id) ?? [])
    .map((k) => store.model.links.get(k)!)
    .filter(Boolean)
    .map((k) => (isLive || k.state !== "running" ? k : { ...k, state: "waiting" as const }));
  const sum = summarize(kids);
  const segs = segments(kids);
  const pct = percent(p.bytesLoaded, p.bytesTotal, p.finished || (sum.total > 0 && sum.finished === sum.total));
  const status = dlPackageStatus(p, sum, isLive);
  const label =
    t("seg.summary", { done: sum.finished, total: sum.total }) + (sum.errors ? `, ${plural("pkg.errors", sum.errors)}` : "");
  return (
    <Frame {...frame} disabled={!p.enabled}>
      <Name name={p.name} twisty expanded={frame.expanded} count={p.childCount} priority={p.priority} />
      <SizeCell bytes={p.bytesTotal} />
      <div class="cell c-host" role="gridcell"><Hosts hosts={p.hosts} /></div>
      <div class="cell c-status" role="gridcell"><Status s={{ ...status, text: pkgStatusText(status, p.speed, isLive) }} /></div>
      <div class="cell c-progress" role="gridcell" title={p.eta != null && p.running ? `${t("field.eta")} ${formatEta(p.eta)}` : undefined}>
        <Segbar segs={segs} label={label} />
        <span class="pct">{formatPercent(pct, lang.value)}</span>
      </div>
    </Frame>
  );
}

function pkgStatusText(s: StateText, speed: number, isLive: boolean): string {
  return s.tone === "run" && speed > 0 && isLive ? `${s.text} · ${formatSpeed(speed, lang.value)}` : s.text;
}

export function DlLinkRow({ store, id, frame }: { store: ListStore<DlPackage, DlLink>; id: string; frame: Omit<FrameProps, "children"> }) {
  const l = store.link(id)?.value;
  if (!l) return null;
  const pct = percent(l.bytesLoaded, l.bytesTotal, l.finished);
  const seg = segments([l]);
  const status = dlLinkStatus(l, live.value);
  return (
    <Frame {...frame} disabled={!l.enabled}>
      <Name name={l.name} indent priority={l.priority} />
      <SizeCell bytes={l.bytesTotal} />
      <div class="cell c-host" role="gridcell"><HostCell host={l.host} /></div>
      <div class="cell c-status" role="gridcell"><Status s={status} /></div>
      <div class="cell c-progress" role="gridcell" title={l.state === "running" && l.eta != null ? `${t("field.eta")} ${formatEta(l.eta)}` : undefined}>
        <Segbar segs={live.value || l.state !== "running" ? seg : segments([{ ...l, state: "waiting" }])} label={`${formatPercent(pct, lang.value)}, ${status.text}`} />
        <span class="pct">{formatPercent(pct, lang.value)}</span>
      </div>
    </Frame>
  );
}

// ---- linkgrabber ---------------------------------------------------------------

export function LgPackageRow({ store, id, frame }: { store: ListStore<LgPackage, LgLink>; id: string; frame: Omit<FrameProps, "children"> }) {
  const p = store.pkg(id)?.value;
  if (!p) return null;
  return (
    <Frame {...frame} disabled={!p.enabled}>
      <Name name={p.name} twisty expanded={frame.expanded} count={p.childCount} priority={p.priority} />
      <div class="cell c-path mono" role="gridcell" title={p.saveTo} dir="ltr"><span class="ellipsis">{p.saveTo}</span></div>
      <SizeCell bytes={p.bytesTotal} />
      <div class="cell c-host" role="gridcell"><Hosts hosts={p.hosts} /></div>
      <div class="cell c-status" role="gridcell"><Status s={lgPackageStatus(p)} /></div>
    </Frame>
  );
}

export function LgLinkRow({ store, id, frame }: { store: ListStore<LgPackage, LgLink>; id: string; frame: Omit<FrameProps, "children"> }) {
  const l = store.link(id)?.value;
  if (!l) return null;
  return (
    <Frame {...frame} disabled={!l.enabled}>
      <Name name={l.name} indent priority={l.priority} />
      <div class="cell c-path" role="gridcell" />
      <SizeCell bytes={l.bytesTotal} />
      <div class="cell c-host" role="gridcell"><HostCell host={l.host} /></div>
      <div class="cell c-status" role="gridcell"><Status s={lgLinkStatus(l)} /></div>
    </Frame>
  );
}

