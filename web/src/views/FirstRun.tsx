// After setup, warnings of the diagnostics that nobody has confirmed are shown once, with a way to confirm them.

import { useEffect } from "preact/hooks";
import { get } from "../api/client.ts";
import { Dialog } from "../components/Dialog.tsx";
import { needsAttention, problems, type Report } from "../lib/diag.ts";
import { t } from "../i18n/index.ts";
import { navigateSettings, pushToast, status } from "../store/app.ts";
import { openDialog } from "../store/dialogs.ts";
import { CheckList, acknowledge } from "./settings/Diagnostics.tsx";

let asked = false;

function FirstRunDialog({ report, onClose }: { report: Report; onClose: () => void }) {
  return (
    <Dialog
      title={t("first.title")}
      onClose={onClose}
      wide
      footer={
        <>
          <button type="button" class="btn" data-autofocus onClick={onClose}>{t("first.later")}</button>
          <button type="button" class="btn" onClick={() => { onClose(); navigateSettings("diagnostics"); }}>{t("first.details")}</button>
          <button
            type="button"
            class="btn pri"
            onClick={async () => {
              if (await acknowledge()) {
                pushToast("info", t("diag.ackDone"));
                onClose();
              }
            }}
          >
            {t("first.ack")}
          </button>
        </>
      }
    >
      <p class="confirm-more">{t("first.intro")}</p>
      <CheckList checks={problems(report)} />
      <p class="confirm-more">{t("first.ackNote")}</p>
    </Dialog>
  );
}

/** Asks for the diagnostics once per page load, as soon as the connection state is known. */
export function useFirstRun(): void {
  const known = !!status.value;
  useEffect(() => {
    if (!known || asked) return;
    asked = true;
    get<Report>("/diagnostics")
      .then((r) => {
        if (needsAttention(r)) openDialog((close) => <FirstRunDialog report={r} onClose={close} />);
      })
      .catch(() => {
        asked = false; // try again with the next status
      });
  }, [known]);
}
