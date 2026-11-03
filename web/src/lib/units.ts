// Speed limit units. JDownloader counts 1 KB as 1024 bytes, like the rest of the interface.

export type SpeedUnit = "KB" | "MB";
const KB = 1024;
const MB = 1024 * 1024;
/** JDWeb accepts 0 to 2147483647 bytes per second, the 32-bit limit of JDownloader (docs/api.md). */
export const MAX_BYTES_PER_SECOND = 2147483647;

/** A decimal number typed by a person: "1,5" and "1.5" are the same. Null when it is not one. */
export function parseDecimal(text: string): number | null {
  const s = text.trim().replace(",", ".");
  if (!/^\d+(\.\d+)?$/.test(s)) return null;
  const n = Number(s);
  return Number.isFinite(n) ? n : null;
}

/** Bytes per second for an input, or null if it is not a number above zero or too large. */
export function toBytesPerSecond(value: number, unit: SpeedUnit): number | null {
  if (!Number.isFinite(value) || value <= 0) return null;
  const bytes = Math.round(value * (unit === "MB" ? MB : KB));
  return bytes >= 1 && bytes <= MAX_BYTES_PER_SECOND ? bytes : null;
}

/**
 * The unit a number of bytes per second is shown in: whole megabytes as MB, everything else as KB. The value
 * is exact (a KB is a power of two, so the division loses nothing): typing it back gives the same bytes, and
 * a form that starts from it is not changed by the round trip.
 */
export function splitBytesPerSecond(bytes: number): { value: number; unit: SpeedUnit } {
  if (bytes >= MB && bytes % MB === 0) return { value: bytes / MB, unit: "MB" };
  return { value: bytes / KB, unit: "KB" };
}

/** A speed for reading, rounded to two decimals: "0.98 KB/s". */
export function describeRate(bytes: number): string {
  const { value, unit } = splitBytesPerSecond(bytes);
  return `${Math.round(value * 100) / 100} ${unit}/s`;
}
