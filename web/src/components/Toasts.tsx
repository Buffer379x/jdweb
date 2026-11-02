import { dismissToast, toasts } from "../store/app.ts";
import { t } from "../i18n/index.ts";
import { Icon } from "./Icon.tsx";

/** Messages about what just happened. Errors are announced at once, the rest politely. */
export function Toasts() {
  const list = toasts.value;
  return (
    <div class="toasts">
      <div role="status" aria-live="polite">
        {list.filter((x) => x.kind === "info").map((x) => <ToastItem key={x.id} {...x} />)}
      </div>
      <div role="alert">
        {list.filter((x) => x.kind === "error").map((x) => <ToastItem key={x.id} {...x} />)}
      </div>
    </div>
  );
}

function ToastItem({ id, kind, text, detail }: { id: number; kind: string; text: string; detail?: string }) {
  return (
    <div class={`toast ${kind}`}>
      <div class="toast-text">
        <span dir="auto">{text}</span>
        {detail ? <small dir="auto">{detail}</small> : null}
      </div>
      <button type="button" class="icon-btn" onClick={() => dismissToast(id)} aria-label={t("common.close")}>
        <Icon name="x" size={14} />
      </button>
    </div>
  );
}
