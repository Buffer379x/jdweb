// Which editor a setting of JDownloader gets, and how a typed value becomes a value to save.
// Pure: the views only draw what this decides.

export const MASK = "••••••";

export interface ConfigEntry {
  interface: string;
  storage: string;
  key: string;
  type: string;
  abstractType: string;
  docs: string;
  value: unknown;
  defaultValue: unknown;
  enumLabel?: string;
  enumOptions?: string[][];
  secret: boolean;
}

export type EditorKind = "bool" | "int" | "long" | "double" | "string" | "enum" | "list" | "json";

const INT_MIN = -2147483648;
const INT_MAX = 2147483647;

export function editorFor(e: Pick<ConfigEntry, "abstractType" | "enumOptions">): EditorKind {
  switch (e.abstractType) {
    case "BOOLEAN": return "bool";
    case "INT": return "int";
    case "LONG": return "long";
    case "DOUBLE": case "FLOAT": return "double";
    case "STRING": case "HEX_COLOR": return "string";
    case "ENUM": return e.enumOptions && e.enumOptions.length > 0 ? "enum" : "string";
    case "STRING_LIST": case "INT_LIST": case "ENUM_LIST": return "list";
    default: return "json"; // OBJECT, OBJECT_LIST and what JDownloader adds later
  }
}

export type ParseError = "int" | "long" | "double" | "list" | "listItems" | "listInts" | "enum" | "json" | "empty";
export type Parsed = { ok: true; value: unknown } | { ok: false; error: ParseError };

/** The text an editor starts with. */
export function valueToText(kind: EditorKind, value: unknown): string {
  if (value === null || value === undefined) return kind === "list" ? "[]" : "";
  switch (kind) {
    case "bool": return value ? "true" : "false";
    case "list": case "json": return JSON.stringify(value, null, 2);
    default: return String(value);
  }
}

function isInt(text: string): boolean {
  return /^-?\d+$/.test(text);
}

/** Turns what was typed into the value to send, or says what is wrong with it. */
export function parseText(kind: EditorKind, text: string, e: Pick<ConfigEntry, "abstractType" | "enumOptions">): Parsed {
  const s = text.trim();
  switch (kind) {
    case "bool": return { ok: true, value: s === "true" };
    case "string": return { ok: true, value: text };
    case "int": {
      if (!isInt(s)) return { ok: false, error: "int" };
      const n = Number(s);
      return n >= INT_MIN && n <= INT_MAX ? { ok: true, value: n } : { ok: false, error: "int" };
    }
    case "long": {
      // JSON numbers are doubles: above 2^53 a long would silently change.
      if (!isInt(s)) return { ok: false, error: "long" };
      const n = Number(s);
      return Number.isSafeInteger(n) ? { ok: true, value: n } : { ok: false, error: "long" };
    }
    case "double": {
      const n = Number(s.replace(",", "."));
      return s !== "" && Number.isFinite(n) ? { ok: true, value: n } : { ok: false, error: "double" };
    }
    case "enum": {
      const ok = (e.enumOptions ?? []).some((o) => o[0] === text);
      return ok ? { ok: true, value: text } : { ok: false, error: "enum" };
    }
    case "list": {
      if (s === "") return { ok: true, value: [] };
      let v: unknown;
      try {
        v = JSON.parse(s);
      } catch {
        return { ok: false, error: "list" };
      }
      if (!Array.isArray(v)) return { ok: false, error: "list" };
      if (e.abstractType === "INT_LIST") return v.every((x) => Number.isSafeInteger(x)) ? { ok: true, value: v } : { ok: false, error: "listInts" };
      return v.every((x) => typeof x === "string") ? { ok: true, value: v } : { ok: false, error: "listItems" };
    }
    case "json": {
      if (s === "") return { ok: false, error: "empty" };
      try {
        return { ok: true, value: JSON.parse(s) };
      } catch {
        return { ok: false, error: "json" };
      }
    }
  }
}

/** Equal as JSON; null, an empty list and an empty text are different things for a secret but one for a number field. */
export function sameValue(a: unknown, b: unknown): boolean {
  return JSON.stringify(a ?? null) === JSON.stringify(b ?? null);
}

// ---- secrets -------------------------------------------------------------

export function isMask(v: unknown): boolean {
  return v === MASK;
}

/** A value that is, or holds, the mask must never be sent: the server refuses it too. */
export function holdsMask(v: unknown): boolean {
  return isMask(v) || (Array.isArray(v) && v.some(isMask));
}

/** Whether a secret has a value at all. The server leaves empty values empty and masks the others. */
export function secretIsSet(v: unknown): boolean {
  if (v === null || v === undefined || v === "") return false;
  if (Array.isArray(v)) return v.length > 0;
  return true;
}

// ---- search --------------------------------------------------------------

export function shortInterface(iface: string): string {
  const i = iface.lastIndexOf(".");
  return i >= 0 ? iface.slice(i + 1) : iface;
}

/** One lower-case line per entry that the filter looks through. Worked out once per load. */
export function searchLine(e: Pick<ConfigEntry, "interface" | "storage" | "key" | "docs">): string {
  return `${shortInterface(e.interface)} ${e.storage} ${e.key} ${e.docs}`.toLowerCase();
}

/** Entries whose line holds every word of the query; an empty query keeps all. */
export function filterIndexes(lines: readonly string[], query: string): number[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const out: number[] = [];
  for (let i = 0; i < lines.length; i++) {
    const l = lines[i]!;
    let all = true;
    for (const w of words) {
      if (l.indexOf(w) < 0) {
        all = false;
        break;
      }
    }
    if (all) out.push(i);
  }
  return out;
}
