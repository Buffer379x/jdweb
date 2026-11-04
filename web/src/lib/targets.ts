/** The save-to directory is set per package; the API (`directory`) answers 400 for a selection of links alone. */
export function canChangeDirectory(tg: { packageIds: readonly string[] }): boolean {
  return tg.packageIds.length > 0;
}
