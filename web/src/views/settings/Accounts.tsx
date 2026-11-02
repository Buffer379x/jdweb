// Premium accounts and the stored logins for HTTP and FTP (basic authentication).

import { useId, useState } from "preact/hooks";
import { post, put } from "../../api/client.ts";
import { ConfirmDialog, Dialog } from "../../components/Dialog.tsx";
import { HosterIcon } from "../../components/HosterIcon.tsx";
import { Lamp } from "../../components/Lamp.tsx";
import { LoadState } from "../../components/LoadState.tsx";
import { Menu, type MenuItem } from "../../components/Menu.tsx";
import { Icon } from "../../components/Icon.tsx";
import { useGet } from "../../hooks/useLoad.ts";
import { accountStatus, daysLeft } from "../../lib/accounts.ts";
import { formatBytes, formatDate } from "../../lib/format.ts";
import { lang, plural, t, type Key } from "../../i18n/index.ts";
import { live, pushToast, pushToastForError } from "../../store/app.ts";
import { openDialog } from "../../store/dialogs.ts";
import { PageHead } from "./common.tsx";

interface Account {
  id: string;
  hoster: string;
  username: string;
  enabled: boolean;
  valid: boolean;
  errorType: string;
  errorString: string;
  trafficLeft: number;
  trafficMax: number;
  validUntil: number;
}

const ERROR_KEYS: Record<string, Key> = { INVALID: "acc.err.INVALID", TEMP_ERROR: "acc.err.TEMP_ERROR", PLUGIN_ERROR: "acc.err.PLUGIN_ERROR", IO_ERROR: "acc.err.IO_ERROR" };

function statusText(a: Account, now: number): { text: string; tone: "run" | "err" | "muted"; detail: string } {
  const s = accountStatus(a, now);
  switch (s.state) {
    case "disabled": return { text: t("acc.state.disabled"), tone: s.tone, detail: "" };
    case "expired": return { text: t("acc.state.expired"), tone: s.tone, detail: "" };
    case "error": return { text: ERROR_KEYS[a.errorType] ? t(ERROR_KEYS[a.errorType]!) : t("acc.state.error"), tone: s.tone, detail: s.detail };
    default: return { text: t("acc.state.valid"), tone: s.tone, detail: "" };
  }
}

function untilText(a: Account, now: number): string {
  if (!(a.validUntil > 0)) return "–";
  const d = daysLeft(a.validUntil, now);
  const date = formatDate(a.validUntil, lang.value).split(",")[0] ?? "";
  if (d === null) return date;
  if (d < 0) return `${date} (${t("acc.over")})`;
  return `${date} (${plural("acc.days", d)})`;
}

function trafficText(a: Account): string {
  if (a.trafficLeft < 0 && a.trafficMax < 0) return "–";
  if (a.trafficMax > 0) return `${formatBytes(a.trafficLeft, lang.value)} / ${formatBytes(a.trafficMax, lang.value)}`;
  return formatBytes(a.trafficLeft, lang.value);
}

async function act(action: "enable" | "disable" | "remove" | "refresh", ids: string[], reload: () => void): Promise<void> {
  try {
    await post(`/accounts/actions/${action}`, { ids });
  } catch (e) {
    pushToastForError(e);
  }
  reload();
  // JDownloader checks an account in the background: look again shortly.
  if (action === "refresh") setTimeout(reload, 2500);
}

