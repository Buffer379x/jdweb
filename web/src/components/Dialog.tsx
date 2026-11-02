import type { ComponentChildren } from "preact";
import { useEffect, useId, useRef, useState } from "preact/hooks";
import { t } from "../i18n/index.ts";
import { pushToastForError } from "../store/app.ts";
import { Icon } from "./Icon.tsx";

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

interface DialogProps {
  title: string;
  onClose: () => void;
  children: ComponentChildren;
  footer?: ComponentChildren;
  wide?: boolean;
  /** Closing by Escape or the backdrop would lose input: ask for it to be off. */
  sticky?: boolean;
}

/** A modal dialog: focus moves in, Tab stays inside, Escape closes, focus returns where it was. */
export function Dialog({ title, onClose, children, footer, wide, sticky }: DialogProps) {
  const ref = useRef<HTMLDivElement>(null);
  const titleId = useId();
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    const root = ref.current!;
    const first = root.querySelector<HTMLElement>("[data-autofocus]") ?? root.querySelector<HTMLElement>(FOCUSABLE);
    first?.focus();
    return () => {
      if (prev && document.contains(prev)) prev.focus();
    };
  }, []);
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      e.preventDefault();
      onClose();
    } else if (e.key === "Tab") {
      const els = [...ref.current!.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((x) => x.offsetParent !== null);
      if (els.length === 0) return;
      const first = els[0]!;
      const last = els[els.length - 1]!;
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  };
  return (
    <div class="scrim" onMouseDown={(e) => e.target === e.currentTarget && !sticky && onClose()}>
      <div ref={ref} class={`dialog${wide ? " wide" : ""}`} role="dialog" aria-modal="true" aria-labelledby={titleId} onKeyDown={onKey}>
        <header class="dialog-head">
          <h2 id={titleId}>{title}</h2>
          <button type="button" class="icon-btn" onClick={onClose} aria-label={t("common.close")}>
            <Icon name="x" />
          </button>
        </header>
        <div class="dialog-body">{children}</div>
        {footer ? <footer class="dialog-foot">{footer}</footer> : null}
      </div>
    </div>
  );
}

interface ConfirmProps {
  title: string;
  /** The first line says how many and what, the next ones what follows from it. */
  lines: string[];
  confirmLabel: string;
  danger?: boolean;
  onConfirm: () => Promise<void> | void;
  onClose: () => void;
}

/** Names count and consequence; the safe choice has the focus. */
export function ConfirmDialog({ title, lines, confirmLabel, danger, onConfirm, onClose }: ConfirmProps) {
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    try {
      await onConfirm();
    } catch (e) {
      pushToastForError(e);
    }
    onClose();
  };
  return (
    <Dialog
      title={title}
      onClose={onClose}
      footer={
        <>
          <button type="button" class="btn" data-autofocus onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button type="button" class={`btn ${danger ? "danger" : "pri"}`} disabled={busy} onClick={run}>
            {confirmLabel}
          </button>
        </>
      }
    >
      {lines.map((l, i) => (
        <p key={i} class={i === 0 ? "confirm-lead" : "confirm-more"} dir="auto">
          {l}
        </p>
      ))}
    </Dialog>
  );
}

export interface PromptField {
  name: string;
  label: string;
  value?: string;
  kind?: "text" | "password" | "multiline";
  required?: boolean;
  placeholder?: string;
  mono?: boolean;
  hint?: string;
}

interface PromptProps {
  title: string;
  intro?: string;
  fields: PromptField[];
  submitLabel: string;
  onSubmit: (values: Record<string, string>) => Promise<void> | void;
  onClose: () => void;
}

export function PromptDialog({ title, intro, fields, submitLabel, onSubmit, onClose }: PromptProps) {
  const [vals, setVals] = useState<Record<string, string>>(() => Object.fromEntries(fields.map((f) => [f.name, f.value ?? ""])));
  const [busy, setBusy] = useState(false);
  const ok = fields.every((f) => !f.required || (vals[f.name] ?? "").trim() !== "");
  const submit = async (e?: Event) => {
    e?.preventDefault();
    if (!ok || busy) return;
    setBusy(true);
    try {
      await onSubmit(vals);
      onClose();
    } catch (err) {
      pushToastForError(err);
      setBusy(false);
    }
  };
  const id = useId();
  return (
    <Dialog
      title={title}
      onClose={onClose}
      footer={
        <>
          <button type="button" class="btn" onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button type="submit" form={id} class="btn pri" disabled={!ok || busy}>
            {submitLabel}
          </button>
        </>
      }
    >
      <form id={id} onSubmit={submit} onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && submit(e)}>
        {intro ? <p class="confirm-more">{intro}</p> : null}
        {fields.map((f, i) => (
          <label key={f.name} class="field">
            <span class="field-label">{f.label}</span>
            {f.kind === "multiline" ? (
              <textarea
                class={`input${f.mono ? " mono" : ""}`}
                rows={4}
                value={vals[f.name]}
                placeholder={f.placeholder}
                data-autofocus={i === 0 ? "" : undefined}
                dir="auto"
                onInput={(e) => setVals({ ...vals, [f.name]: e.currentTarget.value })}
              />
            ) : (
              <input
                class={`input${f.mono ? " mono" : ""}`}
                type={f.kind === "password" ? "password" : "text"}
                value={vals[f.name]}
                placeholder={f.placeholder}
                data-autofocus={i === 0 ? "" : undefined}
                autocomplete="off"
                spellcheck={false}
                dir="auto"
                onInput={(e) => setVals({ ...vals, [f.name]: e.currentTarget.value })}
              />
            )}
            {f.hint ? <span class="field-hint">{f.hint}</span> : null}
          </label>
        ))}
      </form>
    </Dialog>
  );
}
