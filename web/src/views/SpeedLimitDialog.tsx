import { useId, useState } from "preact/hooks";
import { Dialog } from "../components/Dialog.tsx";
import { parseDecimal, splitBytesPerSecond, toBytesPerSecond, type SpeedUnit } from "../lib/units.ts";
import { t } from "../i18n/index.ts";
import { pushToast, status } from "../store/app.ts";
import { openDialog } from "../store/dialogs.ts";
import { setSpeedLimit } from "../store/jd.ts";

function SpeedLimitDialog({ onClose }: { onClose: () => void }) {
  const tg = status.value?.toggles;
  const start = splitBytesPerSecond(tg && tg.limitBytes > 0 ? tg.limitBytes : 1024 * 1024);
  const [value, setValue] = useState(String(start.value));
  const [unit, setUnit] = useState<SpeedUnit>(start.unit);
  const [busy, setBusy] = useState(false);
  const id = useId();
  const n = parseDecimal(value);
  const bytes = n === null ? null : toBytesPerSecond(n, unit);
  const submit = async (e?: Event) => {
    e?.preventDefault();
    if (bytes === null || busy) return;
    setBusy(true);
    if (await setSpeedLimit(true, bytes)) {
      pushToast("info", t("gen.limitSaved"));
      onClose();
    } else setBusy(false);
  };
  return (
    <Dialog
      title={t("gen.limit")}
      onClose={onClose}
      onDismiss={busy ? () => {} : onClose}
      footer={<><button type="button" class="btn" onClick={onClose}>{t("common.cancel")}</button><button type="submit" form={id} class="btn pri" disabled={bytes === null || busy}>{t("gen.limitOn")}</button></>}
    >
      <form id={id} onSubmit={submit}>
        <p class="confirm-more">{t("gen.limitText")}</p>
        <div class="speed-edit">
          <label class="field-inline">
            <span class="field-label">{t("gen.limitValue")}</span>
            <input class="input mono narrow" inputMode="decimal" data-autofocus="" autocomplete="off" value={value} aria-invalid={value !== "" && bytes === null ? "true" : undefined} onInput={(e) => setValue(e.currentTarget.value)} />
          </label>
          <label class="field-inline">
            <span class="field-label">{t("gen.limitUnit")}</span>
            <select class="input narrow" value={unit} onChange={(e) => setUnit(e.currentTarget.value as SpeedUnit)}>
              <option value="KB">KB/s</option>
              <option value="MB">MB/s</option>
            </select>
          </label>
        </div>
        {value !== "" && bytes === null ? <p class="field-hint bad" role="alert">{t("gen.limitInvalid")}</p> : null}
      </form>
    </Dialog>
  );
}

export function openSpeedLimit(): void {
  openDialog((close) => <SpeedLimitDialog onClose={close} />);
}
