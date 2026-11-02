import type { ComponentChildren } from "preact";

/** A labelled on/off control. The state is a word as well as a position. */
export function Switch({ checked, onChange, disabled, children, hint }: {
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  children: ComponentChildren;
  hint?: string;
}) {
  return (
    <label class={`switch${disabled ? " disabled" : ""}`}>
      <input type="checkbox" role="switch" checked={checked} disabled={disabled} onChange={(e) => onChange(e.currentTarget.checked)} />
      <span class="switch-text">
        <span class="switch-label">{children}</span>
        {hint ? <span class="field-hint">{hint}</span> : null}
      </span>
    </label>
  );
}
