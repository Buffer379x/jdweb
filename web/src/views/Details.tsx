import { Dialog } from "../components/Dialog.tsx";
import { Lamp } from "../components/Lamp.tsx";
import { formatBytes, formatDate, formatEta, formatPercent, formatSpeed, percent } from "../lib/format.ts";
import { displayText } from "../lib/text.ts";
import { lang, t } from "../i18n/index.ts";
import { live } from "../store/app.ts";
import { openDialog } from "../store/dialogs.ts";
import { dlLinkStatus, dlPackageStatus, lgLinkStatus, lgPackageStatus } from "../store/linkinfo.ts";
import { summarize } from "../lib/segments.ts";
import type { AnyStore } from "../store/target.ts";
import type { ComponentChildren } from "preact";
import type { LinkState } from "../api/types.ts";

function Item({ label, children, mono }: { label: string; children: ComponentChildren; mono?: boolean }) {
  return (
    <div class="d-item">
      <dt>{label}</dt>
      <dd class={mono ? "mono" : undefined} dir="auto">{children}</dd>
    </div>
  );
}

const none = "–";

/** What is known about the focused entry. Everything is text: names and paths from JDownloader are never markup. */
export function DetailsBody({ store }: { store: AnyStore }) {
  const key = store.focusKey.value ?? (store.selection.value.size === 1 ? [...store.selection.value][0]! : null);
  const isPkg = key?.charCodeAt(0) === 112;
  const id = key?.slice(1) ?? "";
  const e = key ? (isPkg ? store.pkg(id)?.value : store.link(id)?.value) : undefined;
  const l = lang.value;
  if (!e) return <p class="muted">{t("details.none")}</p>;
  const isDl = store.name === "downloads";
  const isLive = live.value;
  const status = isDl
    ? isPkg
      ? dlPackageStatus(
          e,
          summarize(
            (store.model.children.get(id) ?? [])
              .map((c: string) => store.model.links.get(c)!)
              .filter(Boolean)
              // While JDownloader is away nothing runs, whatever the last state says.
              .map((k: { state: LinkState }) => (isLive || k.state !== "running" ? k : { ...k, state: "waiting" as const })),
          ),
          isLive,
        )
      : dlLinkStatus(e, isLive)
    : isPkg
      ? lgPackageStatus(e)
      : lgLinkStatus(e);
  return (
    <dl class="d-list">
      <Item label={t("field.name")}>{displayText(e.name)}</Item>
      <Item label={t("col.status")}><span class="st" title={status.title}><Lamp tone={status.tone} />{status.text}</span>{status.title ? <span class="d-note" dir="auto">{status.title}</span> : null}</Item>
      {isDl ? (
        <>
          <Item label={t("col.size")} mono>{formatBytes(e.bytesTotal, l)}</Item>
          <Item label={t("col.progress")} mono>{formatPercent(percent(e.bytesLoaded, e.bytesTotal, e.finished), l)} ({formatBytes(e.bytesLoaded, l)})</Item>
          {e.running && isLive ? <Item label={t("col.speed")} mono>{formatSpeed(e.speed, l)} · {formatEta(e.eta)}</Item> : null}
        </>
      ) : (
        <Item label={t("col.size")} mono>{formatBytes(e.bytesTotal, l)}</Item>
      )}
      <Item label={t("col.host")}>{isPkg ? (e.hosts as string[]).join(", ") || none : e.host}</Item>
      <Item label={t("field.directory")} mono>{isPkg ? e.saveTo || none : store.model.pkgs.get(e.packageId)?.saveTo || none}</Item>
      {!isPkg ? <Item label={t("details.url")} mono>{e.url || none}</Item> : null}
      <Item label={t("field.priority")}>{t(`prio.${e.priority || "DEFAULT"}` as never)}</Item>
      <Item label={t("details.comment")}>{e.comment || none}</Item>
      <Item label={t("field.downloadPassword")}>{e.downloadPassword ? t("details.set") : none}</Item>
      {isDl && !isPkg && e.extractionStatus ? <Item label={t("details.extraction")}>{e.extractionStatus}</Item> : null}
      {isDl && !isPkg && e.addedDate ? <Item label={t("details.added")}>{formatDate(e.addedDate, l)}</Item> : null}
      {isDl && !isPkg && e.finishedDate ? <Item label={t("details.finished")}>{formatDate(e.finishedDate, l)}</Item> : null}
    </dl>
  );
}

export function DetailsPanel({ store }: { store: AnyStore }) {
  return (
    <aside class="details" aria-label={t("details.title")}>
      <h2>{t("details.title")}</h2>
      <DetailsBody store={store} />
    </aside>
  );
}

export function openDetailsDialog(store: AnyStore): void {
  openDialog((close) => (
    <Dialog title={t("details.title")} onClose={close}>
      <DetailsBody store={store} />
    </Dialog>
  ));
}
