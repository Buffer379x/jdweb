// Small helpers of the system page.

/** How long JDownloader has been running: "3 d 04 h", "5 h 07 min", "12 min". Empty when unknown. */
export function formatUptime(startedAt: number, now: number): string {
  if (!(startedAt > 0) || now < startedAt) return "";
  const min = Math.floor((now - startedAt) / 60000);
  const d = Math.floor(min / 1440);
  const h = Math.floor((min % 1440) / 60);
  const m = min % 60;
  if (d > 0) return `${d} d ${String(h).padStart(2, "0")} h`;
  if (h > 0) return `${h} h ${String(m).padStart(2, "0")} min`;
  return `${m} min`;
}

/** Share of used memory in whole percent, 0 when the maximum is unknown. */
export function heapPercent(used: number, max: number): number {
  if (!(max > 0) || !(used > 0)) return 0;
  return Math.min(100, Math.round((used / max) * 100));
}

export type PowerAction = "shutdown" | "hibernate" | "standby";
export const POWER_ACTIONS: readonly PowerAction[] = ["shutdown", "hibernate", "standby"];

/** The actions that exist on this machine: none of the power actions in Docker, where they are refused anyway. */
export function availableActions(docker: boolean): ("restart" | "exit" | PowerAction)[] {
  return docker ? ["restart", "exit"] : ["restart", "exit", ...POWER_ACTIONS];
}
