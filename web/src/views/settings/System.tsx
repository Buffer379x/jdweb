import type { ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import { get, post } from "../../api/client.ts";
import { ConfirmDialog } from "../../components/Dialog.tsx";
import { Lamp } from "../../components/Lamp.tsx";
import { LoadState } from "../../components/LoadState.tsx";
import { useGet } from "../../hooks/useLoad.ts";
import { errorText } from "../../store/errors.ts";
import { formatBytes, formatDate } from "../../lib/format.ts";
import { availableActions, formatUptime, heapPercent } from "../../lib/system.ts";
import { lang, t, type Key } from "../../i18n/index.ts";
import { jdRestarting, live, pushToast, pushToastForError, status } from "../../store/app.ts";
import { openDialog } from "../../store/dialogs.ts";
import { systemAction } from "../../store/jd.ts";
import type { FolderInfo } from "../../api/types.ts";
import { PageHead, Section } from "./common.tsx";

interface SystemInfo {
  version: string;
  revision: number;
  docker: boolean;
  url: string;
  headless: boolean;
  osFamily: string;
  osString: string;
  javaVersion: string;
  javaName: string;
  heapUsed: number;
  heapCommitted: number;
  heapMax: number;
  startedAt: number;
}

function Row({ label, children, mono }: { label: string; children: ComponentChildren; mono?: boolean }) {
  return (
    <div class="kv">
      <dt>{label}</dt>
      <dd class={mono ? "mono" : undefined} dir="auto">{children}</dd>
    </div>
  );
}

export function SystemPage() {
  const info = useGet<SystemInfo>("/system");
  return (
    <>
      <PageHead title={t("set.system")} intro={t("sys.intro")} />
      <LoadState load={info}>{(s) => <SystemBody s={s} />}</LoadState>
    </>
  );
}

function SystemBody({ s }: { s: SystemInfo }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(id);
  }, []);
  const folders = useGet<FolderInfo[]>("/linkgrabber/folders", ((r: { folders: FolderInfo[] }) => r.folders ?? []) as never);
  const heap = heapPercent(s.heapUsed, s.heapMax);
  // The status is the live view of Docker; the system call was made once.
  const docker = status.value?.jd.docker ?? s.docker;
  const actions = availableActions(docker);
  return (
    <>
      <Section title={t("sys.about")} id="sys-about">
        <dl class="kvs">
          <Row label={t("sys.jdweb")} mono>{s.version}</Row>
          <Row label={t("sys.revision")} mono>{s.revision}</Row>
          <Row label={t("sys.address")} mono>{s.url}</Row>
          <Row label={t("sys.os")}>{s.osString || s.osFamily}{docker ? ` (${t("sys.docker")})` : ""}</Row>
          <Row label={t("sys.java")}>{s.javaName} {s.javaVersion}</Row>
          <Row label={t("sys.uptime")} mono>{formatUptime(s.startedAt, now) || "–"}{s.startedAt > 0 ? ` · ${t("sys.since", { date: formatDate(s.startedAt, lang.value) })}` : ""}</Row>
          <Row label={t("sys.memory")}>
            <span class="mono">{formatBytes(s.heapUsed, lang.value)} / {formatBytes(s.heapMax, lang.value)} ({heap} %)</span>
            <span class="meter" role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={heap} aria-label={t("sys.memory")}><i class="meter-fill" style={{ "--p": `${heap}%` } as never} /></span>
          </Row>
        </dl>
      </Section>
      <Section title={t("sys.storage")} id="sys-storage">
        <LoadState load={folders}>
          {(list) =>
            list.length === 0 ? (
              <p class="muted">{t("sys.storageNone")}</p>
            ) : (
              <ul class="folders">
                {list.slice(0, 6).map((f) => {
                  const used = f.size > 0 ? Math.round(((f.size - f.free) / f.size) * 100) : 0;
                  return (
                    <li key={f.path}>
                      <span class="mono ellipsis" dir="ltr" title={f.path}>{f.path}</span>
                      <span class="mono muted">{f.size > 0 ? t("sys.free", { free: formatBytes(f.free, lang.value), size: formatBytes(f.size, lang.value) }) : t("sys.unknown")}</span>
                      {f.size > 0 ? <span class="meter" role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={used} aria-label={f.path}><i class="meter-fill" style={{ "--p": `${used}%` } as never} /></span> : null}
                    </li>
                  );
                })}
              </ul>
            )
          }
        </LoadState>
      </Section>
      <Section title={t("sys.update")} id="sys-update"><UpdateBox /></Section>
      <Section title={t("sys.control")} id="sys-control">
        {jdRestarting.value ? <p class="note" role="status"><Lamp tone="info" /> {t("sys.restarting")}</p> : null}
        <div class="btn-row">
          {actions.map((a) => (
            <button key={a} type="button" class={`btn${a === "restart" ? "" : " danger-text"}`} disabled={!live.value} onClick={() => confirmAction(a)}>
              {t(`sys.action.${a}` as Key)}
            </button>
          ))}
        </div>
        {docker ? <p class="muted small">{t("sys.dockerNote")}</p> : null}
      </Section>
    </>
  );
}

function confirmAction(a: "restart" | "exit" | "shutdown" | "hibernate" | "standby"): void {
  openDialog((close) => (
    <ConfirmDialog
      title={t(`sys.action.${a}` as Key)}
      lines={[t(`sys.confirm.${a}` as Key), t(`sys.consequence.${a}` as Key)]}
      confirmLabel={t(`sys.action.${a}` as Key)}
      danger={a !== "restart"}
      onConfirm={() => systemAction(a)}
      onClose={close}
    />
  ));
}

function UpdateBox() {
  const upd = useGet<{ available: boolean }>("/update");
  const [checking, setChecking] = useState(false);
  const check = async () => {
    setChecking(true);
    try {
      await post("/update/check");
      // JDownloader looks in the background: ask again after a moment.
      await new Promise((r) => setTimeout(r, 2500));
      const r = await get<{ available: boolean }>("/update");
      upd.setData(r);
      pushToast("info", r.available ? t("sys.updateAvailable") : t("sys.updateNone"));
    } catch (e) {
      pushToastForError(e);
    }
    setChecking(false);
  };
  const apply = () =>
    openDialog((close) => (
      <ConfirmDialog
        title={t("sys.updateApply")}
        lines={[t("sys.updateConfirm"), t("sys.updateConsequence")]}
        confirmLabel={t("sys.updateApply")}
        danger
        onConfirm={async () => {
          await post("/update/apply");
          jdRestarting.value = true;
          setTimeout(() => (jdRestarting.value = false), 180_000);
          pushToast("info", t("sys.updateStarted"));
        }}
        onClose={close}
      />
    ));
  const avail = upd.data?.available;
  return (
    <div class="update-box">
      <p class="st">
        <Lamp tone={avail ? "accent" : "muted"} />
        <span role="status">
          {upd.data === undefined ? (upd.error ? `${t("state.loadFailed")} ${errorText(upd.error)}` : t("boot.loading")) : avail ? t("sys.updateAvailable") : t("sys.updateNone")}
          {upd.data !== undefined && upd.error ? ` ${t("state.stale")}` : ""}
        </span>
      </p>
      <div class="btn-row">
        <button type="button" class="btn" disabled={!live.value || checking} onClick={check}>{checking ? t("sys.checking") : t("sys.updateCheck")}</button>
        {avail ? <button type="button" class="btn pri" disabled={!live.value} onClick={apply}>{t("sys.updateApply")}</button> : null}
      </div>
    </div>
  );
}
