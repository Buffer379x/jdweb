// Questions of JDownloader (dialogs): which fields an answer has, and what is sent.

export type FieldKind = "bool" | "number" | "text";

/** The Java type of an answer field decides the control. */
export function fieldKind(javaType: string): FieldKind {
  const t = javaType.replace(/^(class|interface) /, "");
  if (/(^|\.)(boolean|Boolean)$/.test(t)) return "bool";
  if (/(^|\.)(int|long|short|byte|double|float|Integer|Long|Short|Byte|Double|Float)$/.test(t)) return "number";
  return "text";
}

export interface AnswerField {
  name: string;
  kind: FieldKind;
}

/** The answer fields other than the close reason, which the buttons carry. */
export function answerFields(answer: Record<string, string> | undefined): AnswerField[] {
  return Object.entries(answer ?? {})
    .filter(([name]) => name.toLowerCase() !== "closereason")
    .map(([name, type]) => ({ name, kind: fieldKind(type) }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function hasCloseReason(answer: Record<string, string> | undefined): boolean {
  return Object.keys(answer ?? {}).some((k) => k.toLowerCase() === "closereason");
}

/** The data of an answer: the close reason plus the fields that were filled. Numbers go as numbers. */
export function buildAnswer(
  closeReason: string,
  fields: AnswerField[],
  values: Record<string, string | boolean>,
  answer: Record<string, string> | undefined,
): Record<string, string | number | boolean> {
  const data: Record<string, string | number | boolean> = {};
  const reasonKey = Object.keys(answer ?? {}).find((k) => k.toLowerCase() === "closereason") ?? "closereason";
  data[reasonKey] = closeReason;
  for (const f of fields) {
    const v = values[f.name];
    if (v === undefined) continue;
    if (f.kind === "bool") data[f.name] = v === true || v === "true";
    else if (f.kind === "number") {
      const n = Number(String(v).trim());
      if (String(v).trim() !== "" && Number.isFinite(n)) data[f.name] = n;
    } else data[f.name] = String(v);
  }
  return data;
}

const LEAD = ["title", "caption", "message", "text", "question"];

/** The lines to show as the question, and the other properties as a list. Everything is text. */
export function splitProperties(props: Record<string, string>): { title: string; message: string; rest: [string, string][] } {
  const lower = new Map(Object.keys(props).map((k) => [k.toLowerCase(), k]));
  const take = (names: string[]) => {
    for (const n of names) {
      const k = lower.get(n);
      if (k !== undefined && props[k]) {
        lower.delete(n);
        return props[k]!;
      }
    }
    return "";
  };
  const title = take(["title", "caption"]);
  const message = take(["message", "text", "question"]);
  const used = new Set(LEAD);
  const rest = Object.entries(props).filter(([k, v]) => v !== "" && !used.has(k.toLowerCase()));
  return { title, message, rest };
}

/** The last part of a Java interface name, for the small label above the question. */
export function dialogKind(type: string): string {
  const i = type.lastIndexOf(".");
  return (i >= 0 ? type.slice(i + 1) : type).replace(/Interface$/, "").replace(/Dialog$/, "");
}
