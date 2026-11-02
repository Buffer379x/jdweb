// The speed limit form of the General page, as plain functions so that its behaviour can be tested.
// The form shows what JDownloader has until the person edits it; the switch sends the typed value.

import { parseDecimal, splitBytesPerSecond, toBytesPerSecond, type SpeedUnit } from "./units.ts";

/** What the form starts with when no limit has been set: 1 MB/s. */
const DEFAULT_BYTES = 1024 * 1024;

export interface LimitForm {
  value: string;
  unit: SpeedUnit;
  /** The person typed in the field or changed the unit: from then on the form is theirs. */
  edited: boolean;
}

function fromBytes(limitBytes: number | undefined): { value: string; unit: SpeedUnit } {
  const s = splitBytesPerSecond(limitBytes && limitBytes > 0 ? limitBytes : DEFAULT_BYTES);
  return { value: String(s.value), unit: s.unit };
}

export function initialForm(limitBytes: number | undefined): LimitForm {
  return { ...fromBytes(limitBytes), edited: false };
}

/** The status changed (another browser, JDownloader itself): an untouched form follows it, an edited one stays. */
export function followStatus(form: LimitForm, limitBytes: number | undefined): LimitForm {
  return form.edited ? form : initialForm(limitBytes);
}

export function edit(form: LimitForm, patch: Partial<Pick<LimitForm, "value" | "unit">>): LimitForm {
  return { ...form, ...patch, edited: true };
}

/** The bytes per second the form holds, or null when it is no valid speed. */
export function formBytes(form: LimitForm): number | null {
  const n = parseDecimal(form.value);
  return n === null ? null : toBytesPerSecond(n, form.unit);
}

/** Whether "Save" has something to save: a valid value that is not what JDownloader has. Never without an edit. */
export function isDirty(form: LimitForm, limitBytes: number | undefined): boolean {
  const b = formBytes(form);
  return form.edited && b !== null && b !== limitBytes;
}

export type SwitchRequest = { ok: true; enabled: boolean; bytesPerSecond: number } | { ok: false };

/**
 * What a click on the switch sends. Off sends only the switch. On sends the typed value, so that a limit that
 * was never set cannot be switched on empty (the server answers 400 to that). If the typed value is no speed
 * and a limit is stored, only the switch goes; if none is stored there is nothing valid to switch on.
 */
export function switchRequest(on: boolean, form: LimitForm, limitBytes: number | undefined): SwitchRequest {
  if (!on) return { ok: true, enabled: false, bytesPerSecond: 0 };
  const b = formBytes(form);
  if (b !== null) return { ok: true, enabled: true, bytesPerSecond: b };
  if (limitBytes !== undefined && limitBytes > 0) return { ok: true, enabled: true, bytesPerSecond: 0 };
  return { ok: false };
}