export function AccountsPage() {
  const load = useGet<Account[]>("/accounts");
  const [menu, setMenu] = useState<{ a: Account; at: DOMRect } | null>(null);
  const now = Date.now();
  const reload = load.reload;
  const items = (a: Account): MenuItem[] => [
    { id: "toggle", label: a.enabled ? t("acc.disable") : t("acc.enable"), onSelect: () => void act(a.enabled ? "disable" : "enable", [a.id], reload) },
    { id: "refresh", label: t("acc.refresh"), onSelect: () => void act("refresh", [a.id], reload) },
    { id: "edit", label: t("acc.edit"), onSelect: () => openDialog((close) => <AccountDialog account={a} onClose={close} onDone={reload} />) },
    {
      id: "remove", label: t("acc.remove"), danger: true, separatorBefore: true,
      onSelect: () => openDialog((close) => (
        <ConfirmDialog
          title={t("acc.remove")}
          lines={[t("acc.removeWhat", { user: a.username, host: a.hoster }), t("acc.removeConsequence")]}
          confirmLabel={t("acc.remove")}
          danger
          onConfirm={async () => { await post("/accounts/actions/remove", { ids: [a.id] }); reload(); }}
          onClose={close}
        />
      )),
    },
  ];
  return (
    <>
      <PageHead
        title={t("set.accounts")}
        intro={t("acc.intro")}
        actions={
          <>
            <button type="button" class="btn" disabled={!live.value || !load.data?.length} onClick={() => { void act("refresh", (load.data ?? []).map((a) => a.id), reload); pushToast("info", t("acc.refreshing")); }}>{t("acc.refreshAll")}</button>
            <button type="button" class="btn pri" disabled={!live.value} onClick={() => openDialog((close) => <AccountDialog onClose={close} onDone={reload} />)}>
              <Icon name="plus" size={13} />{t("acc.add")}
            </button>
          </>
        }
      />
      <LoadState load={load}>
        {(list) =>
          list.length === 0 ? (
            <div class="state-box">
              <p>{t("acc.none")}</p>
              <button type="button" class="btn pri" disabled={!live.value} onClick={() => openDialog((close) => <AccountDialog onClose={close} onDone={reload} />)}>{t("acc.add")}</button>
            </div>
          ) : (
            <div class="table-wrap">
              <table class="data-table">
                <caption class="visually-hidden">{t("set.accounts")}</caption>
                <thead>
                  <tr>
                    <th scope="col">{t("acc.col.hoster")}</th>
                    <th scope="col">{t("acc.col.user")}</th>
                    <th scope="col">{t("acc.col.state")}</th>
                    <th scope="col" class="num">{t("acc.col.traffic")}</th>
                    <th scope="col">{t("acc.col.until")}</th>
                    <th scope="col"><span class="visually-hidden">{t("menu.actions")}</span></th>
                  </tr>
                </thead>
                <tbody>
                  {list.map((a) => {
                    const st = statusText(a, now);
                    return (
                      <tr key={a.id} class={a.enabled ? "" : "off"}>
                        <td data-label={t("acc.col.hoster")}><span class="host"><HosterIcon host={a.hoster} /><span class="host-text" dir="auto">{a.hoster}</span></span></td>
                        <td data-label={t("acc.col.user")} dir="auto" class="cell-user">{a.username}</td>
                        <td data-label={t("acc.col.state")}>
                          <span class="st" title={st.detail}><Lamp tone={st.tone} /><span>{st.text}</span></span>
                          {st.detail && st.text !== st.detail ? <small class="muted block" dir="auto">{st.detail}</small> : null}
                        </td>
                        <td data-label={t("acc.col.traffic")} class="num mono">{trafficText(a)}</td>
                        <td data-label={t("acc.col.until")} class="mono">{untilText(a, now)}</td>
                        <td class="cell-act">
                          <button type="button" class="btn small" aria-haspopup="menu" aria-label={`${t("menu.actions")}: ${a.hoster} ${a.username}`} disabled={!live.value} onClick={(e) => setMenu({ a, at: e.currentTarget.getBoundingClientRect() })}>
                            {t("menu.actions")}
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )
        }
      </LoadState>
      {menu ? <Menu items={items(menu.a)} at={menu.at} title={t("menu.actions")} onClose={() => setMenu(null)} /> : null}
    </>
  );
}

function AccountDialog({ account, onClose, onDone }: { account?: Account; onClose: () => void; onDone: () => void }) {
  const hosters = useGet<string[]>("/accounts/hosters");
  const [host, setHost] = useState(account?.hoster ?? "");
  const [user, setUser] = useState(account?.username ?? "");
  const [pw, setPw] = useState("");
  const [busy, setBusy] = useState(false);
  const formId = useId();
  const listId = useId();
  const can = host.trim() !== "" && user.trim() !== "" && pw !== "" && !busy;
  const submit = async (e?: Event) => {
    e?.preventDefault();
    if (!can) return;
    setBusy(true);
    try {
      if (account) await put(`/accounts/${account.id}`, { username: user.trim(), password: pw });
      else await post("/accounts", { hoster: host.trim(), username: user.trim(), password: pw });
      pushToast("info", t("acc.saved"));
      onDone();
      setTimeout(onDone, 2500);
      onClose();
    } catch (err) {
      pushToastForError(err);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={account ? t("acc.edit") : t("acc.add")}
      onClose={onClose}
      onDismiss={busy ? () => {} : onClose}
      footer={
        <>
          <button type="button" class="btn" onClick={onClose}>{t("common.cancel")}</button>
          <button type="submit" form={formId} class="btn pri" disabled={!can}>{t("common.apply")}</button>
        </>
      }
    >
      <form id={formId} onSubmit={submit}>
        <label class="field">
          <span class="field-label">{t("acc.col.hoster")}</span>
          <input class="input" list={listId} value={host} disabled={!!account} data-autofocus={account ? undefined : ""} autocomplete="off" spellcheck={false} onInput={(e) => setHost(e.currentTarget.value)} />
          <datalist id={listId}>{(hosters.data ?? []).map((h) => <option key={h} value={h} />)}</datalist>
        </label>
        <label class="field">
          <span class="field-label">{t("acc.col.user")}</span>
          <input class="input" value={user} data-autofocus={account ? "" : undefined} autocomplete="off" spellcheck={false} dir="auto" onInput={(e) => setUser(e.currentTarget.value)} />
        </label>
        <label class="field">
          <span class="field-label">{t("login.password")}</span>
          <input class="input" type="password" value={pw} autocomplete="new-password" onInput={(e) => setPw(e.currentTarget.value)} />
          <span class="field-hint">{account ? t("acc.passwordAgain") : t("acc.passwordHint")}</span>
        </label>
      </form>
    </Dialog>
  );
}

// ---- basic authentication ------------------------------------------------------

interface BasicAuth {
  id: string;
  type: "HTTP" | "FTP";
  hostMask: string;
  username: string;
  enabled: boolean;
  created: number;
  lastValidated: number;
}

export function BasicAuthPage() {
  const load = useGet<BasicAuth[]>("/basicauth");
  const reload = load.reload;
  const open = (b?: BasicAuth) => openDialog((close) => <BasicAuthDialog entry={b} onClose={close} onDone={reload} />);
  return (
    <>
      <PageHead
        title={t("set.basicauth")}
        intro={t("ba.intro")}
        actions={<button type="button" class="btn pri" disabled={!live.value} onClick={() => open()}><Icon name="plus" size={13} />{t("ba.add")}</button>}
      />
      <LoadState load={load}>
        {(list) =>
          list.length === 0 ? (
            <div class="state-box">
              <p>{t("ba.none")}</p>
              <button type="button" class="btn pri" disabled={!live.value} onClick={() => open()}>{t("ba.add")}</button>
            </div>
          ) : (
            <div class="table-wrap">
              <table class="data-table">
                <caption class="visually-hidden">{t("set.basicauth")}</caption>
                <thead>
                  <tr>
                    <th scope="col">{t("ba.col.type")}</th>
                    <th scope="col">{t("ba.col.host")}</th>
                    <th scope="col">{t("acc.col.user")}</th>
                    <th scope="col">{t("acc.col.state")}</th>
                    <th scope="col">{t("ba.col.checked")}</th>
                    <th scope="col"><span class="visually-hidden">{t("menu.actions")}</span></th>
                  </tr>
                </thead>
                <tbody>
                  {list.map((b) => (
                    <tr key={b.id} class={b.enabled ? "" : "off"}>
                      <td data-label={t("ba.col.type")} class="mono">{b.type}</td>
                      <td data-label={t("ba.col.host")} class="mono" dir="ltr">{b.hostMask}</td>
                      <td data-label={t("acc.col.user")} dir="auto" class="cell-user">{b.username}</td>
                      <td data-label={t("acc.col.state")}><span class="st"><Lamp tone={b.enabled ? "run" : "muted"} />{b.enabled ? t("ba.enabled") : t("acc.state.disabled")}</span></td>
                      <td data-label={t("ba.col.checked")} class="mono">{b.lastValidated > 0 ? formatDate(b.lastValidated, lang.value) : "–"}</td>
                      <td class="cell-act">
                        <button type="button" class="btn small" disabled={!live.value} onClick={() => open(b)}>{t("acc.edit")}</button>
                        <button
                          type="button"
                          class="btn small"
                          disabled={!live.value}
                          onClick={() => openDialog((close) => (
                            <ConfirmDialog
                              title={t("ba.remove")}
                              lines={[t("ba.removeWhat", { user: b.username, host: b.hostMask }), t("ba.removeConsequence")]}
                              confirmLabel={t("ba.remove")}
                              danger
                              onConfirm={async () => { await post("/basicauth/remove", { ids: [b.id] }); reload(); }}
                              onClose={close}
                            />
                          ))}
                        >
                          {t("acc.remove")}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        }
      </LoadState>
    </>
  );
}

function BasicAuthDialog({ entry, onClose, onDone }: { entry?: BasicAuth; onClose: () => void; onDone: () => void }) {
  const [type, setType] = useState<"HTTP" | "FTP">(entry?.type ?? "HTTP");
  const [mask, setMask] = useState(entry?.hostMask ?? "");
  const [user, setUser] = useState(entry?.username ?? "");
  const [pw, setPw] = useState("");
  const [enabled, setEnabled] = useState(entry?.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const formId = useId();
  const can = mask.trim() !== "" && user.trim() !== "" && pw !== "" && !busy;
  const submit = async (e?: Event) => {
    e?.preventDefault();
    if (!can) return;
    setBusy(true);
    try {
      const body = { type, hostMask: mask.trim(), username: user.trim(), password: pw };
      if (entry) await put(`/basicauth/${entry.id}`, { ...body, enabled });
      else await post("/basicauth", body);
      onDone();
      onClose();
    } catch (err) {
      pushToastForError(err);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={entry ? t("ba.edit") : t("ba.add")}
      onClose={onClose}
      onDismiss={busy ? () => {} : onClose}
      footer={
        <>
          <button type="button" class="btn" onClick={onClose}>{t("common.cancel")}</button>
          <button type="submit" form={formId} class="btn pri" disabled={!can}>{t("common.apply")}</button>
        </>
      }
    >
      <form id={formId} onSubmit={submit}>
        <label class="field">
          <span class="field-label">{t("ba.col.type")}</span>
          <select class="input" value={type} onChange={(e) => setType(e.currentTarget.value as "HTTP" | "FTP")}>
            <option value="HTTP">HTTP</option>
            <option value="FTP">FTP</option>
          </select>
        </label>
        <label class="field">
          <span class="field-label">{t("ba.col.host")}</span>
          <input class="input mono" value={mask} data-autofocus="" autocomplete="off" spellcheck={false} dir="ltr" onInput={(e) => setMask(e.currentTarget.value)} />
          <span class="field-hint">{t("ba.hostHint")}</span>
        </label>
        <label class="field">
          <span class="field-label">{t("acc.col.user")}</span>
          <input class="input" value={user} autocomplete="off" spellcheck={false} dir="auto" onInput={(e) => setUser(e.currentTarget.value)} />
        </label>
        <label class="field">
          <span class="field-label">{t("login.password")}</span>
          <input class="input" type="password" value={pw} autocomplete="new-password" onInput={(e) => setPw(e.currentTarget.value)} />
          <span class="field-hint">{entry ? t("acc.passwordAgain") : t("acc.passwordHint")}</span>
        </label>
        {entry ? (
          <label class="check"><input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.currentTarget.checked)} /><span>{t("ba.enabled")}</span></label>
        ) : null}
      </form>
    </Dialog>
  );
}
