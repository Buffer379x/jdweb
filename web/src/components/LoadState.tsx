import type { ComponentChildren } from "preact";
import { t } from "../i18n/index.ts";
import { errorDetail, errorText } from "../store/errors.ts";
import type { Loaded } from "../hooks/useLoad.ts";

/** Loading, error with retry, or the content. Content is shown while a reload runs. */
export function LoadState<T>({ load, children }: { load: Loaded<T>; children: (data: T) => ComponentChildren }) {
  if (load.data !== undefined) return <>{children(load.data)}</>;
  if (load.error) {
    return (
      <div class="state-box" role="alert">
        <p class="state-title">{t("state.loadFailed")}</p>
        <p class="muted" dir="auto">{errorText(load.error)}</p>
        {errorDetail(load.error) ? <p class="muted small" dir="auto">{errorDetail(load.error)}</p> : null}
        <button type="button" class="btn" onClick={load.reload}>{t("common.retry")}</button>
      </div>
    );
  }
  return <div class="state-box" role="status">{t("boot.loading")}</div>;
}
